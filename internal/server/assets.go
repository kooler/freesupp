package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kooler/freesupp/web"
)

// Vite fingerprints everything under assets/, so those URLs can be cached
// forever; index.html is fetched by URL and must revalidate on every load.
const (
	immutableCacheControl = "public, max-age=31536000, immutable"
	indexCacheControl     = "no-cache"
)

// contentTypes pins the MIME types we serve. mime.TypeByExtension consults
// system files, which vary per machine and per container base image.
var contentTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".ico":   "image/vnd.microsoft.icon",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".map":   "application/json",
	".txt":   "text/plain; charset=utf-8",
}

// spaApp serves one embedded Vite build: its assets by path and its index.html
// as the fallback for the app's own routes.
type spaApp struct {
	fsys      fs.FS
	index     []byte
	indexETag string
	// frameable is true for the visitor app, which the widget loads in an
	// iframe on third-party pages; the inbox refuses framing.
	frameable bool
}

var (
	visitorApp = mustSPA(web.VisitorApp, true)
	inboxApp   = mustSPA(web.InboxApp, false)
)

// mustSPA panics on a build that shipped without an index.html — the embed
// directives make that a compile-time-guaranteed invariant.
func mustSPA(fsys fs.FS, frameable bool) *spaApp {
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		panic("web assets: " + err.Error())
	}
	return &spaApp{fsys: fsys, index: index, indexETag: etagOf(index), frameable: frameable}
}

// serveIndex answers an SPA route with the app shell.
func (a *spaApp) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", contentTypes[".html"])
	w.Header().Set("Cache-Control", indexCacheControl)
	w.Header().Set("ETag", a.indexETag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	a.setEmbedHeaders(w)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(a.index))
}

// setEmbedHeaders states how this app may be embedded: the inbox refuses
// framing, the visitor app is what the widget loads in a cross-origin iframe.
//
// A cross-origin isolated host page needs both CORP and an embedder policy on
// the frame. credentialless rather than require-corp because the panel loads
// the Turnstile challenge from an origin that sends no CORP.
func (a *spaApp) setEmbedHeaders(w http.ResponseWriter) {
	if !a.frameable {
		w.Header().Set("X-Frame-Options", "DENY")
		return
	}
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Cross-Origin-Embedder-Policy", "credentialless")
}

// serveFile answers a request for a built asset; the chi wildcard holds the
// path inside the app's dist directory.
func (a *spaApp) serveFile(w http.ResponseWriter, r *http.Request) {
	name := path.Clean(strings.TrimPrefix(chi.URLParam(r, "*"), "/"))
	if name == "." || name == ".." || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}

	data, err := fs.ReadFile(a.fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ct := contentTypes[strings.ToLower(path.Ext(name))]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// /inbox/index.html and /visitor/index.html reach the same shells as / and
	// /widget/, so they need the same headers.
	a.setEmbedHeaders(w)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", immutableCacheControl)
	} else {
		w.Header().Set("Cache-Control", indexCacheControl)
	}
	w.Header().Set("ETag", etagOf(data))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
