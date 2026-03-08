//go:build cgo && !windows

package agentworkflowdocker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/steveyegge/beads/internal/testutil"
)

type issueJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

type workflowJSON struct {
	Issue struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"issue"`
	Created  bool   `json:"created"`
	Deduped  bool   `json:"deduped"`
	ParentID string `json:"parent_id"`
}

func TestAgentWorkflowCommandsWithIsolatedDoltContainer(t *testing.T) {
	port := testutil.StartIsolatedDoltContainer(t)
	t.Setenv("BEADS_TEST_MODE", "1")
	t.Setenv("BEADS_DOLT_PORT", port)
	t.Setenv("BEADS_DOLT_SERVER_MODE", "1")

	repoRoot := findRepoRoot(t)
	binary := buildBDBinary(t, repoRoot)

	tmpDir := t.TempDir()
	runBD(t, repoRoot, tmpDir, binary, "init", "--server", "--prefix", "tst", "--quiet")

	var parent issueJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "create", "Parent issue", "--json"), &parent)

	var discover1 workflowJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "discover", parent.ID, "Follow-up task", "--json"), &discover1)
	if !discover1.Created || discover1.Deduped {
		t.Fatalf("expected first discover to create, got %+v", discover1)
	}

	var discover2 workflowJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "discover", parent.ID, "Follow-up task", "--json"), &discover2)
	if discover2.Created || !discover2.Deduped {
		t.Fatalf("expected second discover to dedupe, got %+v", discover2)
	}
	if discover2.Issue.ID != discover1.Issue.ID {
		t.Fatalf("expected discover dedupe to reuse %s, got %s", discover1.Issue.ID, discover2.Issue.ID)
	}

	var ensure1 workflowJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "ensure", "Shared issue", "--json"), &ensure1)
	if !ensure1.Created {
		t.Fatalf("expected ensure to create issue, got %+v", ensure1)
	}

	var ensure2 workflowJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "ensure", "Shared issue", "--json"), &ensure2)
	if ensure2.Created || !ensure2.Deduped {
		t.Fatalf("expected ensure to dedupe, got %+v", ensure2)
	}
	if ensure2.Issue.ID != ensure1.Issue.ID {
		t.Fatalf("expected ensure dedupe to reuse %s, got %s", ensure1.Issue.ID, ensure2.Issue.ID)
	}

	runBD(t, repoRoot, tmpDir, binary, "create", "Low priority", "-p", "2")
	var high issueJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "create", "High priority", "-p", "1", "--json"), &high)

	var claimed issueJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "claim-ready", "--json"), &claimed)
	if claimed.ID != high.ID {
		t.Fatalf("expected claim-ready to claim %s, got %s", high.ID, claimed.ID)
	}
	if claimed.Status != "in_progress" {
		t.Fatalf("expected claimed issue to be in_progress, got %s", claimed.Status)
	}
	if claimed.Assignee == "" {
		t.Fatal("expected claimed issue assignee to be set")
	}

	var current struct {
		Status string    `json:"status"`
		Issue  issueJSON `json:"issue"`
	}
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "agent", "current"), &current)
	if current.Status != "ok" || current.Issue.ID != claimed.ID {
		t.Fatalf("expected agent current to resolve %s, got %+v", claimed.ID, current)
	}

	runBD(t, repoRoot, tmpDir, binary, "note-current", "Added note from docker test")
	noted := decodeIssueLikeJSON(t, runBD(t, repoRoot, tmpDir, binary, "show", claimed.ID, "--json"))
	if noted.Notes != "Added note from docker test" {
		t.Fatalf("expected note-current to append notes, got %q", noted.Notes)
	}

	runBD(t, repoRoot, tmpDir, binary, "update-current", "--priority", "0")
	updated := decodeIssueLikeJSON(t, runBD(t, repoRoot, tmpDir, binary, "show", claimed.ID, "--json"))
	if updated.ID != claimed.ID {
		t.Fatalf("expected update-current to target %s, got %s", claimed.ID, updated.ID)
	}

	var ensured workflowJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "agent", "ensure", "Fingerprint one", "--dedupe-key", "path=cmd/bd/agent_workflow.go", "--dedupe-key", "symbol=runEnsureCommand"), &ensured)
	if !ensured.Created {
		t.Fatalf("expected agent ensure to create, got %+v", ensured)
	}

	var deduped workflowJSON
	decodeJSON(t, runBD(t, repoRoot, tmpDir, binary, "agent", "ensure", "Fingerprint two", "--dedupe-key", "path=cmd/bd/agent_workflow.go", "--dedupe-key", "symbol=runEnsureCommand"), &deduped)
	if deduped.Created || !deduped.Deduped || deduped.Issue.ID != ensured.Issue.ID {
		t.Fatalf("expected agent ensure dedupe, got %+v", deduped)
	}

	runBD(t, repoRoot, tmpDir, binary, "agent", "close-current", "--reason", "completed in docker test")
	closed := decodeIssueLikeJSON(t, runBD(t, repoRoot, tmpDir, binary, "show", claimed.ID, "--json"))
	if closed.Status != "closed" {
		t.Fatalf("expected close-current to close %s, got %s", claimed.ID, closed.Status)
	}
}

func buildBDBinary(t *testing.T, repoRoot string) string {
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

func runBD(t *testing.T, repoRoot, cwd, binary string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"BEADS_TEST_MODE=1",
		"BEADS_DOLT_SERVER_MODE=1",
		fmt.Sprintf("BEADS_DOLT_PORT=%s", os.Getenv("BEADS_DOLT_PORT")),
	)
	// Keep HOME stable so subprocesses don't depend on the temp cwd for config lookups.
	cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", os.Getenv("HOME")))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("bd %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func decodeJSON(t *testing.T, raw string, target interface{}) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
}

func decodeIssueLikeJSON(t *testing.T, raw string) issueJSON {
	t.Helper()

	var issue issueJSON
	if err := json.Unmarshal([]byte(raw), &issue); err == nil && issue.ID != "" {
		return issue
	}

	var issues []issueJSON
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		t.Fatalf("decode issue-like JSON %q: %v", raw, err)
	}
	if len(issues) == 0 {
		t.Fatalf("expected at least one issue in JSON output, got %q", raw)
	}
	return issues[0]
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
