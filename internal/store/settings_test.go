package store

import (
	"context"
	"errors"
	"testing"
)

func TestGetUserSettingsDefaultsWhenUnset(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Never written: absence is a normal resting state, not ErrNotFound.
	got, err := s.GetUserSettings(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserSettings: %v", err)
	}
	if got != (UserSettings{}) {
		t.Errorf("unset settings = %+v, want zero value", got)
	}

	// A user that does not exist reads the same way — settings are not a
	// probe for whether an account exists.
	got, err = s.GetUserSettings(ctx, user.ID+999)
	if err != nil {
		t.Fatalf("GetUserSettings for unknown user: %v", err)
	}
	if got != (UserSettings{}) {
		t.Errorf("unknown-user settings = %+v, want zero value", got)
	}
}

func TestPatchUserSettings(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("apply refused")
	tests := []struct {
		name    string
		seed    *UserSettings
		apply   func(*UserSettings) error
		wantErr error
		want    UserSettings
	}{
		{
			name:  "first write creates the row",
			apply: func(s *UserSettings) error { s.Theme = "paper"; return nil },
			want:  UserSettings{Theme: "paper"},
		},
		{
			name:  "patch leaves untouched fields alone",
			seed:  &UserSettings{Theme: "paper", Font: "inter"},
			apply: func(s *UserSettings) error { s.FontSize = "large"; return nil },
			want:  UserSettings{Theme: "paper", Font: "inter", FontSize: "large"},
		},
		{
			name:  "empty value clears a preference",
			seed:  &UserSettings{Theme: "paper", Font: "inter"},
			apply: func(s *UserSettings) error { s.Theme = ""; return nil },
			want:  UserSettings{Font: "inter"},
		},
		{
			name:    "an apply error aborts the patch and is returned unchanged",
			seed:    &UserSettings{Theme: "paper"},
			apply:   func(s *UserSettings) error { s.Theme = "slate"; return sentinel },
			wantErr: sentinel,
			want:    UserSettings{Theme: "paper"},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestCoreStore(t)
			ctx := context.Background()
			user, err := s.CreateUser(ctx, "ben", "Ben")
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			if tt.seed != nil {
				seed := *tt.seed
				if _, err := s.PatchUserSettings(ctx, user.ID, func(cur *UserSettings) error {
					*cur = seed
					return nil
				}); err != nil {
					t.Fatalf("seed settings: %v", err)
				}
			}

			got, err := s.PatchUserSettings(ctx, user.ID, tt.apply)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("PatchUserSettings: %v", err)
				}
				if got != tt.want {
					t.Errorf("returned settings = %+v, want %+v", got, tt.want)
				}
			}

			// Always verify against the store: the returned value must not
			// diverge from what was committed (or, on an aborted patch, from
			// what was there before).
			stored, err := s.GetUserSettings(ctx, user.ID)
			if err != nil {
				t.Fatalf("GetUserSettings: %v", err)
			}
			if stored != tt.want {
				t.Errorf("stored settings = %+v, want %+v", stored, tt.want)
			}
		})
	}
}

func TestPatchUserSettingsUnknownUser(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	if _, err := s.PatchUserSettings(context.Background(), 999, func(*UserSettings) error {
		return nil
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// Deleting a user must not require the caller to clear settings first — the
// seed cleanup path deletes users directly and foreign keys are enforced, so
// a missing ON DELETE CASCADE would break it.
func TestDeleteUserCascadesSettings(t *testing.T) {
	t.Parallel()
	s := newTestCoreStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "ben", "Ben")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := s.PatchUserSettings(ctx, user.ID, func(cur *UserSettings) error {
		cur.Theme = "paper"
		return nil
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := s.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser with settings present: %v", err)
	}
	var rows int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_settings WHERE user_id = ?`, user.ID).Scan(&rows); err != nil {
		t.Fatalf("count settings rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("settings rows after user delete = %d, want 0", rows)
	}
}

// Two concurrent patches of different preferences must both survive: the
// read and write share a transaction, so neither writes back a stale copy.
func TestPatchUserSettingsConcurrentFieldsBothSurvive(t *testing.T) {
	t.Parallel()
	const rounds = 40
	s := newTestCoreStore(t)
	ctx := context.Background()

	for round := 0; round < rounds; round++ {
		user, err := s.CreateUser(ctx, "ben"+itoa(round), "Ben")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		start := make(chan struct{})
		errs := make([]error, 2)
		done := make(chan struct{}, 2)
		go func() {
			<-start
			_, errs[0] = s.PatchUserSettings(ctx, user.ID, func(cur *UserSettings) error {
				cur.Theme = "paper"
				return nil
			})
			done <- struct{}{}
		}()
		go func() {
			<-start
			_, errs[1] = s.PatchUserSettings(ctx, user.ID, func(cur *UserSettings) error {
				cur.Font = "inter"
				return nil
			})
			done <- struct{}{}
		}()
		close(start)
		<-done
		<-done
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: patch %d: %v", round, i, err)
			}
		}

		got, err := s.GetUserSettings(ctx, user.ID)
		if err != nil {
			t.Fatalf("round %d: GetUserSettings: %v", round, err)
		}
		if got.Theme != "paper" || got.Font != "inter" {
			t.Fatalf("round %d: settings = %+v, want both theme and font preserved", round, got)
		}
	}
}

// itoa avoids pulling strconv in for a single test helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
