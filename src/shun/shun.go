// Package shun implements error-aware shunning with exponential backoff for
// database monitoring integrations. When a persistent failure is detected
// (e.g. wrong password, unknown database), the Manager enters a shunned state
// and skips connection attempts for an exponentially increasing number of
// collection cycles. This prevents hammering a misconfigured target and avoids
// MySQL's max_connect_errors host blocking.
package shun

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/newrelic/infra-integrations-sdk/v3/log"
)

const (
	// initialBackoffCycles is the number of cycles to skip after the first
	// shunnable failure.
	initialBackoffCycles = 2

	// maxBackoffCycles caps exponential growth. At a 15-second cycle this is
	// ~15 minutes between retry attempts.
	maxBackoffCycles = 60
)

// Clock abstracts time for testing.
type Clock interface {
	Now() time.Time
}

// RealClock uses the system clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// State holds the persisted shun state for a single monitored instance.
type State struct {
	Shunned          bool      `json:"shunned"`
	ErrorCode        string    `json:"error_code"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	BackoffCycles    int       `json:"backoff_cycles"`
	ShunnedAt        time.Time `json:"shunned_at"`
	ShunnedUntil     time.Time `json:"shunned_until"`
}

// Manager tracks shun state for a single monitored instance.
type Manager struct {
	stateFile     string
	cycleDuration time.Duration
	clock         Clock
	state         State
}

// NewManager creates a Manager that persists state to stateFile.
// cycleDuration is the expected interval between collection cycles (e.g. 15s).
func NewManager(stateFile string, cycleDuration time.Duration, clock Clock) *Manager {
	if clock == nil {
		clock = RealClock{}
	}
	return &Manager{
		stateFile:     stateFile,
		cycleDuration: cycleDuration,
		clock:         clock,
	}
}

// IsShunned returns true if the instance is currently shunned and the backoff
// period has not expired.
func (m *Manager) IsShunned() bool {
	if !m.state.Shunned {
		return false
	}
	if m.clock.Now().After(m.state.ShunnedUntil) {
		// TTL expired — allow a retry.
		return false
	}
	return true
}

// CurrentState returns a copy of the current shun state.
func (m *Manager) CurrentState() State {
	return m.state
}

// RecordSuccess clears the shun state. Call this after a successful collection cycle.
func (m *Manager) RecordSuccess() {
	m.state = State{}
}

// RecordFailure updates the shun state based on the error code. If isShunnable
// returns true for the code, consecutive failures are tracked and backoff
// grows exponentially. Non-shunnable errors do not trigger or extend shunning.
func (m *Manager) RecordFailure(errorCode string, isShunnable func(string) bool) {
	if !isShunnable(errorCode) {
		return
	}

	now := m.clock.Now()
	m.state.ConsecutiveFails++
	m.state.ErrorCode = errorCode
	m.state.Shunned = true
	m.state.ShunnedAt = now

	if m.state.BackoffCycles == 0 {
		m.state.BackoffCycles = initialBackoffCycles
	} else {
		m.state.BackoffCycles *= 2
	}
	if m.state.BackoffCycles > maxBackoffCycles {
		m.state.BackoffCycles = maxBackoffCycles
	}

	m.state.ShunnedUntil = now.Add(time.Duration(m.state.BackoffCycles) * m.cycleDuration)
}

// LoadState reads persisted shun state from disk. If the file doesn't exist or
// is corrupt, the state is reset to zero (not shunned).
func (m *Manager) LoadState() error {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			m.state = State{}
			return nil
		}
		return fmt.Errorf("reading shun state: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		log.Warn("Corrupt shun state file %s, resetting: %v", m.stateFile, err)
		m.state = State{}
		return nil
	}
	m.state = s
	return nil
}

// SaveState writes the current shun state to disk.
func (m *Manager) SaveState() error {
	data, err := json.Marshal(m.state)
	if err != nil {
		return fmt.Errorf("marshaling shun state: %w", err)
	}
	if err := os.WriteFile(m.stateFile, data, 0600); err != nil {
		return fmt.Errorf("writing shun state: %w", err)
	}
	return nil
}
