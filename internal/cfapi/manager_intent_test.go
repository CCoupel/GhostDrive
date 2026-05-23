package cfapi

// Tests for CFManager pin-intent queue introduced in v2.2 (#143).
//
// Coverage:
//   - QueuePinIntent stores an intent
//   - QueuePinIntent overwrites a previous intent for the same path (last-intent-wins)
//   - ConsumeIntent returns false when no intent exists
//   - ConsumeIntent reads and removes the intent (LoadAndDelete)
//   - Thread-safety: concurrent Queue and Consume from multiple goroutines

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── QueuePinIntent / ConsumeIntent basics ───────────────────────────────────

func TestCFManager_QueuePinIntent_Basic(t *testing.T) {
	m := newTestManager(t)
	const (
		backendID = "backend-1"
		localPath = "/local/doc.txt"
	)

	m.QueuePinIntent(backendID, localPath, true)

	bid, pin, ok := m.ConsumeIntent(localPath)
	require.True(t, ok, "intent must be found after QueuePinIntent")
	assert.Equal(t, backendID, bid)
	assert.True(t, pin)
}

func TestCFManager_QueuePinIntent_UnpinIntent(t *testing.T) {
	m := newTestManager(t)
	m.QueuePinIntent("b1", "/local/img.png", false /* unpin */)

	_, pin, ok := m.ConsumeIntent("/local/img.png")
	require.True(t, ok)
	assert.False(t, pin, "unpin intent must be correctly stored")
}

// TestCFManager_QueuePinIntent_Overwrites verifies that queuing a second intent
// for the same path replaces the first one (last-intent-wins policy).
func TestCFManager_QueuePinIntent_Overwrites(t *testing.T) {
	m := newTestManager(t)
	const p = "/local/file.bin"

	m.QueuePinIntent("backend-1", p, true)  // first intent: pin
	m.QueuePinIntent("backend-2", p, false) // second intent: unpin — must win

	bid, pin, ok := m.ConsumeIntent(p)
	require.True(t, ok)
	assert.Equal(t, "backend-2", bid, "second QueuePinIntent must overwrite the first")
	assert.False(t, pin, "last intent (unpin) must win over first (pin)")
}

// TestCFManager_ConsumeIntent_NoIntent verifies that consuming a non-existent
// intent returns (""", false, false) without panicking.
func TestCFManager_ConsumeIntent_NoIntent(t *testing.T) {
	m := newTestManager(t)

	bid, pin, ok := m.ConsumeIntent("/no/such/path.txt")
	assert.False(t, ok, "must return ok=false when no intent exists")
	assert.Empty(t, bid)
	assert.False(t, pin)
}

// TestCFManager_ConsumeIntent_ClearsEntry verifies that after a successful
// Consume the entry is removed and a second Consume returns false.
func TestCFManager_ConsumeIntent_ClearsEntry(t *testing.T) {
	m := newTestManager(t)
	const p = "/local/remove.txt"

	m.QueuePinIntent("b", p, true)

	// First consume.
	_, _, ok := m.ConsumeIntent(p)
	require.True(t, ok)

	// Second consume — entry must be gone.
	_, _, ok2 := m.ConsumeIntent(p)
	assert.False(t, ok2, "intent must not be present after first Consume")
}

// ─── Thread-safety ───────────────────────────────────────────────────────────

// TestCFManager_QueueConsumeIntent_Concurrent verifies that concurrent
// Queue and Consume calls from multiple goroutines do not race.
// Run with: go test -race ./internal/cfapi/...
func TestCFManager_QueueConsumeIntent_Concurrent(t *testing.T) {
	m := newTestManager(t)

	const (
		goroutines = 16
		iters      = 200
	)

	var wg sync.WaitGroup

	// Writers.
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iters {
				path := fmt.Sprintf("/local/file-%d-%d.txt", id, j%10)
				pin := j%2 == 0
				m.QueuePinIntent(fmt.Sprintf("backend-%d", id), path, pin)
			}
		}(i)
	}

	// Readers (interleaved with writers).
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iters {
				path := fmt.Sprintf("/local/file-%d-%d.txt", id, j%10)
				_, _, _ = m.ConsumeIntent(path)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// no race → test passes
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent QueuePinIntent/ConsumeIntent timed out or deadlocked")
	}
}

// TestCFManager_ConsumeIntent_ReturnsCorrectFields verifies all three return
// values of ConsumeIntent when a full PinIntent is stored.
func TestCFManager_ConsumeIntent_ReturnsCorrectFields(t *testing.T) {
	m := newTestManager(t)

	m.QueuePinIntent("the-backend", "/the/path.txt", true)

	bid, pin, ok := m.ConsumeIntent("/the/path.txt")

	assert.True(t, ok)
	assert.Equal(t, "the-backend", bid)
	assert.True(t, pin)
}

// TestCFManager_QueuePinIntent_MultipleDistinctPaths verifies that independent
// paths are stored and consumed independently (no cross-contamination).
func TestCFManager_QueuePinIntent_MultipleDistinctPaths(t *testing.T) {
	m := newTestManager(t)

	m.QueuePinIntent("b1", "/path/a.txt", true)
	m.QueuePinIntent("b2", "/path/b.txt", false)

	bidA, pinA, okA := m.ConsumeIntent("/path/a.txt")
	bidB, pinB, okB := m.ConsumeIntent("/path/b.txt")

	require.True(t, okA)
	require.True(t, okB)
	assert.Equal(t, "b1", bidA)
	assert.True(t, pinA)
	assert.Equal(t, "b2", bidB)
	assert.False(t, pinB)
}
