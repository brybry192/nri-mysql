package shun

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a deterministic clock for testing.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTestManager(t *testing.T) (*Manager, *fakeClock) {
	t.Helper()
	stateFile := filepath.Join(t.TempDir(), "shun.json")
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m := NewManager(stateFile, 15*time.Second, clock)
	return m, clock
}

func alwaysShunnable(code string) bool { return true }
func neverShunnable(code string) bool  { return false }

func TestNotShunnedByDefault(t *testing.T) {
	m, _ := newTestManager(t)
	assert.False(t, m.IsShunned())
	assert.Equal(t, State{}, m.CurrentState())
}

func TestFirstFailureSetsBackoffToInitial(t *testing.T) {
	m, _ := newTestManager(t)
	m.RecordFailure("mysql_error_1045", alwaysShunnable)

	assert.True(t, m.IsShunned())
	st := m.CurrentState()
	assert.Equal(t, 1, st.ConsecutiveFails)
	assert.Equal(t, initialBackoffCycles, st.BackoffCycles) // 2
	assert.Equal(t, "mysql_error_1045", st.ErrorCode)
}

func TestBackoffDoublesOnConsecutiveFailures(t *testing.T) {
	m, clock := newTestManager(t)

	expected := []int{2, 4, 8, 16, 32, 60, 60} // capped at 60
	for i, want := range expected {
		// Advance past the current backoff so IsShunned returns false (retry allowed)
		clock.Advance(time.Duration(100) * 15 * time.Second)
		m.RecordFailure("mysql_error_1045", alwaysShunnable)
		assert.Equal(t, want, m.CurrentState().BackoffCycles, "iteration %d", i)
	}
}

func TestBackoffCapsAtMax(t *testing.T) {
	m, clock := newTestManager(t)
	for range 20 {
		clock.Advance(time.Duration(100) * 15 * time.Second)
		m.RecordFailure("mysql_error_1045", alwaysShunnable)
	}
	assert.Equal(t, maxBackoffCycles, m.CurrentState().BackoffCycles)
}

func TestSuccessClearsShunState(t *testing.T) {
	m, _ := newTestManager(t)
	m.RecordFailure("mysql_error_1045", alwaysShunnable)
	assert.True(t, m.IsShunned())

	m.RecordSuccess()
	assert.False(t, m.IsShunned())
	assert.Equal(t, State{}, m.CurrentState())
}

func TestNonShunnableErrorDoesNotTriggerShun(t *testing.T) {
	m, _ := newTestManager(t)
	m.RecordFailure("connection_reset", neverShunnable)

	assert.False(t, m.IsShunned())
	assert.Equal(t, 0, m.CurrentState().ConsecutiveFails)
}

func TestTTLExpiryAllowsRetry(t *testing.T) {
	m, clock := newTestManager(t)
	m.RecordFailure("mysql_error_1045", alwaysShunnable)
	assert.True(t, m.IsShunned())

	// Advance just past the backoff period: 2 cycles * 15s = 30s
	clock.Advance(31 * time.Second)
	assert.False(t, m.IsShunned(), "should allow retry after TTL expiry")
	// State is still set — only IsShunned returns false.
	assert.True(t, m.CurrentState().Shunned)
}

func TestStatePersistence(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "shun.json")
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	m1 := NewManager(stateFile, 15*time.Second, clock)
	m1.RecordFailure("mysql_error_1045", alwaysShunnable)
	require.NoError(t, m1.SaveState())

	// New manager loads the same file.
	m2 := NewManager(stateFile, 15*time.Second, clock)
	require.NoError(t, m2.LoadState())

	assert.True(t, m2.IsShunned())
	assert.Equal(t, "mysql_error_1045", m2.CurrentState().ErrorCode)
	assert.Equal(t, initialBackoffCycles, m2.CurrentState().BackoffCycles)
}

func TestLoadStateMissingFile(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "nonexistent.json")
	m := NewManager(stateFile, 15*time.Second, &fakeClock{now: time.Now()})

	require.NoError(t, m.LoadState())
	assert.False(t, m.IsShunned())
}

func TestLoadStateCorruptFile(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "corrupt.json")
	require.NoError(t, os.WriteFile(stateFile, []byte("not json{{{"), 0600))

	m := NewManager(stateFile, 15*time.Second, &fakeClock{now: time.Now()})
	require.NoError(t, m.LoadState())
	assert.False(t, m.IsShunned(), "corrupt state should reset to not shunned")
}

func TestMultiInstanceIndependence(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	m1 := NewManager(filepath.Join(dir, "instance1.json"), 15*time.Second, clock)
	m2 := NewManager(filepath.Join(dir, "instance2.json"), 15*time.Second, clock)

	m1.RecordFailure("mysql_error_1045", alwaysShunnable)
	require.NoError(t, m1.SaveState())

	require.NoError(t, m2.LoadState())
	assert.True(t, m1.IsShunned())
	assert.False(t, m2.IsShunned(), "instance2 should not be affected by instance1")
}

func TestSaveStateCreatesFile(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "new-state.json")
	m := NewManager(stateFile, 15*time.Second, &fakeClock{now: time.Now()})
	m.RecordFailure("dns_resolution_failed", alwaysShunnable)

	require.NoError(t, m.SaveState())

	_, err := os.Stat(stateFile)
	assert.NoError(t, err, "state file should exist after save")
}

func TestSaveStateFilePermissions(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "perms.json")
	m := NewManager(stateFile, 15*time.Second, &fakeClock{now: time.Now()})
	require.NoError(t, m.SaveState())

	info, err := os.Stat(stateFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "state file should be 0600")
}
