package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kooler/freesupp/internal/auth"
)

// cookieByName returns the named Set-Cookie from a response, or nil.
func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// login signs the seeded admin in and returns the session cookie.
func login(t *testing.T, env *testEnv) *http.Cookie {
	t.Helper()
	return passwordLogin(t, env, "ops@example.com", testPassword)
}

func TestSessionCookieAttributes(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	if !sess.HttpOnly || sess.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie = %+v, want HttpOnly + SameSite=Lax", sess)
	}
	if sess.MaxAge != int(auth.SessionTTL.Seconds()) {
		t.Errorf("session MaxAge = %d, want %d", sess.MaxAge, int(auth.SessionTTL.Seconds()))
	}
	if sess.Secure {
		t.Error("session cookie is Secure on an http BASE_URL; the browser would never send it back")
	}

	parsed, err := auth.ParseSession(env.cfg.SessionSecret, sess.Value, time.Now())
	if err != nil {
		t.Fatalf("ParseSession() error = %v", err)
	}
	if parsed.Email != "ops@example.com" {
		t.Errorf("session email = %q, want %q", parsed.Email, "ops@example.com")
	}
}

func TestSessionCookieIsSecureOnHTTPS(t *testing.T) {
	env := newTestEnv(t, map[string]string{"BASE_URL": "https://support.example.com"})
	if sess := login(t, env); !sess.Secure {
		t.Errorf("session cookie = %+v, want Secure on an https BASE_URL", sess)
	}
}

func TestMeReturnsOperator(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sess)
	rec := env.do(req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"is_admin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Email != "ops@example.com" || !body.IsAdmin {
		t.Errorf("me = %+v, want the seeded admin", body)
	}
}

func TestRequireOperatorRejects(t *testing.T) {
	env := newTestEnv(t)
	valid := login(t, env)

	expired := auth.SignSession(env.cfg.SessionSecret, auth.Session{
		Email: "ops@example.com", ExpiresAt: time.Now().Add(-time.Minute),
	})
	removed := auth.SignSession(env.cfg.SessionSecret, auth.Session{
		Email: "gone@example.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	foreign := auth.SignSession("another-secret", auth.Session{
		Email: "ops@example.com", ExpiresAt: time.Now().Add(time.Hour),
	})

	tests := []struct {
		name   string
		cookie *http.Cookie
	}{
		{name: "no cookie"},
		{name: "garbage cookie", cookie: &http.Cookie{Name: sessionCookie, Value: "nonsense"}},
		{name: "tampered cookie", cookie: &http.Cookie{Name: sessionCookie, Value: valid.Value + "x"}},
		{name: "expired cookie", cookie: &http.Cookie{Name: sessionCookie, Value: expired}},
		{name: "wrong secret", cookie: &http.Cookie{Name: sessionCookie, Value: foreign}},
		{name: "user removed from the database", cookie: &http.Cookie{Name: sessionCookie, Value: removed}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := env.do(req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			var body errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error == "" {
				t.Error("401 response has no error message")
			}
		})
	}
}

func TestLogoutClearsSession(t *testing.T) {
	env := newTestEnv(t)
	sess := login(t, env)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(sess)
	rec := env.do(req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	cleared := cookieByName(rec, sessionCookie)
	if cleared == nil {
		t.Fatal("logout did not set a session cookie")
	}
	if cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Errorf("session cookie = %+v, want empty value with MaxAge < 0", cleared)
	}
}

// The public visitor API must keep working without a session.
func TestPublicRoutesStayAnonymous(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(httptest.NewRequest(http.MethodGet, "/api/thread/unknown-token", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
