//go:build cgo && !windows

package agentworkflowdocker

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/tests/e2ehelpers"
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

	repoRoot := e2ehelpers.FindRepoRoot(t)
	binary := e2ehelpers.BuildBDBinary(t, repoRoot)

	tmpDir := t.TempDir()
	e2ehelpers.InitGitRepo(t, tmpDir)
	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "init", "--server", "--prefix", "tst", "--quiet")

	var parent issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "create", "Parent issue", "--json"), &parent)

	var discover1 workflowJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "discover", parent.ID, "Follow-up task", "--json"), &discover1)
	if !discover1.Created || discover1.Deduped {
		t.Fatalf("expected first discover to create, got %+v", discover1)
	}

	var discover2 workflowJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "discover", parent.ID, "Follow-up task", "--json"), &discover2)
	if discover2.Created || !discover2.Deduped {
		t.Fatalf("expected second discover to dedupe, got %+v", discover2)
	}
	if discover2.Issue.ID != discover1.Issue.ID {
		t.Fatalf("expected discover dedupe to reuse %s, got %s", discover1.Issue.ID, discover2.Issue.ID)
	}

	var ensure1 workflowJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "ensure", "Shared issue", "--json"), &ensure1)
	if !ensure1.Created {
		t.Fatalf("expected ensure to create issue, got %+v", ensure1)
	}

	var ensure2 workflowJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "ensure", "Shared issue", "--json"), &ensure2)
	if ensure2.Created || !ensure2.Deduped {
		t.Fatalf("expected ensure to dedupe, got %+v", ensure2)
	}
	if ensure2.Issue.ID != ensure1.Issue.ID {
		t.Fatalf("expected ensure dedupe to reuse %s, got %s", ensure1.Issue.ID, ensure2.Issue.ID)
	}

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "create", "Low priority", "-p", "2")
	var high issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "create", "High priority", "-p", "1", "--json"), &high)

	var claimed issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "claim-ready", "--json"), &claimed)
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
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "agent", "current"), &current)
	if current.Status != "ok" || current.Issue.ID != claimed.ID {
		t.Fatalf("expected agent current to resolve %s, got %+v", claimed.ID, current)
	}

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "note-current", "Added note from docker test")
	noted := decodeIssueLikeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "show", claimed.ID, "--json"))
	if noted.Notes != "Added note from docker test" {
		t.Fatalf("expected note-current to append notes, got %q", noted.Notes)
	}

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "update-current", "--priority", "0")
	updated := decodeIssueLikeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "show", claimed.ID, "--json"))
	if updated.ID != claimed.ID {
		t.Fatalf("expected update-current to target %s, got %s", claimed.ID, updated.ID)
	}

	var ensured workflowJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "agent", "ensure", "Fingerprint one", "--dedupe-key", "path=cmd/bd/agent_workflow.go", "--dedupe-key", "symbol=runEnsureCommand"), &ensured)
	if !ensured.Created {
		t.Fatalf("expected agent ensure to create, got %+v", ensured)
	}

	var deduped workflowJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "agent", "ensure", "Fingerprint two", "--dedupe-key", "path=cmd/bd/agent_workflow.go", "--dedupe-key", "symbol=runEnsureCommand"), &deduped)
	if deduped.Created || !deduped.Deduped || deduped.Issue.ID != ensured.Issue.ID {
		t.Fatalf("expected agent ensure dedupe, got %+v", deduped)
	}

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "agent", "close-current", "--reason", "completed in docker test")
	closed := decodeIssueLikeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "show", claimed.ID, "--json"))
	if closed.Status != "closed" {
		t.Fatalf("expected close-current to close %s, got %s", claimed.ID, closed.Status)
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
