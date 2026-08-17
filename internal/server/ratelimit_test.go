package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kooler/freesupp/internal/config"
)

func TestRateLimiterAllow(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(3, time.Minute)
	rl.now = func() time.Time { return now }

	for i := range 3 {
		if ok, _ := rl.allow("1.2.3.4"); !ok {
			t.Fatalf("request %d denied, want allowed within burst", i+1)
		}
	}
	ok, retryAfter := rl.allow("1.2.3.4")
	if ok {
		t.Fatal("request 4 allowed, want denied")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want a positive duration", retryAfter)
	}

	// A different key has its own bucket.
	if ok, _ := rl.allow("5.6.7.8"); !ok {
		t.Error("other client denied, want its own bucket")
	}

	// Tokens refill over time.
	now = now.Add(30 * time.Second)
	if ok, _ := rl.allow("1.2.3.4"); !ok {
		t.Error("request denied after refill window, want allowed")
	}
}

func TestRateLimiterSweepsIdleBuckets(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(1, time.Minute)
	rl.now = func() time.Time { return now }

	rl.allow("stale")
	now = now.Add(2 * bucketTTL)
	rl.allow("fresh")

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if _, ok := rl.buckets["stale"]; ok {
		t.Error("idle bucket was not swept")
	}
	if _, ok := rl.buckets["fresh"]; !ok {
		t.Error("active bucket was swept")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	env := newTestEnv(t)
	env.srv.limiter = newRateLimiter(2, time.Minute)

	payload := map[string]string{"email": "v@example.com", "message": "hi"}
	for i := range 2 {
		if rec := env.do(jsonRequest(http.MethodPost, "/api/messages", payload)); rec.Code != http.StatusCreated {
			t.Fatalf("request %d status = %d, want %d", i+1, rec.Code, http.StatusCreated)
		}
	}

	rec := env.do(jsonRequest(http.MethodPost, "/api/messages", payload))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response is missing a Retry-After header")
	}
	if msg := decodeBody[errorResponse](t, rec).Error; msg == "" {
		t.Error("error response has no message")
	}

	// Reads are not rate limited.
	if rec := env.do(httptest.NewRequest(http.MethodGet, "/api/thread/unknown", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("GET status = %d, want %d (reads must not be limited)", rec.Code, http.StatusNotFound)
	}
}

// The follow-up endpoint writes and fans out email just like /api/messages, so
// it must sit behind the same limiter.
func TestRateLimitCoversThreadFollowUps(t *testing.T) {
	env := newTestEnv(t)
	token := seedConversation(t, env, "v@example.com", "first question")
	env.srv.limiter = newRateLimiter(1, time.Minute)

	path := "/api/thread/" + token + "/messages"
	payload := map[string]string{"message": "any news?"}

	if rec := env.do(jsonRequest(http.MethodPost, path, payload)); rec.Code != http.StatusCreated {
		t.Fatalf("first follow-up status = %d, want %d", rec.Code, http.StatusCreated)
	}
	rec := env.do(jsonRequest(http.MethodPost, path, payload))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second follow-up status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response is missing a Retry-After header")
	}
}

func TestRateLimitIsPerClientBehindAProxy(t *testing.T) {
	env := newTestEnv(t, map[string]string{"USE_PROXY": "true"})
	env.srv.limiter = newRateLimiter(1, time.Minute)
	payload := map[string]string{"email": "v@example.com", "message": "hi"}

	first := jsonRequest(http.MethodPost, "/api/messages", payload)
	first.Header.Set("X-Forwarded-For", "203.0.113.1")
	if rec := env.do(first); rec.Code != http.StatusCreated {
		t.Fatalf("first client status = %d, want %d", rec.Code, http.StatusCreated)
	}

	second := jsonRequest(http.MethodPost, "/api/messages", payload)
	second.Header.Set("X-Forwarded-For", "203.0.113.2")
	if rec := env.do(second); rec.Code != http.StatusCreated {
		t.Fatalf("second client status = %d, want %d", rec.Code, http.StatusCreated)
	}

	again := jsonRequest(http.MethodPost, "/api/messages", payload)
	again.Header.Set("X-Forwarded-For", "203.0.113.1")
	if rec := env.do(again); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("repeat from first client status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

// Without USE_PROXY the header is attacker-controlled, so varying it must not
// hand out a fresh bucket.
func TestRateLimitIgnoresForwardedForByDefault(t *testing.T) {
	env := newTestEnv(t)
	env.srv.limiter = newRateLimiter(1, time.Minute)
	payload := map[string]string{"email": "v@example.com", "message": "hi"}

	first := jsonRequest(http.MethodPost, "/api/messages", payload)
	first.Header.Set("X-Forwarded-For", "203.0.113.1")
	if rec := env.do(first); rec.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, want %d", rec.Code, http.StatusCreated)
	}

	spoofed := jsonRequest(http.MethodPost, "/api/messages", payload)
	spoofed.Header.Set("X-Forwarded-For", "203.0.113.2")
	if rec := env.do(spoofed); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed second request status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

// A proxy may append its own header line instead of extending the client's.
func TestClientIPUsesTheLastForwardedForHeader(t *testing.T) {
	srv := &Server{cfg: &config.Config{UseProxy: true}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Add("X-Forwarded-For", "203.0.113.7")
	r.Header.Add("X-Forwarded-For", "198.51.100.9")

	if got := srv.clientIP(r); got != "198.51.100.9" {
		t.Errorf("clientIP() = %q, want %q", got, "198.51.100.9")
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		useProxy   bool
		remoteAddr string
		xff        string
		want       string
	}{
		{name: "remote addr", remoteAddr: "192.0.2.5:1234", want: "192.0.2.5"},
		{name: "forwarded header ignored by default", remoteAddr: "192.0.2.5:1234", xff: "198.51.100.9", want: "192.0.2.5"},
		{name: "single forwarded when proxied", useProxy: true, remoteAddr: "10.0.0.1:1234", xff: " 198.51.100.9 ", want: "198.51.100.9"},
		// The proxy appends the peer it saw, so only the last entry is ours.
		{name: "chain uses the entry the proxy appended", useProxy: true, remoteAddr: "10.0.0.1:1234", xff: "203.0.113.7, 198.51.100.9", want: "198.51.100.9"},
		{name: "spoofed prefix cannot pick the bucket", useProxy: true, remoteAddr: "10.0.0.1:1234", xff: "1.2.3.4, 5.6.7.8, 198.51.100.9", want: "198.51.100.9"},
		{name: "empty last entry falls back to the peer", useProxy: true, remoteAddr: "10.0.0.1:1234", xff: "198.51.100.9, ", want: "10.0.0.1"},
		{name: "proxy without the header", useProxy: true, remoteAddr: "10.0.0.1:1234", want: "10.0.0.1"},
		{name: "unparsable remote addr", remoteAddr: "pipe", want: "pipe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &Server{cfg: &config.Config{UseProxy: tt.useProxy}}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := srv.clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
