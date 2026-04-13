package args

import sdk_args "github.com/newrelic/infra-integrations-sdk/v3/args"

type ArgumentList struct {
	sdk_args.DefaultArgumentList
	Hostname                             string `default:"localhost" help:"Hostname or IP address where MySQL is running."`
	Port                                 int    `default:"3306" help:"Port number on which MySQL server is listening."`
	Socket                               string `default:"" help:"Path to the MySQL socket file."`
	Username                             string `default:"root" help:"Username for database access."`
	Password                             string `default:"password" help:"Password for the specified user."`
	Database                             string `help:"Name of the database."`
	ExtraConnectionURLArgs               string `help:"Additional connection parameters in the format attr1=val1&attr2=val2."` // https://github.com/go-sql-driver/mysql#parameters
	InsecureSkipVerify                   bool   `default:"false" help:"Skip TLS certificate verification when connecting."`
	EnableTLS                            bool   `default:"false" help:"Use a secure (TLS) connection."`
	RemoteMonitoring                     bool   `default:"false" help:"Indicates if the monitored entity is remote. Set to true if unsure."`
	ExtendedMetrics                      bool   `default:"false" help:"Enable collection of extended metrics."`
	ExtendedInnodbMetrics                bool   `default:"false" help:"Enable collection of extended InnoDB metrics."`
	ExtendedMyIsamMetrics                bool   `default:"false" help:"Enable collection of extended MyISAM metrics."`
	OldPasswords                         bool   `default:"false" help:"Allow the use of old passwords: https://dev.mysql.com/doc/refman/5.6/en/server-system-variables.html#sysvar_old_passwords"`
	ShowVersion                          bool   `default:"false" help:"Display build information and exit."`
	EnableQueryMonitoring                bool   `default:"false" help:"Enable collection of detailed query performance metrics."`
	SlowQueryMonitoringFetchInterval     int    `default:"30" help:"Fetch interval in seconds for grouped slow queries. Should match the interval in mysql-config.yml."`
	CollectConnectionTiming              bool   `default:"false" help:"If true, measures DNS lookup and TCP connect time via the connection's dial hook and reports them in MysqlHealthSample with checkType=implicit."`
	AvailabilityCheckQuery               string `default:"" help:"SQL query for explicit availability check. If set, runs each cycle with a deadline and reports result in MysqlHealthSample with checkType=explicit. Common value: 'SELECT 1'."`
	AvailabilityCheckTimeoutMs           int    `default:"5000" help:"Timeout in milliseconds for the explicit availability check query. Defaults to 5000ms."`
	CollectQueryTelemetry                bool   `default:"false" help:"If true, emits a MysqlHealthSample event with checkType=query for every internal monitoring query with duration, error status, and error classification."`
	QueryMonitoringResponseTimeThreshold int    `default:"1" help:"Threshold in milliseconds for query response time to fetch individual query performance metrics."`
	QueryMonitoringCountThreshold        int    `default:"20" help:"Query count limit for fetching grouped slow and individual query performance metrics."`
	ExcludedPerformanceDatabases         string `default:"[]" help:"A JSON array that lists databases to be excluded from performance metrics collection. System databases are always excluded."`
}
