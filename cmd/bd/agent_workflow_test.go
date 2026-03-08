package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

func TestEnsureIssueCreatesThenDedupes(t *testing.T) {
	store := newFakeWorkflowStore()
	spec := issueCreateSpec{
		Title:     "Shared follow-up",
		IssueType: types.TypeTask,
		Priority:  2,
		Labels:    []string{"shared"},
	}

	first, err := ensureIssue(context.Background(), store, spec, "tester")
	if err != nil {
		t.Fatalf("ensureIssue first call: %v", err)
	}
	if !first.Created || first.Deduped {
		t.Fatalf("expected first ensure to create, got %+v", first)
	}

	second, err := ensureIssue(context.Background(), store, spec, "tester")
	if err != nil {
		t.Fatalf("ensureIssue second call: %v", err)
	}
	if second.Created || !second.Deduped {
		t.Fatalf("expected second ensure to dedupe, got %+v", second)
	}
	if second.Issue.ID != first.Issue.ID {
		t.Fatalf("expected ensure to reuse %s, got %s", first.Issue.ID, second.Issue.ID)
	}
}

func TestDiscoverIssueCreatesDependencyAndDedupes(t *testing.T) {
	store := newFakeWorkflowStore()
	store.tx.issues["parent-1"] = &types.Issue{
		ID:       "parent-1",
		Title:    "Parent",
		Status:   types.StatusOpen,
		Priority: 1,
	}

	spec := issueCreateSpec{
		Title:     "Found bug",
		IssueType: types.TypeBug,
		Priority:  1,
		Labels:    []string{"bug"},
	}

	first, err := discoverIssue(context.Background(), store, "parent-1", spec, "tester")
	if err != nil {
		t.Fatalf("discoverIssue first call: %v", err)
	}
	if !first.Created {
		t.Fatalf("expected first discover to create, got %+v", first)
	}

	deps := store.tx.deps[first.Issue.ID]
	if len(deps) != 1 || deps[0].Type != types.DepDiscoveredFrom || deps[0].DependsOnID != "parent-1" {
		t.Fatalf("expected discovered-from dependency to parent-1, got %+v", deps)
	}

	second, err := discoverIssue(context.Background(), store, "parent-1", spec, "tester")
	if err != nil {
		t.Fatalf("discoverIssue second call: %v", err)
	}
	if second.Created || !second.Deduped {
		t.Fatalf("expected second discover to dedupe, got %+v", second)
	}
	if second.Issue.ID != first.Issue.ID {
		t.Fatalf("expected discover to reuse %s, got %s", first.Issue.ID, second.Issue.ID)
	}
}

func TestFindWorkflowIssueMatchRequiresSameDiscoveredParent(t *testing.T) {
	tx := newFakeWorkflowStore().tx
	tx.issues["same-1"] = &types.Issue{ID: "same-1", Title: "Follow up", Status: types.StatusOpen}
	tx.deps["same-1"] = []*types.Dependency{{
		IssueID:     "same-1",
		DependsOnID: "parent-a",
		Type:        types.DepDiscoveredFrom,
	}}

	match, err := findWorkflowIssueMatch(context.Background(), tx, issueCreateSpec{Title: "Follow up"}, "parent-b")
	if err != nil {
		t.Fatalf("findWorkflowIssueMatch: %v", err)
	}
	if match != nil {
		t.Fatalf("expected no match for different parent, got %+v", match)
	}
}

