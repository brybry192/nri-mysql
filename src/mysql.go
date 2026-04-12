//go:generate goversioninfo
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/newrelic/infra-integrations-sdk/v3/integration"
	"github.com/newrelic/infra-integrations-sdk/v3/log"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	arguments "github.com/newrelic/nri-mysql/src/args"
	dbutils "github.com/newrelic/nri-mysql/src/dbutils"
	infrautils "github.com/newrelic/nri-mysql/src/infrautils"
	queryperformancemonitoring "github.com/newrelic/nri-mysql/src/query-performance-monitoring"
	constants "github.com/newrelic/nri-mysql/src/query-performance-monitoring/constants"
	"github.com/newrelic/nri-mysql/src/shun"
)

var (
	args               arguments.ArgumentList
	integrationVersion = "0.0.0"
	gitCommit          = ""
	buildDate          = ""
)

func main() {
	i, err := integration.New(constants.IntegrationName, integrationVersion, integration.Args(&args))
	infrautils.FatalIfErr(err)

	if args.ShowVersion {
		fmt.Printf(
			"New Relic %s integration Version: %s, Platform: %s, GoVersion: %s, GitCommit: %s, BuildDate: %s\n",
			cases.Title(language.Und).String(strings.Replace(constants.IntegrationName, "com.newrelic.", "", 1)),
			integrationVersion,
			fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
			runtime.Version(),
			gitCommit,
			buildDate)
		os.Exit(0)
	}

	log.SetupLogging(args.Verbose)

	e, err := infrautils.CreateNodeEntity(i, args.RemoteMonitoring, args.Hostname, args.Port)
	infrautils.FatalIfErr(err)

	obs := ObservabilityConfig{
		CollectConnectionTiming:    args.CollectConnectionTiming,
		AvailabilityCheckQuery:     args.AvailabilityCheckQuery,
		AvailabilityCheckTimeoutMs: args.AvailabilityCheckTimeoutMs,
		CollectQueryTelemetry:      args.CollectQueryTelemetry,
	}

	obsEnabled := obs.CollectConnectionTiming || obs.AvailabilityCheckQuery != ""

	// Initialize shun manager when observability features are active.
	var shunMgr *shun.Manager
	if obsEnabled {
		stateDir := args.ShunStateDir
		if stateDir == "" {
			stateDir = os.TempDir()
		}
		cycleSec := args.CollectionCycleSec
		if cycleSec <= 0 {
			cycleSec = 15
		}
		stateFile := filepath.Join(stateDir, fmt.Sprintf("nri-mysql-shun-%s-%d.json", args.Hostname, args.Port))
		shunMgr = shun.NewManager(stateFile, time.Duration(cycleSec)*time.Second, nil)
		if err := shunMgr.LoadState(); err != nil {
			log.Warn("Failed to load shun state: %v", err)
		}
	}

	// If shunned, skip the connection entirely but still emit available=0
	// health samples so dashboards continue to report the outage.
	if shunMgr != nil && shunMgr.IsShunned() {
		st := shunMgr.CurrentState()
		log.Debug("Instance %s:%d is shunned (error=%s, backoff=%d cycles, until=%s) — skipping connection",
			args.Hostname, args.Port, st.ErrorCode, st.BackoffCycles, st.ShunnedUntil.Format(time.RFC3339))

		if args.HasMetrics() {
			shunnedErr := &classifiedError{code: st.ErrorCode, msg: "shunned: persistent failure"}
			publishShunnedHealthSample(e, st, shunnedErr, args.Hostname, args.Port, args.RemoteMonitoring)
		}
		infrautils.FatalIfErr(i.Publish())
		return
	}

	// Open DB — lazy pool, always succeeds for a valid DSN.
	// When timing or availability check is enabled, uses mysql.NewConnector
	// with a timingDialer so the first connection's DNS and TCP phases are measured.
	db, err := openDB(dbutils.GenerateDSN(args, ""), obs, args.Socket != "")
	infrautils.FatalIfErr(err)
	defer db.close()

	// Run observability probes (timing dialer + availability check) early so that
	// db.Timing is populated before we publish. Results are held and emitted after
	// MysqlSample so the existing metric ordering is preserved.
	var explicitResult *checkResult
	var connErr error
	var responseTimeMs float64
	if args.HasMetrics() {
		if obs.AvailabilityCheckQuery != "" {
			timeoutMs := obs.AvailabilityCheckTimeoutMs
			if timeoutMs <= 0 {
				timeoutMs = 5000
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
			explicitResult = explicitAvailabilityCheck(ctx, db, obs.AvailabilityCheckQuery, timeoutMs)
			cancel()
			responseTimeMs = explicitResult.durationMs
			// Use the availability check result to derive the implicit connection signal.
			if !explicitResult.available {
				connErr = &classifiedError{code: explicitResult.errorCode, msg: explicitResult.errorMessage}
			}
		} else if obs.CollectConnectionTiming {
			// No availability check — ping to trigger the timing dialer before reading db.Timing.
			pingStart := time.Now()
			connErr = db.ping()
			responseTimeMs = msec(time.Since(pingStart))
		}
	}

	rawInventory, rawMetrics, dbVersion, dataErr := getRawData(db)

	if args.HasInventory() && dataErr == nil {
		populateInventory(e.Inventory, rawInventory)
	}

	if args.HasMetrics() && dataErr == nil {
		ms := infrautils.MetricSet(
			e,
			"MysqlSample",
			args.Hostname,
			args.Port,
			args.RemoteMonitoring,
		)
		populateMetrics(ms, rawMetrics, dbVersion)

		if obs.CollectQueryTelemetry {
			publishQueryHealthSamples(e, db.drainTelemetry(), args.Hostname, args.Port, args.RemoteMonitoring)
		}
	}

	// Emit connection + availability samples only when at least one observability
	// feature is enabled, so the default behavior is unchanged.
	if args.HasMetrics() && obsEnabled {
		publishImplicitHealthSample(e, db.Timing, responseTimeMs, connErr, args.Hostname, args.Port, args.RemoteMonitoring)
		if explicitResult != nil {
			publishExplicitHealthSample(e, explicitResult, args.Hostname, args.Port, args.RemoteMonitoring)
		}
	}

	// Update shun state based on this cycle's outcome.
	if shunMgr != nil {
		errorCode := ""
		if connErr != nil {
			errorCode, _ = classifyError(connErr)
		} else if dataErr != nil {
			errorCode, _ = classifyError(dataErr)
		}
		if errorCode != "" {
			shunMgr.RecordFailure(errorCode, shun.IsShunnableMySQL)
		} else {
			shunMgr.RecordSuccess()
		}
		if err := shunMgr.SaveState(); err != nil {
			log.Warn("Failed to save shun state: %v", err)
		}
	}

	if dataErr != nil {
		log.Error("Error collecting MySQL metrics: %s", dataErr)
	}

	infrautils.FatalIfErr(i.Publish())

	// Exit with non-zero status after publishing so any partial observability
	// samples (connection/availability) are still delivered to the agent.
	if dataErr != nil {
		os.Exit(1)
	}

	if args.EnableQueryMonitoring {
		queryperformancemonitoring.PopulateQueryPerformanceMetrics(args, e, i)
	}
}
