package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.NotEmpty(t, cfg.Version)
	assert.Equal(t, 512, cfg.CacheSizeMaxMB)
	assert.NotNil(t, cfg.Backends)
	assert.Empty(t, cfg.Backends)
	assert.False(t, cfg.CacheEnabled)
	assert.False(t, cfg.StartMinimized)
	assert.False(t, cfg.AutoStart)
}

func TestLoadFromFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	data := `{
		"version": "0.1.0",
		"backends": [],
		"cacheEnabled": true,
		"cacheDir": "/tmp/cache",
		"cacheSizeMaxMB": 1024,
		"startMinimized": true,
		"autoStart": false
	}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "0.1.0", cfg.Version)
	assert.True(t, cfg.CacheEnabled)
	assert.Equal(t, "/tmp/cache", cfg.CacheDir)
	assert.Equal(t, 1024, cfg.CacheSizeMaxMB)
	assert.True(t, cfg.StartMinimized)
	assert.False(t, cfg.AutoStart)
}

func TestLoadMissingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nonexistent.json")

	cfg, err := Load(path)
	require.NoError(t, err, "missing file should return defaults without error")
	assert.Equal(t, DefaultConfig().Version, cfg.Version)
	assert.Equal(t, DefaultConfig().CacheSizeMaxMB, cfg.CacheSizeMaxMB)
}

func TestLoadInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{invalid json`), 0600))

	_, err := Load(path)
	assert.Error(t, err)
}

func TestSaveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "config.json")

	original := AppConfig{
		Version: "0.1.0",
		Backends: []plugins.BackendConfig{
			{
				ID:      "test-id",
				Name:    "My WebDAV",
				Type:    "webdav",
				Enabled: true,
				Params: map[string]string{
					"url":      "https://dav.example.com",
					"username": "user",
					// password intentionally omitted from test to document convention
				},
				SyncDir: "/home/user/sync",
			},
		},
		CacheEnabled:   true,
		CacheDir:       "/tmp/ghostdrive-cache",
		CacheSizeMaxMB: 256,
		StartMinimized: false,
		AutoStart:      true,
	}

	require.NoError(t, Save(original, path))

	loaded, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, original.Version, loaded.Version)
	assert.Equal(t, original.CacheEnabled, loaded.CacheEnabled)
	assert.Equal(t, original.CacheDir, loaded.CacheDir)
	assert.Equal(t, original.CacheSizeMaxMB, loaded.CacheSizeMaxMB)
	assert.Equal(t, original.AutoStart, loaded.AutoStart)
	require.Len(t, loaded.Backends, 1)
	assert.Equal(t, original.Backends[0].ID, loaded.Backends[0].ID)
	assert.Equal(t, original.Backends[0].Type, loaded.Backends[0].Type)
}

func TestSaveCreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	deepPath := filepath.Join(tmp, "a", "b", "c", "config.json")

	cfg := DefaultConfig()
	require.NoError(t, Save(cfg, deepPath))

	_, err := os.Stat(deepPath)
	assert.NoError(t, err, "config file should exist after Save")
}

func TestLoadAppliesDefaults(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	// Write a config with missing fields (zero values)
	data := `{"version": "0.1.0"}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, 512, cfg.CacheSizeMaxMB, "default cacheSizeMaxMB should be applied")
	assert.NotNil(t, cfg.Backends)
}

func TestConfigDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows-specific path logic not testable in this environment")
	}
	dir, err := ConfigDir()
	require.NoError(t, err)
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, "ghostdrive")
}

func TestConfigDirXDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG only applies to non-Windows")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir, err := ConfigDir()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(dir))
	assert.Contains(t, dir, "ghostdrive")
}

func TestConfigPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows-specific path logic not testable in this environment")
	}
	path, err := ConfigPath()
	require.NoError(t, err)
	assert.Equal(t, "config.json", filepath.Base(path))
}

func TestSaveWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod restrictions differ on Windows")
	}
	tmp := t.TempDir()
	// Make directory read-only so Save cannot create the file
	require.NoError(t, os.Chmod(tmp, 0500))
	defer os.Chmod(tmp, 0700) //nolint:errcheck

	path := filepath.Join(tmp, "locked", "config.json")
	err := Save(DefaultConfig(), path)
	assert.Error(t, err, "Save should fail when directory cannot be created")
}

func TestLoadDefaultRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("platform path resolution differs on Windows")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	original := DefaultConfig()
	original.CacheSizeMaxMB = 999
	original.AutoStart = true

	path, err := ConfigPath()
	require.NoError(t, err)
	require.NoError(t, Save(original, path))

	loaded, err := LoadDefault()
	require.NoError(t, err)
	assert.Equal(t, 999, loaded.CacheSizeMaxMB)
	assert.True(t, loaded.AutoStart)
}

func TestSaveDefaultWritesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("platform path resolution differs on Windows")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := DefaultConfig()
	require.NoError(t, SaveDefault(cfg))

	path, err := ConfigPath()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.NoError(t, err, "config file should exist after SaveDefault")
}

func TestSaveRoundTripWithRemotePath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	original := AppConfig{
		Version: "0.2.0",
		Backends: []plugins.BackendConfig{
			{
				ID:         "uuid-test",
				Name:       "NAS WebDAV",
				Type:       "webdav",
				Enabled:    true,
				SyncDir:    "/home/user/ghostdrive",
				RemotePath: "/GhostDrive",
				Params:     map[string]string{"url": "https://nas.local/dav"},
			},
		},
		CacheSizeMaxMB: 256,
	}

	require.NoError(t, Save(original, path))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Backends, 1)
	assert.Equal(t, "/GhostDrive", loaded.Backends[0].RemotePath)
	assert.Equal(t, "uuid-test", loaded.Backends[0].ID)
}
