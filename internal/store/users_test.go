package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndLookupUser(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, " Alice@Example.COM ", "hash-1", true)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if created.Email != "alice@example.com" {
		t.Errorf("Email = %q, want normalized lowercase", created.Email)
	}
	if !created.IsAdmin {
		t.Error("IsAdmin = false, want true")
	}

	// Lookup is case-insensitive because the stored value is normalized.
	got, err := s.UserByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("UserByEmail() error = %v", err)
	}
	if got.ID != created.ID || got.PasswordHash != "hash-1" || !got.IsAdmin {
		t.Errorf("UserByEmail() = %+v, want the created user", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	byID, err := s.UserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("UserByID() error = %v", err)
	}
	if byID.Email != "alice@example.com" {
		t.Errorf("UserByID().Email = %q", byID.Email)
	}

	if _, err := s.UserByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByEmail(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "a@example.com", "h", false); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.CreateUser(ctx, "A@Example.com", "h", false); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate CreateUser() error = %v, want ErrEmailTaken", err)
	}
}

func TestCreateFirstAdmin(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	admin, err := s.CreateFirstAdmin(ctx, "First@example.com", "h")
	if err != nil {
		t.Fatalf("CreateFirstAdmin() error = %v", err)
	}
	if !admin.IsAdmin {
		t.Error("first user IsAdmin = false, want true")
	}
	if admin.Email != "first@example.com" {
		t.Errorf("Email = %q, want normalized", admin.Email)
	}

	// Once anyone exists, setup is closed — even for a different email.
	if _, err := s.CreateFirstAdmin(ctx, "second@example.com", "h"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second CreateFirstAdmin() error = %v, want ErrSetupDone", err)
	}
}

func TestListUsersAndCounts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers() = %d, %v; want 0, nil", n, err)
	}

	a, _ := s.CreateUser(ctx, "a@example.com", "h", true)
	b, _ := s.CreateUser(ctx, "b@example.com", "h", false)

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 || users[0].ID != a.ID || users[1].ID != b.ID {
		t.Errorf("ListUsers() = %+v, want [a, b] oldest first", users)
	}

	emails, err := s.UserEmails(ctx)
	if err != nil {
		t.Fatalf("UserEmails() error = %v", err)
	}
	if len(emails) != 2 || emails[0] != "a@example.com" || emails[1] != "b@example.com" {
		t.Errorf("UserEmails() = %v", emails)
	}

	if n, _ := s.CountUsers(ctx); n != 2 {
		t.Errorf("CountUsers() = %d, want 2", n)
	}
	if n, _ := s.CountAdmins(ctx); n != 1 {
		t.Errorf("CountAdmins() = %d, want 1", n)
	}
}

func TestUpdateAndDeleteUser(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "a@example.com", "old-hash", false)

	if err := s.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatalf("SetUserAdmin() error = %v", err)
	}
	if err := s.SetUserPassword(ctx, u.ID, "new-hash"); err != nil {
		t.Fatalf("SetUserPassword() error = %v", err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if !got.IsAdmin || got.PasswordHash != "new-hash" {
		t.Errorf("user after updates = %+v", got)
	}

	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if _, err := s.UserByID(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID(deleted) error = %v, want ErrNotFound", err)
	}

	// Every mutation on a missing id reports ErrNotFound.
	if err := s.DeleteUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser(missing) error = %v, want ErrNotFound", err)
	}
	if err := s.SetUserAdmin(ctx, u.ID, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetUserAdmin(missing) error = %v, want ErrNotFound", err)
	}
	if err := s.SetUserPassword(ctx, u.ID, "h"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetUserPassword(missing) error = %v, want ErrNotFound", err)
	}
}
