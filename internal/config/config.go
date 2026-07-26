// Package config resolves donezod runtime configuration. Values come from
// CLI flags (with environment-variable fallbacks handled by the CLI layer)
// over the defaults defined here.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// EnvTrustProxy overrides --trust-proxy.
	EnvTrustProxy = "DONEZOD_TRUST_PROXY"
	// EnvDevAutoLogin overrides --dev-auto-login.
	EnvDevAutoLogin = "DONEZOD_DEV_AUTO_LOGIN"
)

// EnvDevAutoLoginConsent must be set to exactly "1" for --dev-auto-login
// to be accepted with a data dir outside /tmp. It is a deliberate
// speed bump: dev auto-login disables authentication entirely.
const EnvDevAutoLoginConsent = "DONEZOD_I_KNOW_WHAT_IM_DOING"

// Config is the resolved donezod runtime configuration.
type Config struct {
	// Port is the HTTP listen port.
	Port int
	// DataDir holds core.db and the spaces/ directory.
	DataDir string
	// SeedPath, when non-empty, is a seed.json to import before serving.
	SeedPath string
	// TrustProxy declares a trusted reverse proxy directly in front of
	// donezod: rate limiting keys on the last X-Forwarded-For hop (the
	// one that proxy appended) instead of the socket address, and
	// X-Forwarded-Proto: https marks session cookies Secure. Leave it
	// off when clients can reach donezod directly — both headers are
	// then attacker-controlled and are ignored.
	TrustProxy bool
	// DevAutoLogin disables authentication and attributes every request
	// to the seeded dev user. It exists purely for frontend development
	// and tests; Validate refuses it unless DataDir is under /tmp or
	// EnvDevAutoLoginConsent is set to "1".
	DevAutoLogin bool
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

// Validate checks the configuration for values that can never work, and
// refuses the dangerous ones. DevAutoLogin is only accepted for
// throwaway data dirs under /tmp, unless EnvDevAutoLoginConsent=1 is
// set in the environment (read here, at validation time).
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d out of range 1-65535", c.Port)
	}
	if c.DataDir == "" {
		return errors.New("config: data dir is required")
	}
	if c.DevAutoLogin && !underTmp(c.DataDir) && os.Getenv(EnvDevAutoLoginConsent) != "1" {
		return fmt.Errorf(
			"config: --dev-auto-login disables authentication and is refused for data dir %s; use a --data-dir under /tmp, or set %s=1 if you really mean it",
			c.DataDir, EnvDevAutoLoginConsent)
	}
	return nil
}

// underTmp reports whether dir resolves to /tmp or below.
func underTmp(dir string) bool {
	clean := filepath.Clean(dir)
	return clean == "/tmp" || strings.HasPrefix(clean, "/tmp"+string(filepath.Separator))
}
