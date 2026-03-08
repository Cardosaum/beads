//go:build cgo && !windows

package producte2edocker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/tests/e2ehelpers"
)

type issueJSON struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Notes       string   `json:"notes"`
	Status      string   `json:"status"`
	Assignee    string   `json:"assignee"`
	Labels      []string `json:"labels"`
}

type issueWithCountsJSON struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	Labels          []string `json:"labels"`
	DependencyCount int      `json:"dependency_count"`
}

type configJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type commentJSON struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

type depJSON struct {
	Status      string `json:"status"`
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
}

type doctorJSON struct {
	Path      string            `json:"path"`
	Checks    []json.RawMessage `json:"checks"`
	OverallOK bool              `json:"overall_ok"`
}

func TestProductCoreCommandsWithIsolatedDoltContainer(t *testing.T) {
	port := testutil.StartIsolatedDoltContainer(t)
	t.Setenv("BEADS_TEST_MODE", "1")
	t.Setenv("BEADS_DOLT_PORT", port)
	t.Setenv("BEADS_DOLT_SERVER_MODE", "1")

	repoRoot := e2ehelpers.FindRepoRoot(t)
	binary := e2ehelpers.BuildBDBinary(t, repoRoot)

	tmpDir := t.TempDir()
	e2ehelpers.InitGitRepo(t, tmpDir)
	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "init", "--server", "--prefix", "tst", "--quiet")

	var doctor doctorJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "doctor", "--json"), &doctor)
	if doctor.Path == "" {
		t.Fatal("expected doctor JSON to include path")
	}
	if len(doctor.Checks) == 0 {
		t.Fatal("expected doctor JSON to include checks")
	}

	var cfg configJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "config", "set", "custom.team", "platform", "--json"), &cfg)
	if cfg.Key != "custom.team" || cfg.Value != "platform" {
		t.Fatalf("unexpected config set result: %+v", cfg)
	}
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "config", "get", "custom.team", "--json"), &cfg)
	if cfg.Value != "platform" {
		t.Fatalf("expected config value platform, got %+v", cfg)
	}

	var parent issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "create", "Parent epic", "-t", "epic", "-p", "1", "--json"), &parent)
	var blocker issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "create", "Blocker task", "-t", "task", "-p", "0", "--json"), &blocker)
	var child issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "create", "Child task", "-t", "task", "-p", "2", "--description", "Child description", "--notes", "Start notes", "--json"), &child)

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "update", child.ID, "--parent", parent.ID, "--add-label", "backend", "--json")

	var dep depJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "dep", "add", child.ID, blocker.ID, "--json"), &dep)
	if dep.IssueID != child.ID || dep.DependsOnID != blocker.ID || dep.Type == "" {
		t.Fatalf("unexpected dependency result: %+v", dep)
	}

	var comment commentJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "comments", "add", child.ID, "First comment", "--json"), &comment)
	if comment.Text != "First comment" {
		t.Fatalf("unexpected comment result: %+v", comment)
	}

	var comments []commentJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "comments", child.ID, "--json"), &comments)
	if len(comments) != 1 || comments[0].Text != "First comment" {
		t.Fatalf("unexpected comments list: %+v", comments)
	}

	var searchResults []issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "search", "Child task", "--json"), &searchResults)
	if !containsIssue(searchResults, child.ID) {
		t.Fatalf("expected search results to include %s, got %+v", child.ID, searchResults)
	}

	var listResults []issueJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "list", "--label", "backend", "--json"), &listResults)
	if !containsIssue(listResults, child.ID) {
		t.Fatalf("expected list results to include labeled child %s, got %+v", child.ID, listResults)
	}

	var readyBefore []issueWithCountsJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "ready", "--type", "task", "--json"), &readyBefore)
	if !containsReadyIssue(readyBefore, blocker.ID) {
		t.Fatalf("expected blocker %s to be ready before close, got %+v", blocker.ID, readyBefore)
	}
	if containsReadyIssue(readyBefore, child.ID) {
		t.Fatalf("expected blocked child %s to be absent from ready list, got %+v", child.ID, readyBefore)
	}

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "close", blocker.ID, "--reason", "unblocked", "--json")

	var readyAfter []issueWithCountsJSON
	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "ready", "--type", "task", "--json"), &readyAfter)
	if !containsReadyIssue(readyAfter, child.ID) {
		t.Fatalf("expected child %s to be ready after blocker closes, got %+v", child.ID, readyAfter)
	}

	claimed := decodeIssueLikeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "update", child.ID, "--claim", "--json"))
	if claimed.Status != "in_progress" || claimed.Assignee == "" {
		t.Fatalf("expected claimed child to be in_progress with assignee, got %+v", claimed)
	}

	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "note-current", "Added note from product e2e", "--json")
	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "comments", "add", child.ID, "Second comment", "--json")
	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "close-current", "--reason", "completed", "--json")
	e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "reopen", child.ID, "--reason", "follow-up needed", "--json")

	shown := decodeIssueLikeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "show", child.ID, "--json"))
	if shown.Status != "open" {
		t.Fatalf("expected reopened child to be open, got %+v", shown)
	}
	if !strings.Contains(shown.Notes, "Start notes") || !strings.Contains(shown.Notes, "Added note from product e2e") {
		t.Fatalf("expected note-current content to persist, got %+v", shown)
	}

	e2ehelpers.DecodeJSON(t, e2ehelpers.RunBD(t, repoRoot, tmpDir, binary, "comments", child.ID, "--json"), &comments)
	if len(comments) != 2 {
		t.Fatalf("expected original comments to remain after reopen, got %+v", comments)
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

func containsIssue(issues []issueJSON, id string) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}

func containsReadyIssue(issues []issueWithCountsJSON, id string) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}
