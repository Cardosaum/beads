package e2ehelpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func BuildBDBinary(t *testing.T, repoRoot string) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "bd-test-bin")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/bd")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build bd: %v\n%s", err, output)
	}
	return binary
}

func RunBD(t *testing.T, repoRoot, cwd, binary string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"BEADS_TEST_MODE=1",
		"BEADS_DOLT_SERVER_MODE=1",
		fmt.Sprintf("BEADS_DOLT_PORT=%s", os.Getenv("BEADS_DOLT_PORT")),
		fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("bd %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func RunCommand(t *testing.T, cwd string, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", name, args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func DecodeJSON(t *testing.T, raw string, target interface{}) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
}

func FindRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func InitGitRepo(t *testing.T, dir string) {
	t.Helper()
	RunCommand(t, dir, "git", "init")
	RunCommand(t, dir, "git", "config", "user.name", "Docker E2E")
	RunCommand(t, dir, "git", "config", "user.email", "docker-e2e@example.com")
}
