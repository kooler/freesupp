package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrEmailTaken is returned when creating a user with an email that exists.
var ErrEmailTaken = errors.New("store: email already registered")

// ErrSetupDone is returned by CreateFirstAdmin once any user exists.
var ErrSetupDone = errors.New("store: setup already completed")

// User is an operator account. Access is decided solely by this table: a row
// here means the person may sign in, and admins additionally manage users.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

const userColumns = "id, email, password_hash, is_admin, created_at"

// CreateUser inserts a user. The email is stored lowercased so lookups are
// case-insensitive.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, isAdmin bool) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	now := s.now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		email, passwordHash, isAdmin, formatTime(now))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("store: create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: create user id: %w", err)
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, IsAdmin: isAdmin, CreatedAt: now.UTC()}, nil
}

// CreateFirstAdmin inserts the initial admin only while the table is empty,
// so two concurrent setup submissions cannot both win.
func (s *Store) CreateFirstAdmin(ctx context.Context, email, passwordHash string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	now := s.now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, is_admin, created_at)
		 SELECT ?, ?, 1, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		email, passwordHash, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("store: create first admin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: create first admin: %w", err)
	}
	if n == 0 {
		return nil, ErrSetupDone
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: create first admin id: %w", err)
	}
	return &User{ID: id, Email: email, PasswordHash: passwordHash, IsAdmin: true, CreatedAt: now.UTC()}, nil
}

// UserByEmail looks a user up case-insensitively.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = ?`, email))
}

// UserByID returns a single user.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// ListUsers returns every user, oldest first.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// UserEmails returns every user's address (notification recipients).
func (s *Store) UserEmails(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list user emails: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("store: scan user email: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountUsers reports how many accounts exist; zero means setup is pending.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

// CountAdmins backs the guard that keeps the inbox from going admin-less.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count admins: %w", err)
	}
	return n, nil
}

// DeleteUser removes an account; ErrNotFound when id does not exist.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return s.execOnUser(ctx, `DELETE FROM users WHERE id = ?`, id)
}

// SetUserAdmin flips the admin flag.
func (s *Store) SetUserAdmin(ctx context.Context, id int64, isAdmin bool) error {
	return s.execOnUser(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, isAdmin, id)
}

// SetUserPassword replaces the stored hash.
func (s *Store) SetUserPassword(ctx context.Context, id int64, passwordHash string) error {
	return s.execOnUser(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
}

func (s *Store) execOnUser(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanUser(row *sql.Row) (*User, error) {
	u, err := scanUserRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func scanUserRow(row rowScanner) (*User, error) {
	var (
		u       User
		created string
	)
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("store: scan user: %w", err)
	}
	t, err := parseTime(created)
	if err != nil {
		return nil, fmt.Errorf("store: parse user created_at: %w", err)
	}
	u.CreatedAt = t
	return &u, nil
}

// isUniqueViolation detects the driver's UNIQUE constraint error without
// depending on its concrete error type.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
