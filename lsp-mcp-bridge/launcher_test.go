package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type launcherFixture struct {
	repoDir      string
	binDir       string
	pidPath      string
	logPath      string
	healthMarker string
	startLsp     string
	stopLsp      string
}

func TestStartLspWaitsForHealthyBridge(t *testing.T) {
	fx := newLauncherFixture(t, "healthy")

	output, err := runLauncherScript(t, fx, fx.startLsp)
	if err != nil {
		logData, _ := os.ReadFile(fx.logPath)
		t.Fatalf("start-lsp.sh failed: %v\n%s\nlog:\n%s", err, output, string(logData))
	}
	if !strings.Contains(output, "Started (pid") {
		t.Fatalf("start-lsp.sh output = %q, want success message", output)
	}
	if _, err := os.Stat(fx.pidPath); err != nil {
		t.Fatalf("pid file missing: %v", err)
	}
	if _, err := os.Stat(fx.healthMarker); err != nil {
		t.Fatalf("health marker missing: %v", err)
	}

	stopOutput, stopErr := runLauncherScript(t, fx, fx.stopLsp)
	if stopErr != nil {
		t.Fatalf("stop-lsp.sh failed: %v\n%s", stopErr, stopOutput)
	}
	if _, err := os.Stat(fx.pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still present after stop: %v", err)
	}
	if _, err := os.Stat(fx.healthMarker); !os.IsNotExist(err) {
		t.Fatalf("health marker still present after stop: %v", err)
	}
}

func TestStartLspFailsWhenBridgeExitsBeforeHealthy(t *testing.T) {
	fx := newLauncherFixture(t, "exit")

	output, err := runLauncherScript(t, fx, fx.startLsp)
	if err == nil {
		t.Fatalf("start-lsp.sh succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(output, "Bridge failed to become healthy") {
		t.Fatalf("start-lsp.sh output = %q, want readiness failure", output)
	}
	if _, err := os.Stat(fx.pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed on failure: %v", err)
	}
}

func TestStartLspFailsWhenBridgeNeverBecomesHealthy(t *testing.T) {
	fx := newLauncherFixture(t, "hang")

	output, err := runLauncherScript(t, fx, fx.startLsp)
	if err == nil {
		t.Fatalf("start-lsp.sh succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(output, "Bridge failed to become healthy") {
		t.Fatalf("start-lsp.sh output = %q, want readiness failure", output)
	}
	if _, err := os.Stat(fx.pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed on timeout: %v", err)
	}
}

func TestStartLspRejectsUnhealthyExistingPID(t *testing.T) {
	fx := newLauncherFixture(t, "healthy")

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	if err := os.WriteFile(fx.pidPath, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	output, err := runLauncherScript(t, fx, fx.startLsp)
	if err == nil {
		t.Fatalf("start-lsp.sh succeeded unexpectedly:\n%s", output)
	}
	if strings.Contains(output, "already running") {
		t.Fatalf("start-lsp.sh treated unhealthy pid as healthy:\n%s", output)
	}
	if !strings.Contains(output, "health") {
		t.Fatalf("start-lsp.sh output = %q, want health warning", output)
	}
}

func newLauncherFixture(t *testing.T, bridgeMode string) launcherFixture {
	t.Helper()

	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found")
	}

	repoDir := t.TempDir()
	binDir := filepath.Join(repoDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	startPath := filepath.Join(repoDir, "start-lsp.sh")
	stopPath := filepath.Join(repoDir, "stop-lsp.sh")
	copyFile(t, filepath.Join(repoRoot(t), "start-lsp.sh"), startPath, 0755)
	copyFile(t, filepath.Join(repoRoot(t), "stop-lsp.sh"), stopPath, 0755)

	writeExecutable(t, filepath.Join(binDir, "pyright-langserver"), "#!/usr/bin/env bash\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "typescript-language-server"), "#!/usr/bin/env bash\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "node"), "#!/usr/bin/env bash\necho v20.0.0\n")
	writeExecutable(t, filepath.Join(binDir, "lsp-mcp-bridge"), fakeBridgeScript(t, bridgeMode))

	pidPath := filepath.Join(repoDir, "lsp.pid")
	logPath := filepath.Join(repoDir, "lsp.log")
	healthMarker := filepath.Join(repoDir, "healthy.marker")
	timeoutSec := 1
	if bridgeMode == "healthy" {
		timeoutSec = 5
	}
	envLsp := fmt.Sprintf(`# test env.lsp
LSP_PORT=7890
LSP_WORKSPACE=%s
LSP_PYTHON=%s
LSP_PYRIGHT_BIN=%s
LSP_TSS_BIN=%s
LSP_LOG=%s
LSP_PID=%s
LSP_HEALTH_MARKER=%s
LSP_HEALTHCHECK_CMD='test -f %s'
LSP_START_TIMEOUT_SEC=%d
`,
		filepath.Join(repoDir, "workspace"),
		pythonPath,
		filepath.Join(binDir, "pyright-langserver"),
		filepath.Join(binDir, "typescript-language-server"),
		logPath,
		pidPath,
		healthMarker,
		healthMarker,
		timeoutSec,
	)
	if err := os.WriteFile(filepath.Join(repoDir, "env.lsp"), []byte(envLsp), 0644); err != nil {
		t.Fatalf("write env.lsp: %v", err)
	}

	return launcherFixture{
		repoDir:      repoDir,
		binDir:       binDir,
		pidPath:      pidPath,
		logPath:      logPath,
		healthMarker: healthMarker,
		startLsp:     startPath,
		stopLsp:      stopPath,
	}
}

func runLauncherScript(t *testing.T, fx launcherFixture, script string) (string, error) {
	t.Helper()

	cmd := exec.Command("bash", script)
	cmd.Dir = fx.repoDir
	cmd.Env = append(os.Environ(), "PATH="+fx.binDir+":"+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func fakeBridgeScript(t *testing.T, mode string) string {
	t.Helper()

	switch mode {
	case "healthy":
		return `#!/usr/bin/env bash
set -euo pipefail
sleep 0.2
: > "$LSP_HEALTH_MARKER"
trap 'rm -f "$LSP_HEALTH_MARKER"; exit 0' TERM INT
while true; do
    sleep 1
done
`
	case "hang":
		return `#!/usr/bin/env bash
set -euo pipefail
trap 'exit 0' TERM INT
while true; do
    sleep 1
done
`
	case "exit":
		return `#!/usr/bin/env bash
set -euo pipefail
echo "startup failed" >&2
exit 1
`
	default:
		t.Fatalf("unknown fake bridge mode %q", mode)
		return ""
	}
}

func copyFile(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(file))
}
