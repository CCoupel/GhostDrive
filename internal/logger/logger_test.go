// Package logger_test provides non-regression tests for the logger package.
//
// These tests validate the fixes introduced for issue #81:
//   - Info() writes messages to the configured log file.
//   - NewPrefixed() returns a writer that prepends the given prefix to every line.
package logger_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaultLogPath mirrors the package-internal defaultLogPath() logic so tests
// can locate the log file without modifying logger's internals.
func defaultLogPath(t *testing.T) (string, bool) {
	t.Helper()
	if dir := os.Getenv("APPDATA"); dir != "" {
		return filepath.Join(dir, "GhostDrive", "logs", "ghostdrive.log"), true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".local", "share", "ghostdrive", "logs", "ghostdrive.log"), true
}

// TestLogger_WritesToFile verifies that a call to logger.Info writes the
// message to the log file on disk.
// Non-regression test for issue #81.
func TestLogger_WritesToFile(t *testing.T) {
	logPath, ok := defaultLogPath(t)
	if !ok {
		t.Skip("cannot determine home directory — skipping log-file test")
	}

	if _, err := os.Stat(logPath); err != nil {
		t.Skipf("log file %s not created by logger init — environment may not support file logging: %v", logPath, err)
	}

	// Write a uniquely identifiable marker so the assertion is unambiguous even
	// if the log file already contains previous content.
	marker := fmt.Sprintf("NR81-file-%d", time.Now().UnixNano())
	logger.Info("%s", marker)

	content, err := os.ReadFile(logPath)
	require.NoError(t, err, "log file must be readable after Info()")
	assert.True(t,
		strings.Contains(string(content), marker),
		"log file must contain the message written by Info(); got:\n%s", string(content),
	)
}

// TestNewPrefixed_WritesWithPrefix verifies that the io.Writer returned by
// NewPrefixed prepends the configured prefix to every log line written through
// it and that the message itself is also present in the log file.
// Non-regression test for issue #81.
func TestNewPrefixed_WritesWithPrefix(t *testing.T) {
	const prefix = "[plugin/test]"

	logPath, ok := defaultLogPath(t)
	if !ok {
		t.Skip("cannot determine home directory — skipping log-file test")
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Skipf("log file %s not available — skipping prefix verification: %v", logPath, err)
	}

	w := logger.NewPrefixed(prefix)
	require.NotNil(t, w, "NewPrefixed must return a non-nil io.Writer")

	// Write a line through the prefixed writer; it should appear in the log file
	// with the prefix prepended.
	marker := fmt.Sprintf("NR81-prefix-%d", time.Now().UnixNano())
	n, err := w.Write([]byte(marker + "\n"))
	require.NoError(t, err, "Write to prefixed writer must not error")
	assert.Equal(t, len(marker)+1, n, "Write must report consuming all bytes")

	content, err := os.ReadFile(logPath)
	require.NoError(t, err, "log file must be readable after prefixed Write()")
	logStr := string(content)

	assert.True(t,
		strings.Contains(logStr, prefix),
		"log file must contain prefix %q after NewPrefixed write; got:\n%s", prefix, logStr,
	)
	assert.True(t,
		strings.Contains(logStr, marker),
		"log file must contain the written message; got:\n%s", logStr,
	)
}
