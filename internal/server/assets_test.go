package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/kooler/freesupp/web"
)

// firstAsset returns a built asset path (e.g. "assets/index-abc123.js") from an
// embedded app, so tests do not hard-code Vite's content hashes.
func firstAsset(t *testing.T, fsys fs.FS, ext string) string {
	t.Helper()
	entries, err := fs.ReadDir(fsys, "assets")
	if err != nil {
		t.Fatalf("reading assets dir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && path.Ext(e.Name()) == ext {
			return "assets/" + e.Name()
		}
	}
	t.Fatalf("no %s asset in the embedded build", ext)
	return ""
}

func TestSPAFallbackRoutes(t *testing.T) {
	env := newTestEnv(t)

	visitorIndex := string(mustRead(t, web.VisitorApp, "index.html"))
	inboxIndex := string(mustRead(t, web.InboxApp, "index.html"))

	tests := []struct {
		name string
		path string
		want string
	}{
		{"widget", "/widget/", visitorIndex},
		{"widget without slash", "/widget", visitorIndex},
		{"magic link", "/t/deadbeef", visitorIndex},
		{"inbox root", "/", inboxIndex},
		{"inbox conversation", "/conversations/42", inboxIndex},
		{"inbox conversations", "/conversations", inboxIndex},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got, want := rec.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}
			if got := rec.Header().Get("Cache-Control"); got != indexCacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, indexCacheControl)
			}
			if rec.Header().Get("ETag") == "" {
				t.Error("ETag is empty")
			}
			if body := rec.Body.String(); body != tc.want {
				t.Errorf("body does not match the embedded index.html:\n%s", body)
			}
		})
	}
}

// The widget iframe loads the visitor app cross-origin; the inbox must not be
// framable at all.
func TestSPAFramingHeaders(t *testing.T) {
	env := newTestEnv(t)

	visitor := env.do(httptest.NewRequest(http.MethodGet, "/widget/", nil))
	if got := visitor.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("visitor X-Frame-Options = %q, want it unset", got)
	}
	inbox := env.do(httptest.NewRequest(http.MethodGet, "/", nil))
	if got, want := inbox.Header().Get("X-Frame-Options"), "DENY"; got != want {
		t.Errorf("inbox X-Frame-Options = %q, want %q", got, want)
	}

	// /inbox/index.html reaches the same shell through the asset route, so the
	// refusal must hold there too or it is one URL away from bypassed.
	direct := env.do(httptest.NewRequest(http.MethodGet, "/inbox/index.html", nil))
	if direct.Code != http.StatusOK {
		t.Fatalf("/inbox/index.html status = %d, want %d", direct.Code, http.StatusOK)
	}
	if got, want := direct.Header().Get("X-Frame-Options"), "DENY"; got != want {
		t.Errorf("/inbox/index.html X-Frame-Options = %q, want %q", got, want)
	}
}

