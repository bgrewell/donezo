package store

import (
	"path/filepath"
	"testing"
)

func TestOpenDBPoolLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
	}{
		{name: "core-style database", file: "core.db"},
		{name: "space-style database", file: "sandbox.db"},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, err := openDB(filepath.Join(t.TempDir(), tt.file))
			if err != nil {
				t.Fatalf("openDB: %v", err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			})
			// SQLite is single-writer: the pool must be capped at one
			// long-lived connection or concurrent requests degrade into
			// SQLITE_BUSY contention and connection churn.
			if got := db.Stats().MaxOpenConnections; got != 1 {
				t.Errorf("MaxOpenConnections = %d, want 1", got)
			}
		})
	}
}

func TestValidateSpaceID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "simple slug", id: "sandbox"},
		{name: "dash and underscore", id: "my-space_2"},
		{name: "single char", id: "a"},
		{name: "empty", id: "", wantErr: true},
		{name: "path traversal", id: "../escape", wantErr: true},
		{name: "uppercase", id: "Sandbox", wantErr: true},
		{name: "leading dash", id: "-x", wantErr: true},
		{name: "slash", id: "a/b", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSpaceID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSpaceID(%q) err = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}
