// Package logger provides centralized logging for GhostDrive.
//
// Logs are written to a rotating file:
//   - Windows: %APPDATA%\GhostDrive\logs\ghostdrive.log
//   - Linux:   ~/.local/share/ghostdrive/logs/ghostdrive.log
//
// Set GHOSTDRIVE_DEBUG=1 to enable DEBUG-level messages and to duplicate all
// output to stderr.
//
// Rotation: the log file is rotated when it exceeds 10 MB. Up to 3 backup
// files are kept (ghostdrive.log.1, .2, .3).
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxLogSize = 10 * 1024 * 1024 // 10 MB
	maxBackups = 3
)

var (
	mu           sync.Mutex
	logFile      *os.File
	logPath      string
	debugEnabled bool

	infoLog  *log.Logger
	debugLog *log.Logger
	warnLog  *log.Logger
	errLog   *log.Logger
)

func init() {
	debugEnabled = os.Getenv("GHOSTDRIVE_DEBUG") == "1"
	logPath = defaultLogPath()

	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err == nil {
		logFile, _ = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	}

	setupLoggers()
}

// defaultLogPath returns the platform-appropriate log file path.
// On Windows (APPDATA set): %APPDATA%\GhostDrive\logs\ghostdrive.log
// On Linux/macOS: ~/.local/share/ghostdrive/logs/ghostdrive.log
func defaultLogPath() string {
	if dir, ok := os.LookupEnv("APPDATA"); ok && dir != "" {
		return filepath.Join(dir, "GhostDrive", "logs", "ghostdrive.log")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ghostdrive", "logs", "ghostdrive.log")
}

// setupLoggers (re-)creates the four level-prefixed loggers.
// Must be called either before any goroutine starts (init) or with mu held.
func setupLoggers() {
	var writers []io.Writer
	if logFile != nil {
		writers = append(writers, logFile)
	}
	if debugEnabled || logFile == nil {
		writers = append(writers, os.Stderr)
	}

	var w io.Writer
	switch len(writers) {
	case 0:
		w = io.Discard
	case 1:
		w = writers[0]
	default:
		w = io.MultiWriter(writers...)
	}

	flags := log.LstdFlags
	infoLog = log.New(w, "[INFO]  ", flags)
	debugLog = log.New(w, "[DEBUG] ", flags)
	warnLog = log.New(w, "[WARN]  ", flags)
	errLog = log.New(w, "[ERROR] ", flags)
}

// rotate rotates the log file when it exceeds maxLogSize.
// Must be called with mu held.
func rotate() {
	if logFile == nil {
		return
	}
	info, err := logFile.Stat()
	if err != nil || info.Size() < maxLogSize {
		return
	}

	logFile.Close()
	logFile = nil

	// Shift .2→.3, .1→.2; drop .3 (oldest kept backup) before shifting.
	for i := maxBackups; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", logPath, i)
		dst := fmt.Sprintf("%s.%d", logPath, i+1)
		if i == maxBackups {
			os.Remove(src) // remove .3, not .4 — only maxBackups files kept
			continue
		}
		os.Rename(src, dst) //nolint:errcheck
	}
	os.Rename(logPath, logPath+".1") //nolint:errcheck

	logFile, _ = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	setupLoggers()
}

// Info logs a message at INFO level.
func Info(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	rotate()
	infoLog.Printf(format, args...)
}

// Debug logs a message at DEBUG level. No-op unless GHOSTDRIVE_DEBUG=1.
func Debug(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	rotate()
	debugLog.Printf(format, args...)
}

// Warn logs a message at WARN level.
func Warn(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	rotate()
	warnLog.Printf(format, args...)
}

// Error logs a message at ERROR level.
func Error(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()
	rotate()
	errLog.Printf(format, args...)
}

// NewPrefixed returns an io.Writer that prefixes each line with prefix and
// routes it through the central logger (INFO level). Intended for use as
// cmd.Stdout / cmd.Stderr on plugin subprocesses so their output lands in the
// GhostDrive log file.
func NewPrefixed(prefix string) io.Writer {
	return &prefixWriter{prefix: prefix}
}

// prefixWriter is an io.Writer that logs each received line via Info.
type prefixWriter struct {
	prefix string
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	// Split on newlines; each non-empty line is logged independently.
	lines := strings.Split(strings.TrimRight(string(p), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		Info("%s%s", pw.prefix, line)
	}
	return len(p), nil
}
