package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/beads"
)

const workflowContextFile = "current-context.json"

type workflowContext struct {
	CurrentIssueID string    `json:"current_issue_id,omitempty"`
	CurrentParent  string    `json:"current_parent_id,omitempty"`
	LastReadyID    string    `json:"last_ready_issue_id,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

func getWorkflowContext() *workflowContext {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, workflowContextFile)) // #nosec G304 -- path derived from beads dir
	if err != nil {
		return nil
	}

	var ctx workflowContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil
	}
	return &ctx
}

func saveWorkflowContext(update func(*workflowContext)) {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return
	}

	ctx := getWorkflowContext()
	if ctx == nil {
		ctx = &workflowContext{}
	}
	update(ctx)
	ctx.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(beadsDir, workflowContextFile), append(data, '\n'), 0o600)
}

func setWorkflowCurrentIssue(issueID string) {
	if issueID == "" {
		return
	}
	saveWorkflowContext(func(ctx *workflowContext) {
		ctx.CurrentIssueID = issueID
	})
}

func setWorkflowCurrentParent(issueID string) {
	if issueID == "" {
		return
	}
	saveWorkflowContext(func(ctx *workflowContext) {
		ctx.CurrentParent = issueID
	})
}

func setWorkflowLastReadyIssue(issueID string) {
	if issueID == "" {
		return
	}
	saveWorkflowContext(func(ctx *workflowContext) {
		ctx.LastReadyID = issueID
	})
}

func clearWorkflowCurrentIssueIfMatches(issueID string) {
	if issueID == "" {
		return
	}
	saveWorkflowContext(func(ctx *workflowContext) {
		if ctx.CurrentIssueID == issueID {
			ctx.CurrentIssueID = ""
		}
		if ctx.CurrentParent == issueID {
			ctx.CurrentParent = ""
		}
	})
}

func getWorkflowCurrentIssueID() string {
	ctx := getWorkflowContext()
	if ctx == nil {
		return ""
	}
	return ctx.CurrentIssueID
}
