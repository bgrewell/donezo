package config

import (
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
