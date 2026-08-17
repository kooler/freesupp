// Package web holds the browser assets embedded into the FreeSupp binary.
package web

import (
	"embed"
	"io/fs"
)

// WidgetJS is the embed script host pages load with a single <script> tag.
//
//go:embed widget/widget.js
var WidgetJS []byte

// WidgetMark is the logo glyph the bubble button shows, white on transparent so
// it sits on any bubble colour. Kept out of widget.js so the script stays small
// on every host page.
//
//go:embed widget/mark.png
var WidgetMark []byte

// The two Vite builds are committed so `go build` works without Node; `make
// build` regenerates them. "all:" keeps dot-files (none today) from being
// silently dropped.
//
//go:embed all:visitor/dist
var visitorDist embed.FS

//go:embed all:inbox/dist
var inboxDist embed.FS

// VisitorApp is the built visitor SPA rooted at its index.html.
var VisitorApp = mustSub(visitorDist, "visitor/dist")

// InboxApp is the built operator inbox SPA rooted at its index.html.
var InboxApp = mustSub(inboxDist, "inbox/dist")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
