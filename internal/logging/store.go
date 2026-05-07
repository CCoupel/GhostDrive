// Package logging provides an in-process log store that captures standard
// log output and exposes entries to the Wails frontend via GetLogs / ClearLogs.
package logging

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// Level represents a log severity level.
type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

// Entry is a single captured log line.
type Entry struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`    // RFC3339
	Level   Level  `json:"level"`
	Source  string `json:"source"`  // extracted from [source] prefix, may be empty
	Message string `json:"message"`
}

const maxEntries = 2000

// Store captures log output written by the standard log package and makes
// entries available for polling or event-driven streaming.
//
// Implements io.Writer — pass to log.SetOutput(io.MultiWriter(os.Stderr, store)).
type Store struct {
	mu      sync.Mutex
	entries []Entry
	seq     int64
	onNew   func(Entry) // called outside mu
}

// NewStore returns an empty Store ready to use.
func NewStore() *Store {
	return &Store{entries: make([]Entry, 0, 256)}
}

// SetOnNew registers a callback invoked (outside the mutex) for every new entry.
// Pass nil to disable.
func (s *Store) SetOnNew(fn func(Entry)) {
	s.mu.Lock()
	s.onNew = fn
	s.mu.Unlock()
}

// Write implements io.Writer.  Each call is expected to be one log line
// (the standard log package guarantees atomic per-line writes).
func (s *Store) Write(p []byte) (n int, err error) {
	line := strings.TrimRightFunc(string(p), unicode.IsSpace)
	if line == "" {
		return len(p), nil
	}

	entry := parseLine(line)

	var fn func(Entry)
	s.mu.Lock()
	s.seq++
	entry.ID = s.seq
	if len(s.entries) >= maxEntries {
		// Drop oldest entry.
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:len(s.entries)-1]
	}
	s.entries = append(s.entries, entry)
	fn = s.onNew
	s.mu.Unlock()

	if fn != nil {
		fn(entry)
	}
	return len(p), nil
}

// GetEntries returns all entries with ID > sinceID.
// Pass sinceID=0 to return all stored entries.
func (s *Store) GetEntries(sinceID int64) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sinceID <= 0 {
		result := make([]Entry, len(s.entries))
		copy(result, s.entries)
		return result
	}

	var result []Entry
	for _, e := range s.entries {
		if e.ID > sinceID {
			result = append(result, e)
		}
	}
	return result
}

// Clear removes all stored entries.
func (s *Store) Clear() {
	s.mu.Lock()
	s.entries = s.entries[:0]
	s.mu.Unlock()
}

// ─── Internal parsing ─────────────────────────────────────────────────────────

// parseLine converts a raw log line into an Entry.
//
// Standard log format (LstdFlags = Ldate|Ltime):
//
//	"2026/05/07 15:30:00 [source] message"
//
// Detected levels (keyword matching, case-insensitive):
//   - error / failed / fatal / panic → ERROR
//   - warn / warning                → WARN
//   - debug / trace                 → DEBUG
//   - (default)                     → INFO
func parseLine(line string) Entry {
	e := Entry{
		Time:  time.Now().Format(time.RFC3339),
		Level: LevelInfo,
	}

	msg := line

	// Strip "YYYY/MM/DD HH:MM:SS " prefix (20 chars).
	if len(line) >= 20 && line[4] == '/' && line[7] == '/' && line[13] == ':' {
		ts := line[:19]
		if t, err := time.ParseInLocation("2006/01/02 15:04:05", ts, time.Local); err == nil {
			e.Time = t.Format(time.RFC3339)
		}
		msg = line[20:]
	}

	// Extract [source] prefix.
	if len(msg) > 0 && msg[0] == '[' {
		if end := strings.Index(msg, "] "); end > 0 {
			e.Source = msg[1:end]
			msg = msg[end+2:]
		}
	}

	// Detect level from keywords.
	lower := strings.ToLower(msg)
	switch {
	case containsAny(lower, "error", "failed", "fatal", "panic"):
		e.Level = LevelError
	case containsAny(lower, "warn", "warning"):
		e.Level = LevelWarn
	case containsAny(lower, "debug", "trace"):
		e.Level = LevelDebug
	}

	e.Message = msg
	return e
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}
