// Package config resolves donezod runtime configuration. Values come from
// CLI flags (with environment-variable fallbacks handled by the CLI layer)
// over the defaults defined here.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPort is the default HTTP listen port.
const DefaultPort = 8787

// Environment variable names honored as flag fallbacks by the CLI.
const (
	// EnvPort overrides --port.
	EnvPort = "DONEZOD_PORT"
	// EnvDataDir overrides --data-dir.
	EnvDataDir = "DONEZOD_DATA_DIR"
	// EnvSeed overrides --seed.
	EnvSeed = "DONEZOD_SEED"
)

// Config is the resolved donezod runtime configuration.
type Config struct {
	// Port is the HTTP listen port.
	Port int
	// DataDir holds core.db and the spaces/ directory.
	DataDir string
	// SeedPath, when non-empty, is a seed.json to import before serving.
	SeedPath string
}

// DefaultDataDir returns the XDG data directory for donezo:
// $XDG_DATA_HOME/donezo when set, otherwise ~/.local/share/donezo.
func DefaultDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "donezo"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "donezo"), nil
}

// Validate checks the configuration for values that can never work.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d out of range 1-65535", c.Port)
	}
	if c.DataDir == "" {
		return errors.New("config: data dir is required")
	}
	return nil
}
