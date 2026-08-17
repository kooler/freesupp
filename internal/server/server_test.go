package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kooler/freesupp/internal/auth"
	"github.com/kooler/freesupp/internal/config"
	"github.com/kooler/freesupp/internal/mail"
	"github.com/kooler/freesupp/internal/store"
)

// sentEmail records one delivery made through fakeSender.
type sentEmail struct {
	To      string
	Subject string
	Body    string
}

type fakeSender struct {
	mu   sync.Mutex
	err  error
	sent []sentEmail
}

func (f *fakeSender) Send(to, subject, textBody string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentEmail{To: to, Subject: subject, Body: textBody})
	return f.err
}

func (f *fakeSender) messages() []sentEmail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentEmail(nil), f.sent...)
}

// fakeVerifier returns a fixed verification outcome.
type fakeVerifier struct {
	err   error
	token string
	ip    string
}

func (f *fakeVerifier) Verify(_ context.Context, token, remoteIP string) error {
	f.token, f.ip = token, remoteIP
	return f.err
}

type testEnv struct {
	srv      *Server
	store    *store.Store
	sender   *fakeSender
	verifier *fakeVerifier
	notifier *mail.Notifier
	cfg      *config.Config
}

func testConfig(t *testing.T, overrides ...map[string]string) *config.Config {
	t.Helper()
	env := map[string]string{
		"BASE_URL":       "http://localhost:8080",
		"SESSION_SECRET": "test-secret-long-enough",
	}
	for _, o := range overrides {
		for k, v := range o {
			env[k] = v
		}
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return cfg
}

// testPassword is what every seeded user can sign in with.
const testPassword = "Password1"

var (
	testHashOnce sync.Once
	testHash     string
)

// testPasswordHash hashes testPassword once for the whole package — bcrypt is
// deliberately slow and most tests only need some valid hash.
func testPasswordHash(t *testing.T) string {
	t.Helper()
	testHashOnce.Do(func() {
		h, err := auth.HashPassword(testPassword)
		if err != nil {
			t.Fatalf("HashPassword() error = %v", err)
		}
		testHash = h
	})
	return testHash
}

func seedUser(t *testing.T, st *store.Store, email string, isAdmin bool) *store.User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), email, testPasswordHash(t), isAdmin)
	if err != nil {
		t.Fatalf("CreateUser(%q) error = %v", email, err)
	}
	return u
}

// newTestEnv builds a server with two seeded users: ops@example.com (admin)
// and second@example.com. Use newEmptyTestEnv for first-run setup tests.
func newTestEnv(t *testing.T, overrides ...map[string]string) *testEnv {
	env := newEmptyTestEnv(t, overrides...)
	seedUser(t, env.store, "ops@example.com", true)
	seedUser(t, env.store, "second@example.com", false)
	return env
}

func newEmptyTestEnv(t *testing.T, overrides ...map[string]string) *testEnv {
	t.Helper()
	cfg := testConfig(t, overrides...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sender := &fakeSender{}
	verifier := &fakeVerifier{}
	notifier := mail.NewNotifier(cfg, sender, log)

	srv := New(cfg, log, Deps{Store: st, Notifier: notifier, Verifier: verifier})
	// Handler tests should not trip the rate limiter; it has its own tests.
	srv.limiter = newRateLimiter(1000, time.Minute)

	return &testEnv{
		srv:      srv,
		store:    st,
		sender:   sender,
		verifier: verifier,
		notifier: notifier,
		cfg:      cfg,
	}
}

// do runs a request and waits for any notification goroutines it started.
func (e *testEnv) do(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, r)
	e.notifier.Wait()
	return rec
}

func TestPing(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(httptest.NewRequest(http.MethodGet, "/ping", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestUnknownRoute404(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
