// Package auth handles operator identity: password hashing and signed session
// cookies. There is no session table — the cookie is the session.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SessionTTL is how long an operator stays logged in.
const SessionTTL = 30 * 24 * time.Hour

// ErrInvalidSession covers every unusable cookie: malformed, tampered or expired.
var ErrInvalidSession = errors.New("auth: invalid session")

// Session is the content of an operator's cookie.
type Session struct {
	Email     string
	ExpiresAt time.Time
}

// SignSession encodes and signs a session as "<payload>.<signature>".
func SignSession(secret string, s Session) string {
	payload := base64.RawURLEncoding.EncodeToString(
		fmt.Appendf(nil, "%s|%d", s.Email, s.ExpiresAt.Unix()),
	)
	return payload + "." + sign(secret, payload)
}

// ParseSession verifies the signature and expiry of a cookie value.
func ParseSession(secret, raw string, now time.Time) (Session, error) {
	payload, sig, ok := strings.Cut(raw, ".")
	if !ok || payload == "" || sig == "" {
		return Session{}, fmt.Errorf("%w: malformed", ErrInvalidSession)
	}
	if !hmac.Equal([]byte(sig), []byte(sign(secret, payload))) {
		return Session{}, fmt.Errorf("%w: bad signature", ErrInvalidSession)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Session{}, fmt.Errorf("%w: undecodable payload", ErrInvalidSession)
	}
	email, expRaw, ok := strings.Cut(string(decoded), "|")
	if !ok || email == "" {
		return Session{}, fmt.Errorf("%w: malformed payload", ErrInvalidSession)
	}
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil {
		return Session{}, fmt.Errorf("%w: malformed expiry", ErrInvalidSession)
	}

	s := Session{Email: email, ExpiresAt: time.Unix(exp, 0).UTC()}
	if !now.Before(s.ExpiresAt) {
		return Session{}, fmt.Errorf("%w: expired", ErrInvalidSession)
	}
	return s, nil
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