func TestEnsureIssueDedupesByMetadataFingerprint(t *testing.T) {
	store := newFakeWorkflowStore()
	firstSpec := issueCreateSpec{
		Title:      "Shared follow-up",
		IssueType:  types.TypeTask,
		Priority:   2,
		DedupeKeys: map[string]string{"path": "internal/storage/dolt/issues.go", "symbol": "CreateIssue"},
		Metadata:   workflowMetadataFromDedupeKeys(map[string]string{"path": "internal/storage/dolt/issues.go", "symbol": "CreateIssue"}),
	}
	secondSpec := issueCreateSpec{
		Title:      "Implement retry on create",
		IssueType:  types.TypeTask,
		Priority:   2,
		DedupeKeys: map[string]string{"path": "internal/storage/dolt/issues.go", "symbol": "CreateIssue"},
		Metadata:   workflowMetadataFromDedupeKeys(map[string]string{"path": "internal/storage/dolt/issues.go", "symbol": "CreateIssue"}),
	}

	first, err := ensureIssue(context.Background(), store, firstSpec, "tester")
	if err != nil {
		t.Fatalf("ensureIssue first call: %v", err)
	}
	second, err := ensureIssue(context.Background(), store, secondSpec, "tester")
	if err != nil {
		t.Fatalf("ensureIssue second call: %v", err)
	}

	if second.Issue.ID != first.Issue.ID {
		t.Fatalf("expected metadata dedupe to reuse %s, got %s", first.Issue.ID, second.Issue.ID)
	}
	if second.Created || !second.Deduped {
		t.Fatalf("expected second ensure to dedupe, got %+v", second)
	}
}

func TestNormalizeWorkflowTitle(t *testing.T) {
	got := normalizeWorkflowTitle("  Shared   Follow Up  ")
	if got != "shared follow up" {
		t.Fatalf("normalizeWorkflowTitle() = %q", got)
	}
}

type fakeWorkflowStore struct {
	tx            *fakeWorkflowTx
	lastCommitMsg string
}

func newFakeWorkflowStore() *fakeWorkflowStore {
	return &fakeWorkflowStore{
		tx: &fakeWorkflowTx{
			issues:   make(map[string]*types.Issue),
			deps:     make(map[string][]*types.Dependency),
			labels:   make(map[string][]string),
			config:   make(map[string]string),
			meta:     make(map[string]string),
			comments: make(map[string][]*types.Comment),
		},
	}
}

func (s *fakeWorkflowStore) RunInTransaction(ctx context.Context, commitMsg string, fn func(tx storage.Transaction) error) error {
	s.lastCommitMsg = commitMsg
	return fn(s.tx)
}

type fakeWorkflowTx struct {
	issues   map[string]*types.Issue
	deps     map[string][]*types.Dependency
	labels   map[string][]string
	config   map[string]string
	meta     map[string]string
	comments map[string][]*types.Comment
	nextID   int
}

func (t *fakeWorkflowTx) CreateIssue(_ context.Context, issue *types.Issue, _ string) error {
	if issue.ID == "" {
		t.nextID++
		issue.ID = fmt.Sprintf("tst-%d", t.nextID)
	}
	t.issues[issue.ID] = cloneWorkflowIssue(issue)
	return nil
}

func (t *fakeWorkflowTx) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	for _, issue := range issues {
		if err := t.CreateIssue(ctx, issue, actor); err != nil {
			return err
		}
	}
	return nil
}

func (t *fakeWorkflowTx) UpdateIssue(context.Context, string, map[string]interface{}, string) error {
	return nil
}

func (t *fakeWorkflowTx) CloseIssue(context.Context, string, string, string, string) error {
	return nil
}

func (t *fakeWorkflowTx) DeleteIssue(context.Context, string) error {
	return nil
}

