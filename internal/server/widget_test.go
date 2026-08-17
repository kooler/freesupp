package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kooler/freesupp/web"
)

func TestWidgetJS(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(httptest.NewRequest(http.MethodGet, "/widget.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/javascript; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Cache-Control"), "public, max-age=3600"; got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	// A no-cors <script src> is gated on CORP, not the CORS header above.
	if got, want := rec.Header().Get("Cross-Origin-Resource-Policy"), "cross-origin"; got != want {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want %q", got, want)
	}

	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) || len(etag) < 8 {
		t.Errorf("ETag = %q, want a quoted, non-empty tag", etag)
	}
	if body := rec.Body.String(); body != string(web.WidgetJS) {
		t.Errorf("body does not match the embedded asset (%d vs %d bytes)", len(body), len(web.WidgetJS))
	}
}

func TestWidgetJSNotModified(t *testing.T) {
	env := newTestEnv(t)
	first := env.do(httptest.NewRequest(http.MethodGet, "/widget.js", nil))
	etag := first.Header().Get("ETag")

	req := httptest.NewRequest(http.MethodGet, "/widget.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec := env.do(req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0", rec.Body.Len())
	}
	if got := rec.Header().Get("ETag"); got != etag {
		t.Errorf("ETag = %q, want %q", got, etag)
	}
}

func TestWidgetJSStaleETagServesBody(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/widget.js", nil)
	req.Header.Set("If-None-Match", `"stale"`)
	rec := env.do(req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != len(web.WidgetJS) {
		t.Errorf("body length = %d, want %d", rec.Body.Len(), len(web.WidgetJS))
	}
}

func TestWidgetMark(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(httptest.NewRequest(http.MethodGet, "/widget-mark.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Header().Get("Content-Type"), "image/png"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Cache-Control"), "public, max-age=3600"; got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	// widget.js loads this as an <img> on the host page, so it needs CORP for
	// the same reason the script does.
	if got, want := rec.Header().Get("Cross-Origin-Resource-Policy"), "cross-origin"; got != want {
		t.Errorf("Cross-Origin-Resource-Policy = %q, want %q", got, want)
	}
	if got := rec.Body.Len(); got != len(web.WidgetMark) {
		t.Errorf("body length = %d, want %d", got, len(web.WidgetMark))
	}
}

func TestWidgetMarkNotModified(t *testing.T) {
	env := newTestEnv(t)
	etag := env.do(httptest.NewRequest(http.MethodGet, "/widget-mark.png", nil)).Header().Get("ETag")

	req := httptest.NewRequest(http.MethodGet, "/widget-mark.png", nil)
	req.Header.Set("If-None-Match", etag)
	rec := env.do(req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0", rec.Body.Len())
	}
}

// The script must be self-configuring: it derives its origin from its own src
// and points the iframe at the visitor app route.
func TestWidgetJSContent(t *testing.T) {
	src := string(web.WidgetJS)
	for _, want := range []string{"document.currentScript", "'/widget/'", "'/widget-mark.png'", "data-base-url"} {
		if !strings.Contains(src, want) {
			t.Errorf("widget.js does not contain %q", want)
		}
	}
	if len(web.WidgetJS) > 16<<10 {
		t.Errorf("widget.js is %d bytes, want it to stay under 16 KiB", len(web.WidgetJS))
	}
}
