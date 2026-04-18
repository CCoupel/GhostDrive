package sync

import (
	"sync"
	"time"
)

const (
	maxRetries     = 5
	baseBackoff    = time.Second
)

// TaskDirection indicates the direction of synchronization.
type TaskDirection string

const (
	DirectionUpload   TaskDirection = "upload"
	DirectionDownload TaskDirection = "download"
)

// SyncTask represents a single synchronization operation to execute.
type SyncTask struct {
	ID         string
	LocalPath  string
	RemotePath string
	LocalRoot  string // allowed root for path-traversal validation
	Direction  TaskDirection
	Retries    int
	NextRetry  time.Time
}

// IsReady returns true if the task is eligible for execution (retry delay elapsed).
func (t *SyncTask) IsReady() bool {
	return time.Now().After(t.NextRetry)
}

// ShouldAbandon returns true if the task has exceeded the maximum retry count.
func (t *SyncTask) ShouldAbandon() bool {
	return t.Retries >= maxRetries
}

// RecordFailure increments the retry counter and sets the next retry time
// using exponential backoff: 1s, 2s, 4s, 8s, 16s.
func (t *SyncTask) RecordFailure() {
	t.Retries++
	backoff := baseBackoff * (1 << uint(t.Retries-1))
	t.NextRetry = time.Now().Add(backoff)
}

// SyncQueue is a thread-safe FIFO queue of SyncTasks with retry support.
type SyncQueue struct {
	mu    sync.Mutex
	items []*SyncTask
}

// Enqueue adds a task to the back of the queue.
func (q *SyncQueue) Enqueue(task *SyncTask) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, task)
}

// Dequeue returns the next ready task (retry delay elapsed).
// Returns (nil, false) if no ready task is available.
func (q *SyncQueue) Dequeue() (*SyncTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, task := range q.items {
		if task.ShouldAbandon() {
			// Remove abandoned tasks silently
			q.items = append(q.items[:i], q.items[i+1:]...)
			return nil, false
		}
		if task.IsReady() {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return task, true
		}
	}

	return nil, false
}

// Requeue puts a task back at the end of the queue after a failure.
// If the task has exceeded max retries it is discarded.
func (q *SyncQueue) Requeue(task *SyncTask) bool {
	task.RecordFailure()
	if task.ShouldAbandon() {
		return false
	}
	q.Enqueue(task)
	return true
}

// Size returns the number of tasks currently in the queue.
func (q *SyncQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Drain removes and returns all tasks (used for shutdown/testing).
func (q *SyncQueue) Drain() []*SyncTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	tasks := make([]*SyncTask, len(q.items))
	copy(tasks, q.items)
	q.items = nil
	return tasks
}
