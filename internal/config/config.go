package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/CCoupel/GhostDrive/plugins"
)

// AppConfig represents the global application configuration (config.json).
type AppConfig struct {
	Version        string                `json:"version"`
	Backends       []plugins.BackendConfig `json:"backends"`
	CacheEnabled   bool                  `json:"cacheEnabled"`
	CacheDir       string                `json:"cacheDir"`
	CacheSizeMaxMB int                   `json:"cacheSizeMaxMB"`
	StartMinimized bool                  `json:"startMinimized"`
	AutoStart      bool                  `json:"autoStart"`
}

// DefaultConfig returns a new AppConfig with sensible defaults.
func DefaultConfig() AppConfig {
	return AppConfig{
		Version:        "0.1.0",
		Backends:       []plugins.BackendConfig{},
		CacheEnabled:   false,
		CacheDir:       "",
		CacheSizeMaxMB: 512,
		StartMinimized: false,
		AutoStart:      false,
	}
}

// ConfigDir returns the platform-specific configuration directory.
func ConfigDir() (string, error) {
	var base string
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", fmt.Errorf("config: APPDATA environment variable not set")
		}
		base = appdata
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: cannot determine home directory: %w", err)
		}
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg != "" {
			base = xdg
		} else {
			base = filepath.Join(home, ".config")
		}
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(base, "GhostDrive"), nil
	}
	return filepath.Join(base, "ghostdrive"), nil
}

// ConfigPath returns the full path to config.json.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads AppConfig from the given file path.
// If the file does not exist, returns DefaultConfig without error.
func Load(path string) (AppConfig, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}

	// Apply defaults for zero values
	if cfg.CacheSizeMaxMB == 0 {
		cfg.CacheSizeMaxMB = 512
	}
	if cfg.Backends == nil {
		cfg.Backends = []plugins.BackendConfig{}
	}

	return cfg, nil
}

// LoadDefault loads the config from the platform default location.
func LoadDefault() (AppConfig, error) {
	path, err := ConfigPath()
	if err != nil {
		return DefaultConfig(), err
	}
	return Load(path)
}

// Save writes AppConfig to the given file path.
// Creates parent directories if they do not exist.
// Never logs passwords from backend configurations.
func Save(cfg AppConfig, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("config: create directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}

	return nil
}

// SaveDefault saves the config to the platform default location.
func SaveDefault(cfg AppConfig) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return Save(cfg, path)
}
