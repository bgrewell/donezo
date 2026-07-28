package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDataDir(t *testing.T) {
	tests := []struct {
		name string
		xdg  string
		home string
		want string
	}{
		{
			name: "XDG_DATA_HOME set",
			xdg:  "/custom/data",
			home: "/home/tester",
			want: filepath.Join("/custom/data", "donezo"),
		},
		{
			name: "XDG_DATA_HOME unset falls back to ~/.local/share",
			xdg:  "",
			home: "/home/tester",
			want: filepath.Join("/home/tester", ".local", "share", "donezo"),
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", tt.xdg)
			t.Setenv("HOME", tt.home)
			got, err := DefaultDataDir()
			if err != nil {
				t.Fatalf("DefaultDataDir: %v", err)
			}
			if got != tt.want {
				t.Errorf("DefaultDataDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "valid", cfg: Config{Port: 8787, DataDir: "/tmp/x"}},
		{name: "valid with seed", cfg: Config{Port: 8791, DataDir: "/tmp/x", SeedPath: "seed.json"}},
		{name: "port zero", cfg: Config{Port: 0, DataDir: "/tmp/x"}, wantErr: true},
		{name: "port negative", cfg: Config{Port: -1, DataDir: "/tmp/x"}, wantErr: true},
		{name: "port too high", cfg: Config{Port: 65536, DataDir: "/tmp/x"}, wantErr: true},
		{name: "port boundary low", cfg: Config{Port: 1, DataDir: "/tmp/x"}},
		{name: "port boundary high", cfg: Config{Port: 65535, DataDir: "/tmp/x"}},
		{name: "missing data dir", cfg: Config{Port: 8787}, wantErr: true},
		{name: "trust proxy is always fine", cfg: Config{Port: 8787, DataDir: "/var/lib/donezo", TrustProxy: true}},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigValidateDevAutoLogin covers the safety gate on the
// authentication bypass. Not parallel: it manipulates the consent
// environment variable via t.Setenv.
func TestConfigValidateDevAutoLogin(t *testing.T) {
	tests := []struct {
		name    string
		dataDir string
		consent string // value for EnvDevAutoLoginConsent; "" = unset
		wantErr bool
	}{
		{name: "under /tmp is allowed", dataDir: "/tmp/donezo-dev"},
		{name: "/tmp itself is allowed", dataDir: "/tmp"},
		{name: "unclean path under /tmp is allowed", dataDir: "/tmp/../tmp/donezo-dev"},
		{name: "outside /tmp is refused", dataDir: "/home/ben/.local/share/donezo", wantErr: true},
		{name: "/tmpfoo does not count as /tmp", dataDir: "/tmpfoo/data", wantErr: true},
		{name: "escaping /tmp via .. is refused", dataDir: "/tmp/../etc/donezo", wantErr: true},
		{name: "consent env allows outside /tmp", dataDir: "/home/ben/.local/share/donezo", consent: "1"},
		{name: "consent env must be exactly 1", dataDir: "/home/ben/.local/share/donezo", consent: "yes", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvDevAutoLoginConsent, tt.consent)
			if tt.consent == "" {
				// t.Setenv("", "") still sets an empty value; explicitly
				// unset to model a clean environment.
				if err := os.Unsetenv(EnvDevAutoLoginConsent); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}
			cfg := Config{Port: 8787, DataDir: tt.dataDir, DevAutoLogin: true}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
