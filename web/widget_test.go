package web

import (
	"os"
	"os/exec"
	"testing"
)

// TestWidgetJSSyntax runs the embed script through node's parser. There is no
// build step for widget.js, so this is the only thing standing between a typo
// and a broken embed on every host page.
func TestWidgetJSSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping widget.js syntax check")
	}

	out, err := exec.Command(node, "--check", "widget/widget.js").CombinedOutput()
	if err != nil {
		t.Fatalf("node --check widget/widget.js failed: %v\n%s", err, out)
	}
}

func TestWidgetJSEmbedMatchesFile(t *testing.T) {
	onDisk, err := os.ReadFile("widget/widget.js")
	if err != nil {
		t.Fatalf("reading widget.js: %v", err)
	}
	if string(WidgetJS) != string(onDisk) {
		t.Error("embedded WidgetJS differs from widget/widget.js")
	}
}
