package sync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueEnqueueDequeue(t *testing.T) {
	q := &SyncQueue{}

	task := &SyncTask{
		ID:         "t1",
		LocalPath:  "/local/file.txt",
		RemotePath: "/remote/file.txt",
		Direction:  DirectionUpload,
	}
	q.Enqueue(task)
	assert.Equal(t, 1, q.Size())

	got, ok := q.Dequeue()
	require.True(t, ok)
	assert.Equal(t, "t1", got.ID)
	assert.Equal(t, 0, q.Size())
}

func TestQueueDequeueEmpty(t *testing.T) {
	q := &SyncQueue{}
	got, ok := q.Dequeue()
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestQueueRetryNotReady(t *testing.T) {
	q := &SyncQueue{}
	task := &SyncTask{
		ID:        "t2",
		NextRetry: time.Now().Add(1 * time.Hour), // not ready yet
	}
	q.Enqueue(task)

	got, ok := q.Dequeue()
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestQueueRequeue(t *testing.T) {
	q := &SyncQueue{}
	task := &SyncTask{ID: "t3"}

	requeued := q.Requeue(task)
	assert.True(t, requeued, "first retry should succeed")
	assert.Equal(t, 1, task.Retries)
	assert.Equal(t, 1, q.Size())
}

func TestQueueRequeueTooManyRetries(t *testing.T) {
	q := &SyncQueue{}
	task := &SyncTask{ID: "t4", Retries: maxRetries - 1}

	requeued := q.Requeue(task)
	assert.False(t, requeued, "should be abandoned after max retries")
}

func TestQueueBackoffProgression(t *testing.T) {
	task := &SyncTask{ID: "backoff"}

	before := time.Now()
	task.RecordFailure() // Retries=1, backoff=1s
	assert.Equal(t, 1, task.Retries)
	assert.True(t, task.NextRetry.After(before.Add(900*time.Millisecond)),
		"backoff should be ~1s")

	task.RecordFailure() // Retries=2, backoff=2s
	assert.Equal(t, 2, task.Retries)
	assert.True(t, task.NextRetry.After(before.Add(1900*time.Millisecond)),
		"backoff should be ~2s")
}

func TestQueueAbandon(t *testing.T) {
	task := &SyncTask{ID: "abandon", Retries: maxRetries}
	assert.True(t, task.ShouldAbandon())
}

func TestQueueDrain(t *testing.T) {
	q := &SyncQueue{}
	for i := 0; i < 5; i++ {
		q.Enqueue(&SyncTask{ID: "task"})
	}
	assert.Equal(t, 5, q.Size())

	drained := q.Drain()
	assert.Len(t, drained, 5)
	assert.Equal(t, 0, q.Size())
}

func TestQueueSize(t *testing.T) {
	q := &SyncQueue{}
	assert.Equal(t, 0, q.Size())
	q.Enqueue(&SyncTask{})
	q.Enqueue(&SyncTask{})
	assert.Equal(t, 2, q.Size())
}
