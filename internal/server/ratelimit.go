package server

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rate limits for the public POST endpoints.
const (
	defaultRateBurst  = 5
	defaultRateWindow = time.Minute
	// bucketTTL controls how long an idle bucket is kept before being swept.
	bucketTTL = 10 * time.Minute
)

type bucket struct {
	tokens float64
	seen   time.Time
}

// rateLimiter is an in-memory per-key token bucket. Keys are client IPs.
// State is per-process; a restart resets it, which is fine for spam control.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	burst  float64
	refill float64 // tokens per second
	now    func() time.Time

	lastSweep time.Time
}

// newRateLimiter allows burst requests, refilled evenly over window.
func newRateLimiter(burst int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*bucket),
		burst:   float64(burst),
		refill:  float64(burst) / window.Seconds(),
		now:     time.Now,
	}
}

// allow consumes one token for key, reporting whether the request may proceed
// and how long to wait when it may not.
func (rl *rateLimiter) allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweep(now)

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.burst, seen: now}
		rl.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.seen).Seconds() * rl.refill
		if b.tokens > rl.burst {
			b.tokens = rl.burst
		}
		b.seen = now
	}

	if b.tokens < 1 {
		return false, time.Duration((1-b.tokens)/rl.refill*float64(time.Second)) + time.Second
	}
	b.tokens--
	return true, 0
}

// sweep drops buckets that have been idle long enough to be fully refilled.
func (rl *rateLimiter) sweep(now time.Time) {
	if now.Sub(rl.lastSweep) < bucketTTL {
		return
	}
	rl.lastSweep = now
	for k, b := range rl.buckets {
		if now.Sub(b.seen) > bucketTTL {
			delete(rl.buckets, k)
		}
	}
}

// rateLimit rejects requests over the per-IP limit with 429.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, retryAfter := s.limiter.allow(s.clientIP(r))
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many requests, please try again in a moment")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP identifies the caller. X-Forwarded-For is honoured only when
// USE_PROXY says a reverse proxy is the sole route to this process — and even
// then only its last entry, the one the proxy itself appended; otherwise any
// client could mint a fresh rate-limit bucket per request just by varying the
// header. The result is used for rate limiting and as the
// optional remoteip hint to Turnstile, never for auth.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.UseProxy {
		if xff := forwardedFor(r); xff != "" {
			return xff
		}
	}
	return peerIP(r)
}

// forwardedFor returns the last X-Forwarded-For entry, if any. Proxies append
// the peer they saw to whatever the client sent, so only the final entry was
// written by our own proxy — everything to its left is client-supplied and
// forgeable. Taking the leftmost entry would let anyone pick their own bucket.
func forwardedFor(r *http.Request) string {
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return ""
	}
	// A proxy may append a second header line rather than extend the first.
	last := values[len(values)-1]
	if i := strings.LastIndexByte(last, ','); i >= 0 {
		last = last[i+1:]
	}
	return strings.TrimSpace(last)
}

// peerIP is the address of the socket that made the request.
func peerIP(r *http.Request) string {
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