// A cross-origin isolated host page frames the panel only with both headers.
// credentialless, not require-corp: Turnstile's origin sends no CORP.
func TestVisitorIsolationHeaders(t *testing.T) {
	env := newTestEnv(t)

	for _, url := range []string{"/widget/", "/widget", "/t/deadbeef", "/visitor/index.html"} {
		t.Run(url, func(t *testing.T) {
			rec := env.do(httptest.NewRequest(http.MethodGet, url, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got, want := rec.Header().Get("Cross-Origin-Resource-Policy"), "cross-origin"; got != want {
				t.Errorf("Cross-Origin-Resource-Policy = %q, want %q", got, want)
			}
			if got, want := rec.Header().Get("Cross-Origin-Embedder-Policy"), "credentialless"; got != want {
				t.Errorf("Cross-Origin-Embedder-Policy = %q, want %q", got, want)
			}
		})
	}

	// The inbox is not embedded anywhere and must not opt in.
	inbox := env.do(httptest.NewRequest(http.MethodGet, "/", nil))
	if got := inbox.Header().Get("Cross-Origin-Resource-Policy"); got != "" {
		t.Errorf("inbox Cross-Origin-Resource-Policy = %q, want it unset", got)
	}
	if got := inbox.Header().Get("Cross-Origin-Embedder-Policy"); got != "" {
		t.Errorf("inbox Cross-Origin-Embedder-Policy = %q, want it unset", got)
	}
}

func TestServeHashedAssets(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name    string
		base    string
		fsys    fs.FS
		ext     string
		wantCT  string
		wantSub string
	}{
		{"visitor js", "/visitor/", web.VisitorApp, ".js", "text/javascript; charset=utf-8", ""},
		{"visitor css", "/visitor/", web.VisitorApp, ".css", "text/css; charset=utf-8", ""},
		{"inbox js", "/inbox/", web.InboxApp, ".js", "text/javascript; charset=utf-8", ""},
		{"inbox css", "/inbox/", web.InboxApp, ".css", "text/css; charset=utf-8", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := firstAsset(t, tc.fsys, tc.ext)
			rec := env.do(httptest.NewRequest(http.MethodGet, tc.base+name, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.wantCT {
				t.Errorf("Content-Type = %q, want %q", got, tc.wantCT)
			}
			if got := rec.Header().Get("Cache-Control"); got != immutableCacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, immutableCacheControl)
			}
			if got, want := rec.Body.Bytes(), mustRead(t, tc.fsys, name); string(got) != string(want) {
				t.Errorf("body (%d bytes) does not match the embedded asset (%d bytes)", len(got), len(want))
			}
		})
	}
}

// The built index.html must reference the app's own asset base, or the routes
// above serve a shell whose scripts 404.
func TestIndexReferencesAssetBase(t *testing.T) {
	for _, tc := range []struct {
		name string
		fsys fs.FS
		base string
	}{
		{"visitor", web.VisitorApp, "/visitor/assets/"},
		{"inbox", web.InboxApp, "/inbox/assets/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if body := string(mustRead(t, tc.fsys, "index.html")); !strings.Contains(body, tc.base) {
				t.Errorf("index.html does not reference %q:\n%s", tc.base, body)
			}
		})
	}
}

func TestAssetNotModified(t *testing.T) {
	env := newTestEnv(t)
	url := "/visitor/" + firstAsset(t, web.VisitorApp, ".js")

	first := env.do(httptest.NewRequest(http.MethodGet, url, nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag is empty")
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("If-None-Match", etag)
	rec := env.do(req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0", rec.Body.Len())
	}
}

// The shells are served no-cache with an ETag, so a returning visitor should
// revalidate cheaply instead of re-downloading both apps.
func TestIndexNotModified(t *testing.T) {
	env := newTestEnv(t)
	for _, url := range []string{"/", "/widget/"} {
		t.Run(url, func(t *testing.T) {
			first := env.do(httptest.NewRequest(http.MethodGet, url, nil))
			etag := first.Header().Get("ETag")
			if etag == "" {
				t.Fatal("ETag is empty")
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("If-None-Match", etag)
			rec := env.do(req)

			if rec.Code != http.StatusNotModified {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("body length = %d, want 0", rec.Body.Len())
			}
		})
	}
}

func TestAssetNotFound(t *testing.T) {
	env := newTestEnv(t)
	for _, p := range []string{
		"/visitor/assets/missing.js",
		"/inbox/assets/missing.css",
		"/visitor/",
		"/visitor/assets",
		"/visitor/../internal/server/assets.go",
	} {
		rec := env.do(httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want %d", p, rec.Code, http.StatusNotFound)
		}
	}
}

func TestConfigEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{"captcha disabled", nil, ""},
		{
			name:      "captcha configured",
			overrides: map[string]string{"TURNSTILE_SITE_KEY": "site-key", "TURNSTILE_SECRET": "secret-key"},
			want:      "site-key",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, tc.overrides)
			rec := env.do(httptest.NewRequest(http.MethodGet, "/api/config", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if got["turnstile_site_key"] != tc.want {
				t.Errorf("turnstile_site_key = %v, want %q", got["turnstile_site_key"], tc.want)
			}
			// Nothing else may leak through this public endpoint.
			if len(got) != 1 {
				t.Errorf("response has %d fields (%v), want only turnstile_site_key", len(got), got)
			}
		})
	}
}

func mustRead(t *testing.T, fsys fs.FS, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return b
}
