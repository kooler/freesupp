package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// envKeyRe matches the env lookups in config.go: get("X") / getenv("X").
var envKeyRe = regexp.MustCompile(`get(?:env)?\("([A-Z0-9_]+)"\)`)

// Every variable config.Load reads must be documented in docker-compose.yml
// (set or commented out), so operators deploying the image can discover it.
func TestDockerComposeDocumentsEveryEnvVar(t *testing.T) {
	root := filepath.Join("..", "..")

	src := readFile(t, "config.go")
	compose := readFile(t, filepath.Join(root, "docker-compose.yml"))
	dockerfile := readFile(t, filepath.Join(root, "Dockerfile"))

	matches := envKeyRe.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatal("no env lookups found in config.go — did the loader change shape?")
	}

	seen := make(map[string]bool)
	for _, m := range matches {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !strings.Contains(compose, key+":") {
			t.Errorf("%s is read by config.Load but not documented in docker-compose.yml", key)
		}
	}

	// The image pins the two vars whose defaults are container-specific.
	for _, key := range []string{"LISTEN", "DB_PATH"} {
		if !strings.Contains(dockerfile, key+"=") {
			t.Errorf("Dockerfile should set a container default for %s", key)
		}
	}
}

// The README carries the env var reference operators read before deploying,
// so a newly added variable must show up there too.
func TestReadmeDocumentsEveryEnvVar(t *testing.T) {
	src := readFile(t, "config.go")
	readme := readFile(t, filepath.Join("..", "..", "README.md"))

	matches := envKeyRe.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatal("no env lookups found in config.go — did the loader change shape?")
	}

	seen := make(map[string]bool)
	for _, m := range matches {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !strings.Contains(readme, "`"+key+"`") {
			t.Errorf("%s is read by config.Load but not documented in README.md", key)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
