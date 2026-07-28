package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// newTestCoreStore builds a CoreStore in a temp dir with a fixed clock.
func newTestCoreStore(t *testing.T) *CoreStore {
	t.Helper()
	s, err := NewCoreStore(WithDataDir(t.TempDir()), WithClock(fixedClock))
	if err != nil {
		t.Fatalf("NewCoreStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close core store: %v", err)
		}
	})
	return s
}

func TestNewCoreStoreRequiresDataDir(t *testing.T) {
	t.Parallel()
	if _, err := NewCoreStore(); err == nil {
		t.Fatal("want error without WithDataDir")
	}
}

func TestCreateUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		username    string
		displayName string
		setup       func(t *testing.T, s *CoreStore)
		wantErr     bool
	}{
		{name: "happy path", username: "ben", displayName: "Ben"},
		{name: "empty username", username: "", displayName: "Nobody", wantErr: true},
		{
			name:     "duplicate username",
			username: "ben",
			setup: func(t *testing.T, s *CoreStore) {
				t.Helper()
				if _, err := s.CreateUser(context.Background(), "ben", "Ben"); err != nil {
					t.Fatalf("setup user: %v", err)
				}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			if tt.setup != nil {
				tt.setup(t, s)
			}
			u, err := s.CreateUser(context.Background(), tt.username, tt.displayName)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			if u.ID == 0 {
				t.Error("user ID not assigned")
			}
			if u.CreatedAt != fixedNow {
				t.Errorf("CreatedAt = %q, want %q", u.CreatedAt, fixedNow)
			}
			got, err := s.GetUserByUsername(context.Background(), tt.username)
			if err != nil {
				t.Fatalf("GetUserByUsername: %v", err)
			}
			if got != u {
				t.Errorf("round-trip mismatch: got %+v, want %+v", got, u)
			}
			if got.PasswordHash != "" {
				t.Errorf("PasswordHash = %q, want empty (phase 2 sets it)", got.PasswordHash)
			}
		})
	}
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		setup        func(t *testing.T, s *CoreStore) int64 // returns id to delete
		wantNotFound bool
	}{
		{
			name: "existing user",
			setup: func(t *testing.T, s *CoreStore) int64 {
				t.Helper()
				u, err := s.CreateUser(context.Background(), "ben", "Ben")
				if err != nil {
					t.Fatalf("setup user: %v", err)
				}
				return u.ID
			},
		},
		{
			name:         "missing user",
			setup:        func(*testing.T, *CoreStore) int64 { return 999 },
			wantNotFound: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			id := tt.setup(t, s)
			err := s.DeleteUser(context.Background(), id)
			if tt.wantNotFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}
			if _, err := s.GetUserByUsername(context.Background(), "ben"); !errors.Is(err, ErrNotFound) {
				t.Errorf("user still present after delete: err = %v", err)
			}
		})
	}
}

func TestDeleteSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		id           string
		setup        bool // create the space first
		wantNotFound bool
	}{
		{name: "existing space", id: "sandbox", setup: true},
		{name: "missing space", id: "ghost", wantNotFound: true},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			ctx := context.Background()
			if tt.setup {
				u, err := s.CreateUser(ctx, "ben", "Ben")
				if err != nil {
					t.Fatalf("setup user: %v", err)
				}
				if _, err := s.CreateSpace(ctx, Space{ID: tt.id, UserID: u.ID, Name: "Sandbox", Color: "blue"}); err != nil {
					t.Fatalf("setup space: %v", err)
				}
			}
			err := s.DeleteSpace(ctx, tt.id)
			if tt.wantNotFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeleteSpace: %v", err)
			}
			if _, err := s.GetSpace(ctx, tt.id); !errors.Is(err, ErrNotFound) {
				t.Errorf("space still present after delete: err = %v", err)
			}
		})
	}
}

func TestGetUserByUsernameNotFound(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	_, err := s.GetUserByUsername(context.Background(), "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		space   Space
		setup   func(t *testing.T, s *CoreStore, userID int64)
		wantErr bool
	}{
		{name: "happy path", space: Space{ID: "sandbox", Name: "Sandbox", Color: "blue"}},
		{name: "invalid id: path traversal", space: Space{ID: "../evil", Name: "X", Color: "blue"}, wantErr: true},
		{name: "invalid id: empty", space: Space{ID: "", Name: "X", Color: "blue"}, wantErr: true},
		{name: "invalid id: uppercase", space: Space{ID: "Sandbox", Name: "X", Color: "blue"}, wantErr: true},
		{name: "invalid id: too long", space: Space{ID: strings.Repeat("a", 65), Name: "X", Color: "blue"}, wantErr: true},
		{name: "boundary id: 64 chars", space: Space{ID: strings.Repeat("a", 64), Name: "X", Color: "blue"}},
		{name: "missing name", space: Space{ID: "sandbox", Color: "blue"}, wantErr: true},
		{
			name:  "duplicate id",
			space: Space{ID: "sandbox", Name: "Sandbox", Color: "blue"},
			setup: func(t *testing.T, s *CoreStore, userID int64) {
				t.Helper()
				if _, err := s.CreateSpace(context.Background(), Space{ID: "sandbox", UserID: userID, Name: "First", Color: "blue"}); err != nil {
					t.Fatalf("setup space: %v", err)
				}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			u, err := s.CreateUser(context.Background(), "ben", "Ben")
			if err != nil {
				t.Fatalf("setup user: %v", err)
			}
			if tt.setup != nil {
				tt.setup(t, s, u.ID)
			}
			tt.space.UserID = u.ID
			got, err := s.CreateSpace(context.Background(), tt.space)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateSpace: %v", err)
			}
			if got.CreatedAt != fixedNow {
				t.Errorf("CreatedAt = %q, want %q", got.CreatedAt, fixedNow)
			}
			fetched, err := s.GetSpace(context.Background(), tt.space.ID)
			if err != nil {
				t.Fatalf("GetSpace: %v", err)
			}
			if fetched != got {
				t.Errorf("round-trip mismatch: got %+v, want %+v", fetched, got)
			}
		})
	}
}

func TestCreateSpaceUnknownUser(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	_, err := s.CreateSpace(context.Background(),
		Space{ID: "sandbox", UserID: 999, Name: "Sandbox", Color: "blue"})
	if err == nil {
		t.Fatal("want foreign key error for unknown user, got nil")
	}
}

func TestGetSpaceNotFound(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	_, err := s.GetSpace(context.Background(), "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListSpaces(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("setup user: %v", err)
	}

	empty, err := s.ListSpaces(ctx)
	if err != nil {
		t.Fatalf("ListSpaces (empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list length = %d, want 0", len(empty))
	}

	for _, sp := range []Space{
		{ID: "zeta", UserID: u.ID, Name: "Zeta", Color: "green", Position: 1},
		{ID: "alpha", UserID: u.ID, Name: "Alpha", Color: "blue", Position: 0},
	} {
		if _, err := s.CreateSpace(ctx, sp); err != nil {
			t.Fatalf("setup space %s: %v", sp.ID, err)
		}
	}
	got, err := s.ListSpaces(ctx)
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list length = %d, want 2", len(got))
	}
	if got[0].ID != "alpha" || got[1].ID != "zeta" {
		t.Errorf("order = [%s, %s], want [alpha, zeta] (by position)", got[0].ID, got[1].ID)
	}
}
