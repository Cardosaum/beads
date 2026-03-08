package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowContextRoundTrip(t *testing.T) {
	tmpBeads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(tmpBeads, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpBeads, "metadata.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	t.Setenv("BEADS_DIR", tmpBeads)

	setWorkflowCurrentIssue("bd-101")
	setWorkflowCurrentParent("bd-100")
	setWorkflowLastReadyIssue("bd-101")

	ctx := getWorkflowContext()
	if ctx == nil {
		t.Fatal("expected workflow context")
	}
	if ctx.CurrentIssueID != "bd-101" {
		t.Fatalf("CurrentIssueID = %q, want %q", ctx.CurrentIssueID, "bd-101")
	}
	if ctx.CurrentParent != "bd-100" {
		t.Fatalf("CurrentParent = %q, want %q", ctx.CurrentParent, "bd-100")
	}
	if ctx.LastReadyID != "bd-101" {
		t.Fatalf("LastReadyID = %q, want %q", ctx.LastReadyID, "bd-101")
	}
	if ctx.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestClearWorkflowCurrentIssueIfMatches(t *testing.T) {
	tmpBeads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(tmpBeads, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpBeads, "metadata.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	t.Setenv("BEADS_DIR", tmpBeads)

	setWorkflowCurrentIssue("bd-101")
	setWorkflowCurrentParent("bd-101")
	clearWorkflowCurrentIssueIfMatches("bd-101")

	ctx := getWorkflowContext()
	if ctx == nil {
		t.Fatal("expected workflow context")
	}
	if ctx.CurrentIssueID != "" {
		t.Fatalf("CurrentIssueID = %q, want empty", ctx.CurrentIssueID)
	}
	if ctx.CurrentParent != "" {
		t.Fatalf("CurrentParent = %q, want empty", ctx.CurrentParent)
	}
}
