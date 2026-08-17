package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/kooler/freesupp/web"
)

// widgetJSMaxAge is short because /widget.js is an unhashed URL: the ETag does
// the real caching work, this only bounds how long a fix takes to reach hosts.
const widgetJSMaxAge = time.Hour

// ETags are stable for a given build, so they are computed once.
var (
	widgetJSETag   = etagOf(web.WidgetJS)
	widgetMarkETag = etagOf(web.WidgetMark)
)

// handleWidgetJS serves the embed script with revalidation support.
func (s *Server) handleWidgetJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(widgetJSMaxAge.Seconds())))
	w.Header().Set("ETag", widgetJSETag)
	// Host pages embed this cross-origin. CORP as well as CORS: a <script src>
	// is a no-cors request, so an isolated host page checks CORP, not CORS.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")

	// Zero modtime keeps Last-Modified out; ServeContent still answers
	// If-None-Match from the ETag above.
	http.ServeContent(w, r, "widget.js", time.Time{}, bytes.NewReader(web.WidgetJS))
}

// handleWidgetMark serves the bubble glyph, which widget.js loads from the same
// deployment as an <img> on the host page.
func (s *Server) handleWidgetMark(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", contentTypes[".png"])
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(widgetJSMaxAge.Seconds())))
	w.Header().Set("ETag", widgetMarkETag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// An <img> on a host page is a no-cors request, same as the script.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	http.ServeContent(w, r, "mark.png", time.Time{}, bytes.NewReader(web.WidgetMark))
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
