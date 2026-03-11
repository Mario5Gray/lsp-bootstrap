package testutil

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// FixturesDir returns the absolute path to testutil/fixtures/.
func FixturesDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "fixtures")
}

// FixturePath returns the absolute path to a named fixture file.
func FixturePath(name string) string {
	return filepath.Join(FixturesDir(), name)
}

// LoadEnvLsp parses a KEY=VALUE env file and returns the map.
// Lines starting with # and blank lines are ignored.
// Values preserve everything after the first '=' including spaces.
func LoadEnvLsp(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("LoadEnvLsp: open %s: %v", path, err)
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := line[idx+1:] // preserve everything after first '=', including spaces
		result[key] = val
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("LoadEnvLsp: scan %s: %v", path, err)
	}
	return result
}

// TempEnvLsp writes a temporary env.lsp file from a key/value map and
// returns its path. The file is removed when the test ends.
func TempEnvLsp(t *testing.T, overrides map[string]string) string {
	t.Helper()

	// Start from the fixture defaults.
	base := LoadEnvLsp(t, FixturePath("env.lsp"))
	for k, v := range overrides {
		base[k] = v
	}

	f, err := os.CreateTemp(t.TempDir(), "env.lsp.*")
	if err != nil {
		t.Fatalf("TempEnvLsp: create temp file: %v", err)
	}
	defer f.Close()

	for k, v := range base {
		if _, err := f.WriteString(k + "=" + v + "\n"); err != nil {
			t.Fatalf("TempEnvLsp: write: %v", err)
		}
	}
	return f.Name()
}

// RealBinPath looks up a binary on PATH and skips the test if not found.
// Used in integration tests to skip cleanly when a language server is absent.
func RealBinPath(t *testing.T, binary string) string {
	t.Helper()
	path, err := exec.LookPath(binary)
	if err != nil {
		t.Skipf("binary %q not found on PATH — skipping integration test", binary)
	}
	return path
}
