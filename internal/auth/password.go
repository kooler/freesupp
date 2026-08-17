package auth

import (
	"errors"
	"fmt"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// Password complexity bounds. The upper bound exists because bcrypt ignores
// everything past 72 bytes, which would silently weaken longer passwords.
const (
	MinPasswordLen      = 8
	maxPasswordBytes    = 72
	passwordRequirement = "password must be at least 8 characters and contain an uppercase letter, a lowercase letter and a digit"
)

// ErrWrongPassword is returned by CheckPassword on any mismatch.
var ErrWrongPassword = errors.New("auth: wrong password")

// dummyHash is compared against when the account does not exist, so a login
// attempt costs the same time whether or not the email is registered.
var dummyHash = func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}()

// ValidatePassword enforces the complexity rule; the returned error message is
// safe to show to the user.
func ValidatePassword(pw string) error {
	if len(pw) > maxPasswordBytes {
		return fmt.Errorf("password must be at most %d characters", maxPasswordBytes)
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if len([]rune(pw)) < MinPasswordLen || !hasUpper || !hasLower || !hasDigit {
		return errors.New(passwordRequirement)
	}
	return nil
}

// HashPassword derives the stored bcrypt hash.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword verifies pw against the stored hash. Pass an empty hash for
// unknown accounts: a dummy comparison still runs, keeping response time even.
func CheckPassword(hash, pw string) error {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(pw))
		return ErrWrongPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		return ErrWrongPassword
	}
	return nil
}
