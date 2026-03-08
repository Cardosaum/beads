//go:build cgo && integration

package main

import (
	"encoding/json"
	"testing"
)

type workflowCommandResult struct {
	Issue struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Notes  string `json:"notes"`
		Status string `json:"status"`
	} `json:"issue"`
	Created bool   `json:"created"`
	Deduped bool   `json:"deduped"`
	Parent  string `json:"parent_id"`
}

type issueJSON struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

func TestCLI_DiscoverDedupesByParentAndTitle(t *testing.T) {
	tmpDir := setupCLITestDB(t)

	parentOut := runBDInProcess(t, tmpDir, "create", "Parent issue", "--json")
	var parent issueJSON
	decodeJSON(t, parentOut, &parent)

	firstOut := runBDInProcess(t, tmpDir, "discover", parent.ID, "Follow-up task", "--json")
	var first workflowCommandResult
	decodeJSON(t, firstOut, &first)
	if !first.Created || first.Deduped {
		t.Fatalf("expected first discover to create issue, got %+v", first)
	}

	secondOut := runBDInProcess(t, tmpDir, "discover", parent.ID, "Follow-up task", "--json")
	var second workflowCommandResult
	decodeJSON(t, secondOut, &second)
	if second.Created || !second.Deduped {
		t.Fatalf("expected second discover to dedupe, got %+v", second)
	}
	if second.Issue.ID != first.Issue.ID {
		t.Fatalf("expected deduped discover to reuse %s, got %s", first.Issue.ID, second.Issue.ID)
	}
}

func TestCLI_DiscoverCurrentUsesCurrentIssue(t *testing.T) {
	tmpDir := setupCLITestDB(t)

	parentOut := runBDInProcess(t, tmpDir, "create", "Current parent", "--json")
	var parent issueJSON
	decodeJSON(t, parentOut, &parent)

	runBDInProcess(t, tmpDir, "update", parent.ID, "--claim", "--json")

	discoverOut := runBDInProcess(t, tmpDir, "discover-current", "Current follow-up", "--json")
	var result workflowCommandResult
	decodeJSON(t, discoverOut, &result)

	if !result.Created {
		t.Fatalf("expected discover-current to create issue, got %+v", result)
	}
	if result.Parent != parent.ID {
		t.Fatalf("expected parent_id %s, got %s", parent.ID, result.Parent)
	}
}

func TestCLI_EnsureReusesExistingIssue(t *testing.T) {
	tmpDir := setupCLITestDB(t)

	firstOut := runBDInProcess(t, tmpDir, "ensure", "Stable title", "--json")
	var first workflowCommandResult
	decodeJSON(t, firstOut, &first)
	if !first.Created {
		t.Fatalf("expected first ensure to create issue, got %+v", first)
	}

	secondOut := runBDInProcess(t, tmpDir, "ensure", "Stable title", "--json")
	var second workflowCommandResult
	decodeJSON(t, secondOut, &second)
	if second.Created || !second.Deduped {
		t.Fatalf("expected second ensure to dedupe, got %+v", second)
	}
	if second.Issue.ID != first.Issue.ID {
		t.Fatalf("expected ensure to reuse %s, got %s", first.Issue.ID, second.Issue.ID)
	}
}

func TestCLI_ClaimReadyClaimsTopPriorityIssue(t *testing.T) {
	tmpDir := setupCLITestDB(t)

	runBDInProcess(t, tmpDir, "create", "Lower priority", "-p", "2")
	highOut := runBDInProcess(t, tmpDir, "create", "Higher priority", "-p", "1", "--json")
	var high issueJSON
	decodeJSON(t, highOut, &high)

	claimedOut := runBDInProcess(t, tmpDir, "claim-ready", "--json")
	var claimed issueJSON
	decodeJSON(t, claimedOut, &claimed)

	if claimed.ID != high.ID {
		t.Fatalf("expected claim-ready to pick %s, got %s", high.ID, claimed.ID)
	}
	if claimed.Status != "in_progress" {
		t.Fatalf("expected claimed issue status in_progress, got %s", claimed.Status)
	}
	if claimed.Assignee == "" {
		t.Fatal("expected claimed issue to have assignee set")
	}
}

func TestCLI_NoteCurrentAppendsNotes(t *testing.T) {
	tmpDir := setupCLITestDB(t)

	issueOut := runBDInProcess(t, tmpDir, "create", "Note target", "--notes", "Original notes", "--json")
	var issue issueJSON
	decodeJSON(t, issueOut, &issue)

	runBDInProcess(t, tmpDir, "note-current", "Added note", "--json")

	showOut := runBDInProcess(t, tmpDir, "show", issue.ID, "--json")
	var shown issueJSON
	decodeJSON(t, showOut, &shown)
	if shown.Notes != "Original notes\nAdded note" {
		t.Fatalf("expected appended notes, got %q", shown.Notes)
	}
}

func decodeJSON(t *testing.T, raw string, target interface{}) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("failed to decode JSON %q: %v", raw, err)
	}
}
