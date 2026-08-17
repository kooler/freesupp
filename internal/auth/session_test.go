package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

const secret = "s3cr3t"

func TestSignParseRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	want := Session{Email: "ops@example.com", ExpiresAt: now.Add(SessionTTL)}

	got, err := ParseSession(secret, SignSession(secret, want), now)
	if err != nil {
		t.Fatalf("ParseSession() error = %v", err)
	}
	if got.Email != want.Email {
		t.Errorf("Email = %q, want %q", got.Email, want.Email)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestParseSessionRejects(t *testing.T) {
	now := time.Now().UTC()
	valid := SignSession(secret, Session{Email: "ops@example.com", ExpiresAt: now.Add(time.Hour)})
	payload, sig, _ := strings.Cut(valid, ".")

	tests := []struct {
		name   string
		secret string
		value  string
		now    time.Time
	}{
		{name: "empty", secret: secret, value: "", now: now},
		{name: "no signature", secret: secret, value: payload, now: now},
		{name: "wrong secret", secret: "other", value: valid, now: now},
		{name: "tampered signature", secret: secret, value: payload + "." + flip(sig), now: now},
		{name: "tampered payload", secret: secret, value: flip(payload) + "." + sig, now: now},
		{name: "undecodable payload", secret: secret, value: "!!!." + sign(secret, "!!!"), now: now},
		{name: "missing expiry", secret: secret, value: signedPayload(secret, "ops@example.com"), now: now},
		{name: "non-numeric expiry", secret: secret, value: signedPayload(secret, "ops@example.com|soon"), now: now},
		{name: "empty email", secret: secret, value: signedPayload(secret, "|123"), now: now},
		{name: "expired", secret: secret, value: valid, now: now.Add(2 * time.Hour)},
		{name: "expiring exactly now", secret: secret, value: valid, now: now.Add(time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseSession(tt.secret, tt.value, tt.now); !errors.Is(err, ErrInvalidSession) {
				t.Errorf("ParseSession() error = %v, want ErrInvalidSession", err)
			}
		})
	}
}

// signedPayload correctly signs an arbitrary payload body.
func signedPayload(secret, raw string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	return encoded + "." + sign(secret, encoded)
}

func flip(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