func (t *fakeWorkflowTx) GetIssue(_ context.Context, id string) (*types.Issue, error) {
	issue, ok := t.issues[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return cloneWorkflowIssue(issue), nil
}

func (t *fakeWorkflowTx) SearchIssues(_ context.Context, _ string, filter types.IssueFilter) ([]*types.Issue, error) {
	var results []*types.Issue
	titleQuery := normalizeWorkflowTitle(filter.TitleSearch)
	for _, issue := range t.issues {
		if issue.Status == types.StatusClosed {
			skipClosed := false
			for _, status := range filter.ExcludeStatus {
				if status == types.StatusClosed {
					skipClosed = true
					break
				}
			}
			if skipClosed {
				continue
			}
		}
		if titleQuery != "" && !strings.Contains(normalizeWorkflowTitle(issue.Title), titleQuery) {
			continue
		}
		if len(filter.MetadataFields) > 0 {
			var metadata map[string]string
			if err := json.Unmarshal(issue.Metadata, &metadata); err != nil {
				continue
			}
			matches := true
			for key, want := range filter.MetadataFields {
				if metadata[key] != want {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}
		results = append(results, cloneWorkflowIssue(issue))
	}
	return results, nil
}

func (t *fakeWorkflowTx) AddDependency(_ context.Context, dep *types.Dependency, _ string) error {
	cloned := *dep
	t.deps[dep.IssueID] = append(t.deps[dep.IssueID], &cloned)
	return nil
}

func (t *fakeWorkflowTx) RemoveDependency(_ context.Context, issueID, dependsOnID string, _ string) error {
	deps := t.deps[issueID]
	filtered := deps[:0]
	for _, dep := range deps {
		if dep.DependsOnID == dependsOnID {
			continue
		}
		filtered = append(filtered, dep)
	}
	t.deps[issueID] = filtered
	return nil
}

func (t *fakeWorkflowTx) GetDependencyRecords(_ context.Context, issueID string) ([]*types.Dependency, error) {
	deps := t.deps[issueID]
	out := make([]*types.Dependency, 0, len(deps))
	for _, dep := range deps {
		cloned := *dep
		out = append(out, &cloned)
	}
	return out, nil
}

func (t *fakeWorkflowTx) AddLabel(_ context.Context, issueID, label, _ string) error {
	t.labels[issueID] = append(t.labels[issueID], label)
	return nil
}

func (t *fakeWorkflowTx) RemoveLabel(_ context.Context, issueID, label, _ string) error {
	labels := t.labels[issueID]
	filtered := labels[:0]
	for _, existing := range labels {
		if existing == label {
			continue
		}
		filtered = append(filtered, existing)
	}
	t.labels[issueID] = filtered
	return nil
}

func (t *fakeWorkflowTx) GetLabels(_ context.Context, issueID string) ([]string, error) {
	return append([]string(nil), t.labels[issueID]...), nil
}

func (t *fakeWorkflowTx) SetConfig(_ context.Context, key, value string) error {
	t.config[key] = value
	return nil
}

func (t *fakeWorkflowTx) GetConfig(_ context.Context, key string) (string, error) {
	return t.config[key], nil
}

func (t *fakeWorkflowTx) SetMetadata(_ context.Context, key, value string) error {
	t.meta[key] = value
	return nil
}

func (t *fakeWorkflowTx) GetMetadata(_ context.Context, key string) (string, error) {
	return t.meta[key], nil
}

func (t *fakeWorkflowTx) AddComment(_ context.Context, issueID, actor, comment string) error {
	t.comments[issueID] = append(t.comments[issueID], &types.Comment{
		IssueID:   issueID,
		Author:    actor,
		Text:      comment,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (t *fakeWorkflowTx) ImportIssueComment(_ context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	comment := &types.Comment{
		IssueID:   issueID,
		Author:    author,
		Text:      text,
		CreatedAt: createdAt,
	}
	t.comments[issueID] = append(t.comments[issueID], comment)
	return comment, nil
}

func (t *fakeWorkflowTx) GetIssueComments(_ context.Context, issueID string) ([]*types.Comment, error) {
	return append([]*types.Comment(nil), t.comments[issueID]...), nil
}

func cloneWorkflowIssue(issue *types.Issue) *types.Issue {
	if issue == nil {
		return nil
	}
	cloned := *issue
	if issue.Metadata != nil {
		cloned.Metadata = append([]byte(nil), issue.Metadata...)
	}
	if issue.ClosedAt != nil {
		closedAt := *issue.ClosedAt
		cloned.ClosedAt = &closedAt
	}
	return &cloned
}
