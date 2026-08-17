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

// widgetJSETag is stable for a given build, so it is computed once.
var widgetJSETag = etagOf(web.WidgetJS)

// handleWidgetJS serves the embed script with revalidation support.
func (s *Server) handleWidgetJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(widgetJSMaxAge.Seconds())))
	w.Header().Set("ETag", widgetJSETag)
	// Host pages embed this cross-origin.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Zero modtime keeps Last-Modified out; ServeContent still answers
	// If-None-Match from the ETag above.
	http.ServeContent(w, r, "widget.js", time.Time{}, bytes.NewReader(web.WidgetJS))
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
