package logging

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLine_ExplicitLevel(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		level Level
		msg   string
	}{
		{
			name:  "explicit ERROR bracket",
			line:  "2026/05/07 15:30:00 [ERROR] something went wrong",
			level: LevelError,
			msg:   "something went wrong",
		},
		{
			name:  "explicit WARN bracket",
			line:  "2026/05/07 15:30:00 [WARN]  low disk space",
			level: LevelWarn,
			msg:   "low disk space",
		},
		{
			name:  "explicit DEBUG bracket",
			line:  "2026/05/07 15:30:00 [DEBUG] verbose detail",
			level: LevelDebug,
			msg:   "verbose detail",
		},
		{
			name:  "explicit INFO bracket",
			line:  "2026/05/07 15:30:00 [INFO]  connected",
			level: LevelInfo,
			msg:   "connected",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := parseLine(tc.line)
			assert.Equal(t, tc.level, e.Level)
			assert.Equal(t, tc.msg, e.Message)
		})
	}
}

// TestParseLine_PluginProxyLog verifies that plugin subprocess logs routed via
// prefixWriter→logger.Info arrive as "[INFO]  [plugin/xxx] <msg>" and that the
// keyword-based level detection upgrades the level when the message contains
// "error", "failed", "warn", etc.
//
// This is the non-regression test for issue #96 (store.go: apply keyword detection
// even when levelExplicit=true and level is INFO, so proxy-wrapped error messages
// from plugin subprocesses are not silently displayed as INFO).
func TestParseLine_PluginProxyLog_ErrorUpgrade(t *testing.T) {
	// Plugin error message proxied through logger.Info → arrives as [INFO] [plugin/...]
	line := "2026/05/07 15:30:00 [INFO]  [plugin/moosefs.ghdp] mfsclient: connection failed"
	e := parseLine(line)
	assert.Equal(t, LevelError, e.Level, "proxy-wrapped error message must be upgraded to ERROR")
	assert.Equal(t, "plugin/moosefs.ghdp", e.Source)
	assert.Equal(t, "mfsclient: connection failed", e.Message)
}

func TestParseLine_PluginProxyLog_WarnUpgrade(t *testing.T) {
	line := "2026/05/07 15:30:00 [INFO]  [plugin/webdav.ghdp] slow response — warning threshold exceeded"
	e := parseLine(line)
	assert.Equal(t, LevelWarn, e.Level, "proxy-wrapped warn message must be upgraded to WARN")
}

func TestParseLine_PluginProxyLog_InfoStaysInfo(t *testing.T) {
	line := "2026/05/07 15:30:00 [INFO]  [plugin/moosefs.ghdp] sessionID=42 registered"
	e := parseLine(line)
	assert.Equal(t, LevelInfo, e.Level, "proxy-wrapped info message must stay INFO")
	assert.Equal(t, "plugin/moosefs.ghdp", e.Source)
}

func TestParseLine_PluginProxyLog_DebugNotDowngraded(t *testing.T) {
	// An explicit [INFO] bracket must NOT be downgraded to DEBUG even if the
	// message contains "debug" — the keyword only applies when levelExplicit=false.
	line := "2026/05/07 15:30:00 [INFO]  [plugin/moosefs.ghdp] entering debug handler"
	e := parseLine(line)
	assert.Equal(t, LevelInfo, e.Level, "explicit INFO must not be downgraded to DEBUG by keyword")
}

func TestParseLine_ExplicitError_KeywordDoesNotDowngrade(t *testing.T) {
	// An explicit [ERROR] bracket must stay ERROR — keyword check does not
	// override a non-INFO explicit level.
	line := "2026/05/07 15:30:00 [ERROR] everything is fine actually"
	e := parseLine(line)
	assert.Equal(t, LevelError, e.Level)
}

func TestParseLine_NoTimestamp(t *testing.T) {
	line := "[INFO]  simple message"
	e := parseLine(line)
	assert.Equal(t, LevelInfo, e.Level)
	assert.Equal(t, "simple message", e.Message)
}

func TestParseLine_KeywordOnly_NoExplicitBracket(t *testing.T) {
	line := "something failed badly"
	e := parseLine(line)
	assert.Equal(t, LevelError, e.Level)
}

func TestStore_Write_PopulatesEntries(t *testing.T) {
	s := NewStore()
	n, err := s.Write([]byte("2026/05/07 15:30:00 [ERROR] disk full\n"))
	require.NoError(t, err)
	assert.Greater(t, n, 0)

	entries := s.GetEntries(0)
	require.Len(t, entries, 1)
	assert.Equal(t, LevelError, entries[0].Level)
	assert.True(t, strings.Contains(entries[0].Message, "disk full"))
}

func TestStore_GetEntries_SinceID(t *testing.T) {
	s := NewStore()
	_, _ = s.Write([]byte("line 1\n"))
	_, _ = s.Write([]byte("line 2\n"))
	_, _ = s.Write([]byte("line 3\n"))

	all := s.GetEntries(0)
	require.Len(t, all, 3)

	after2 := s.GetEntries(2)
	require.Len(t, after2, 1)
	assert.Contains(t, after2[0].Message, "line 3")
}

func TestStore_Clear(t *testing.T) {
	s := NewStore()
	_, _ = s.Write([]byte("something\n"))
	s.Clear()
	assert.Empty(t, s.GetEntries(0))
}

func TestStore_OnNew_Callback(t *testing.T) {
	s := NewStore()
	var received []Entry
	s.SetOnNew(func(e Entry) { received = append(received, e) })

	_, _ = s.Write([]byte("event one\n"))
	_, _ = s.Write([]byte("event two\n"))

	assert.Len(t, received, 2)
}
