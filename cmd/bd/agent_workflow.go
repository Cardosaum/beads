package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
	"github.com/steveyegge/beads/internal/validation"
)

type ensuredIssueResult struct {
	Issue   *types.Issue `json:"issue"`
	Created bool         `json:"created"`
	Deduped bool         `json:"deduped"`
}

type issueCreateSpec struct {
	Title       string
	Description string
	Notes       string
	IssueType   types.IssueType
	Priority    int
	Assignee    string
	Labels      []string
	DedupeKeys  map[string]string
	Metadata    json.RawMessage
}

type workflowStore interface {
	RunInTransaction(ctx context.Context, commitMsg string, fn func(tx storage.Transaction) error) error
}

var discoverCmd = &cobra.Command{
	Use:   "discover [parent-id] [title]",
	Short: "Create or reuse discovered follow-up work",
	Long: `Create a discovered-from follow-up issue, deduping against existing open work.

Examples:
  bd discover bd-42 "Fix follow-up race"
  bd discover-current "Need docs for this edge case"`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		runDiscover(cmd, args, false)
	},
}

var discoverCurrentCmd = &cobra.Command{
	Use:   "discover-current [title]",
	Short: "Create or reuse discovered follow-up work from the current issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runDiscover(cmd, args, true)
	},
}

var noteCurrentCmd = &cobra.Command{
	Use:   "note-current [text]",
	Short: "Append notes to the current issue",
	Args:  cobra.MaximumNArgs(1),
	Run:   runNoteCurrentCommand,
}

var updateCurrentCmd = &cobra.Command{
	Use:   "update-current",
	Short: "Update the current issue",
	Args:  cobra.NoArgs,
	Run:   runUpdateCurrentCommand,
}

var closeCurrentCmd = &cobra.Command{
	Use:   "close-current",
	Short: "Close the current issue",
	Args:  cobra.NoArgs,
	Run:   runCloseCurrentCommand,
}

var ensureCmd = &cobra.Command{
	Use:   "ensure [title]",
	Short: "Find or create an active issue by exact title",
	Args:  cobra.ExactArgs(1),
	Run:   runEnsureCommand,
}

var claimReadyCmd = &cobra.Command{
	Use:   "claim-ready",
	Short: "Atomically claim the highest-priority ready issue",
	Args:  cobra.NoArgs,
	Run:   runClaimReadyCommand,
}

func init() {
	registerWorkflowCreateFlags(discoverCmd)
	registerWorkflowCreateFlags(discoverCurrentCmd)
	registerWorkflowCreateFlags(ensureCmd)
	registerUpdateFlags(updateCurrentCmd)
	registerCloseFlags(closeCurrentCmd)

	noteCurrentCmd.Flags().StringP("file", "f", "", "Read note text from file")

	claimReadyCmd.Flags().Int("priority", 0, "Filter to a specific priority (0-4)")
	claimReadyCmd.Flags().StringSlice("label", nil, "Require labels (AND semantics)")
	claimReadyCmd.Flags().String("type", "", "Filter by issue type")
	claimReadyCmd.Flags().Bool("include-assigned", false, "Consider ready issues even if already assigned")

	rootCmd.AddCommand(discoverCmd)
	rootCmd.AddCommand(discoverCurrentCmd)
	rootCmd.AddCommand(noteCurrentCmd)
	rootCmd.AddCommand(updateCurrentCmd)
	rootCmd.AddCommand(closeCurrentCmd)
	rootCmd.AddCommand(ensureCmd)
	rootCmd.AddCommand(claimReadyCmd)
}

func registerWorkflowCreateFlags(cmd *cobra.Command) {
	registerPriorityFlag(cmd, "2")
	cmd.Flags().StringP("type", "t", "task", "Issue type")
	cmd.Flags().StringP("assignee", "a", "", "Assignee")
	cmd.Flags().StringP("description", "d", "", "Issue description")
	cmd.Flags().String("body", "", "Alias for --description")
	_ = cmd.Flags().MarkHidden("body")
	cmd.Flags().StringP("message", "m", "", "Alias for --description")
	_ = cmd.Flags().MarkHidden("message")
	cmd.Flags().String("body-file", "", "Read description from file (use - for stdin)")
	cmd.Flags().String("description-file", "", "Alias for --body-file")
	_ = cmd.Flags().MarkHidden("description-file")
	cmd.Flags().Bool("stdin", false, "Read description from stdin")
	cmd.MarkFlagsMutuallyExclusive("stdin", "body-file")
	cmd.MarkFlagsMutuallyExclusive("stdin", "description-file")
	cmd.MarkFlagsMutuallyExclusive("stdin", "description")
	cmd.MarkFlagsMutuallyExclusive("stdin", "body")
	cmd.MarkFlagsMutuallyExclusive("stdin", "message")
	cmd.Flags().String("notes", "", "Additional notes")
	cmd.Flags().StringSliceP("labels", "l", nil, "Labels to add")
	cmd.Flags().StringSlice("label", nil, "Alias for --labels")
	_ = cmd.Flags().MarkHidden("label")
	cmd.Flags().StringArray("dedupe-key", nil, "Dedupe fingerprint key=value (repeatable, exact match across open issues)")
}

func runNoteCurrentCommand(cmd *cobra.Command, args []string) {
	CheckReadonly("note-current")

	noteText := readPositionalOrFile(cmd, args)
	if strings.TrimSpace(noteText) == "" {
		FatalErrorRespectJSON("note text cannot be empty")
	}
	ctx := rootCtx
	currentResult, err := resolveCurrentIssueForWorkflow(ctx)
	if err != nil {
		FatalErrorRespectJSON("resolving current issue: %v", err)
	}
	if currentResult == nil || currentResult.Issue == nil {
		FatalErrorRespectJSON("no current issue found (no in-progress, hooked, or recent workflow context)")
	}
	defer currentResult.Close()
	currentID := currentResult.ResolvedID

	issue := currentResult.Issue
	combined := issue.Notes
	if combined != "" {
		combined += "\n"
	}
	combined += noteText

	if err := currentResult.Store.UpdateIssue(ctx, currentID, map[string]interface{}{"notes": combined}, actor); err != nil {
		FatalErrorRespectJSON("updating notes for %s: %v", currentID, err)
	}

	updated, err := currentResult.Store.GetIssue(ctx, currentID)
	if err != nil {
		FatalErrorRespectJSON("reloading %s: %v", currentID, err)
	}

	SetLastTouchedID(currentID)
	setWorkflowCurrentIssue(currentID)

	if jsonOutput {
		outputJSON(updated)
		return
	}

	fmt.Printf("%s Appended notes to %s\n", ui.RenderPass("✓"), formatFeedbackID(currentID, updated.Title))
}

func runUpdateCurrentCommand(cmd *cobra.Command, _ []string) {
	CheckReadonly("update-current")

	currentResult, err := resolveCurrentIssueForWorkflow(rootCtx)
	if err != nil {
		FatalErrorRespectJSON("resolving current issue: %v", err)
	}
	if currentResult == nil || currentResult.Issue == nil {
		FatalErrorRespectJSON("no current issue found (no in-progress, hooked, or recent workflow context)")
	}
	defer currentResult.Close()

	setWorkflowCurrentIssue(currentResult.ResolvedID)
	runUpdateCommand(cmd, []string{currentResult.ResolvedID})
}

func runCloseCurrentCommand(cmd *cobra.Command, _ []string) {
	CheckReadonly("close-current")

	currentResult, err := resolveCurrentIssueForWorkflow(rootCtx)
	if err != nil {
		FatalErrorRespectJSON("resolving current issue: %v", err)
	}
	if currentResult == nil || currentResult.Issue == nil {
		FatalErrorRespectJSON("no current issue found (no in-progress, hooked, or recent workflow context)")
	}
	defer currentResult.Close()

	runCloseCommand(cmd, []string{currentResult.ResolvedID})
}

func runEnsureCommand(cmd *cobra.Command, args []string) {
	CheckReadonly("ensure")

	spec := parseWorkflowCreateSpec(cmd, args[0])
	result, err := ensureIssue(rootCtx, store, spec, actor)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if result.Created && hookRunner != nil {
		hookRunner.Run(hooks.EventCreate, result.Issue)
	}
	SetLastTouchedID(result.Issue.ID)
	setWorkflowCurrentIssue(result.Issue.ID)

	if jsonOutput {
		outputJSON(result)
		return
	}

	if result.Created {
		fmt.Printf("%s Created issue: %s\n", ui.RenderPass("✓"), formatFeedbackID(result.Issue.ID, result.Issue.Title))
		return
	}
	fmt.Printf("%s Reusing existing issue: %s\n", ui.RenderAccent("↺"), formatFeedbackID(result.Issue.ID, result.Issue.Title))
}

func runClaimReadyCommand(cmd *cobra.Command, _ []string) {
	CheckReadonly("claim-ready")

	ctx := rootCtx
	labels, _ := cmd.Flags().GetStringSlice("label")
	issueType, _ := cmd.Flags().GetString("type")
	issueType = utils.NormalizeIssueType(issueType)
	includeAssigned, _ := cmd.Flags().GetBool("include-assigned")
	filter := types.WorkFilter{
		Status:     types.StatusOpen,
		Unassigned: !includeAssigned,
		Labels:     utils.NormalizeLabels(labels),
	}
	if cmd.Flags().Changed("priority") {
		priority, _ := cmd.Flags().GetInt("priority")
		filter.Priority = &priority
	}
	if issueType != "" {
		filter.Type = issueType
	}

	activeStore := store
	routedStore, routed, err := openRoutedReadStore(ctx, activeStore)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}
	if routed {
		defer func() { _ = routedStore.Close() }()
		activeStore = routedStore
	}

	readyIssues, err := activeStore.GetReadyWork(ctx, filter)
	if err != nil {
		FatalErrorRespectJSON("getting ready work: %v", err)
	}

	for _, issue := range readyIssues {
		err := activeStore.ClaimIssue(ctx, issue.ID, actor)
		if err == nil {
			claimed, getErr := activeStore.GetIssue(ctx, issue.ID)
			if getErr != nil {
				FatalErrorRespectJSON("reloading %s: %v", issue.ID, getErr)
			}
			SetLastTouchedID(issue.ID)
			setWorkflowCurrentIssue(issue.ID)
			setWorkflowLastReadyIssue(issue.ID)
			if jsonOutput {
				outputJSON(claimed)
				return
			}
			fmt.Printf("%s Claimed %s\n", ui.RenderPass("✓"), formatFeedbackID(issue.ID, claimed.Title))
			return
		}
		if errors.Is(err, storage.ErrAlreadyClaimed) {
			continue
		}
		fmt.Fprintf(os.Stderr, "Warning: failed to claim %s: %v\n", issue.ID, err)
	}

	FatalErrorRespectJSON("no ready issue could be claimed")
}

func runDiscover(cmd *cobra.Command, args []string, currentParent bool) {
	CheckReadonly("discover")

	ctx := rootCtx
	var parentArg string
	var title string
	if currentParent {
		title = args[0]
		currentResult, err := resolveCurrentIssueForWorkflow(ctx)
		if err != nil {
			FatalErrorRespectJSON("resolving current issue: %v", err)
		}
		if currentResult == nil || currentResult.Issue == nil {
			FatalErrorRespectJSON("no current issue found (no in-progress, hooked, or recently touched issues)")
		}
		defer currentResult.Close()
		parentArg = currentResult.ResolvedID
	} else if len(args) == 1 {
		title = args[0]
		currentResult, err := resolveCurrentIssueForWorkflow(ctx)
		if err != nil {
			FatalErrorRespectJSON("resolving current issue: %v", err)
		}
		if currentResult == nil || currentResult.Issue == nil {
			FatalErrorRespectJSON("no parent ID provided and no current issue found")
		}
		defer currentResult.Close()
		parentArg = currentResult.ResolvedID
	} else {
		parentArg = args[0]
		title = args[1]
	}

	parentResult, err := resolveAndGetIssueWithRouting(ctx, store, parentArg)
	if err != nil {
		FatalErrorRespectJSON("resolving parent %s: %v", parentArg, err)
	}
	defer parentResult.Close()
	parentID := parentResult.ResolvedID

	spec := parseWorkflowCreateSpec(cmd, title)
	result, err := discoverIssue(ctx, parentResult.Store, parentID, spec, actor)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if result.Created && hookRunner != nil {
		hookRunner.Run(hooks.EventCreate, result.Issue)
	}
	SetLastTouchedID(result.Issue.ID)
	setWorkflowCurrentParent(parentID)

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"issue":     result.Issue,
			"created":   result.Created,
			"deduped":   result.Deduped,
			"parent_id": parentID,
		})
		return
	}

	if result.Created {
		fmt.Printf("%s Created discovered issue: %s (from %s)\n", ui.RenderPass("✓"), formatFeedbackID(result.Issue.ID, result.Issue.Title), parentID)
		return
	}
	fmt.Printf("%s Reusing discovered issue: %s (from %s)\n", ui.RenderAccent("↺"), formatFeedbackID(result.Issue.ID, result.Issue.Title), parentID)
}

func parseWorkflowCreateSpec(cmd *cobra.Command, title string) issueCreateSpec {
	description, _ := getDescriptionFlag(cmd)
	notes, _ := cmd.Flags().GetString("notes")
	priorityStr, _ := cmd.Flags().GetString("priority")
	priority, err := validation.ValidatePriority(priorityStr)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}
	issueType, _ := cmd.Flags().GetString("type")
	issueType = utils.NormalizeIssueType(issueType)
	if err := validateWorkflowIssueType(issueType); err != nil {
		FatalErrorRespectJSON("%v", err)
	}
	assignee, _ := cmd.Flags().GetString("assignee")
	labels, _ := cmd.Flags().GetStringSlice("labels")
	labelAlias, _ := cmd.Flags().GetStringSlice("label")
	labels = append(labels, labelAlias...)
	dedupeKeyFlags, _ := cmd.Flags().GetStringArray("dedupe-key")
	dedupeKeys := parseWorkflowDedupeKeys(dedupeKeyFlags)
	metadata := workflowMetadataFromDedupeKeys(dedupeKeys)

	return issueCreateSpec{
		Title:       strings.TrimSpace(title),
		Description: description,
		Notes:       notes,
		IssueType:   types.IssueType(issueType).Normalize(),
		Priority:    priority,
		Assignee:    assignee,
		Labels:      utils.NormalizeLabels(labels),
		DedupeKeys:  dedupeKeys,
		Metadata:    metadata,
	}
}

func validateWorkflowIssueType(issueType string) error {
	var customTypes []string
	if store != nil {
		ct, err := store.GetCustomTypes(rootCtx)
		if err == nil {
			customTypes = ct
		}
	}
	if len(customTypes) == 0 {
		customTypes = config.GetCustomTypesFromYAML()
	}
	if !types.IssueType(issueType).IsValidWithCustom(customTypes) {
		validTypes := "bug, feature, task, epic, chore, decision"
		if len(customTypes) > 0 {
			validTypes += ", " + strings.Join(customTypes, ", ")
		}
		return fmt.Errorf("invalid issue type %q. Valid types: %s", issueType, validTypes)
	}
	return nil
}

func ensureIssue(ctx context.Context, s workflowStore, spec issueCreateSpec, actor string) (*ensuredIssueResult, error) {
	return ensureIssueWithParent(ctx, s, spec, "", actor)
}

func discoverIssue(ctx context.Context, s workflowStore, parentID string, spec issueCreateSpec, actor string) (*ensuredIssueResult, error) {
	return ensureIssueWithParent(ctx, s, spec, parentID, actor)
}

func ensureIssueWithParent(ctx context.Context, s workflowStore, spec issueCreateSpec, parentID string, actor string) (*ensuredIssueResult, error) {
	if strings.TrimSpace(spec.Title) == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}

	result := &ensuredIssueResult{}
	commitMsg := "bd: ensure issue"
	if parentID != "" {
		commitMsg = fmt.Sprintf("bd: discover from %s", parentID)
	}

	err := s.RunInTransaction(ctx, commitMsg, func(tx storage.Transaction) error {
		if parentID != "" {
			parentIssue, err := tx.GetIssue(ctx, parentID)
			if err != nil {
				return fmt.Errorf("get parent %s: %w", parentID, err)
			}
			existing, err := findWorkflowIssueMatch(ctx, tx, spec, parentID)
			if err != nil {
				return err
			}
			if existing != nil {
				result.Issue = existing
				return nil
			}

			newIssue := workflowIssueFromSpec(spec)
			if parentIssue.SourceRepo != "" {
				newIssue.SourceRepo = parentIssue.SourceRepo
			}
			if err := tx.CreateIssue(ctx, newIssue, actor); err != nil {
				return fmt.Errorf("create discovered issue: %w", err)
			}
			dep := &types.Dependency{
				IssueID:     newIssue.ID,
				DependsOnID: parentID,
				Type:        types.DepDiscoveredFrom,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("add discovered-from dependency: %w", err)
			}
			for _, label := range spec.Labels {
				if err := tx.AddLabel(ctx, newIssue.ID, label, actor); err != nil {
					return fmt.Errorf("add label %s: %w", label, err)
				}
			}
			result.Issue = newIssue
			result.Created = true
			return nil
		}

		existing, err := findWorkflowIssueMatch(ctx, tx, spec, "")
		if err != nil {
			return err
		}
		if existing != nil {
			result.Issue = existing
			return nil
		}

		newIssue := workflowIssueFromSpec(spec)
		if err := tx.CreateIssue(ctx, newIssue, actor); err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		for _, label := range spec.Labels {
			if err := tx.AddLabel(ctx, newIssue.ID, label, actor); err != nil {
				return fmt.Errorf("add label %s: %w", label, err)
			}
		}
		result.Issue = newIssue
		result.Created = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	result.Deduped = !result.Created
	if result.Issue == nil {
		return nil, fmt.Errorf("ensure transaction completed without an issue")
	}
	return result, nil
}

func workflowIssueFromSpec(spec issueCreateSpec) *types.Issue {
	return &types.Issue{
		Title:       spec.Title,
		Description: spec.Description,
		Notes:       spec.Notes,
		Status:      types.StatusOpen,
		Priority:    spec.Priority,
		IssueType:   spec.IssueType,
		Assignee:    spec.Assignee,
		Metadata:    spec.Metadata,
		CreatedBy:   getActorWithGit(),
		Owner:       getOwner(),
	}
}

func findWorkflowIssueMatch(ctx context.Context, tx storage.Transaction, spec issueCreateSpec, discoveredFromParent string) (*types.Issue, error) {
	filter := types.IssueFilter{ExcludeStatus: []types.Status{types.StatusClosed}}
	if len(spec.DedupeKeys) > 0 {
		filter.MetadataFields = spec.DedupeKeys
	} else {
		filter.TitleSearch = spec.Title
	}
	candidates, err := tx.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("search existing issues: %w", err)
	}

	normalizedTitle := normalizeWorkflowTitle(spec.Title)
	for _, candidate := range candidates {
		if len(spec.DedupeKeys) == 0 && normalizeWorkflowTitle(candidate.Title) != normalizedTitle {
			continue
		}
		if discoveredFromParent == "" {
			return candidate, nil
		}
		deps, err := tx.GetDependencyRecords(ctx, candidate.ID)
		if err != nil {
			return nil, fmt.Errorf("get dependencies for %s: %w", candidate.ID, err)
		}
		for _, dep := range deps {
			if dep.Type == types.DepDiscoveredFrom && dep.DependsOnID == discoveredFromParent {
				return candidate, nil
			}
		}
	}
	return nil, nil
}

func normalizeWorkflowTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(title)), " "))
}

func parseWorkflowDedupeKeys(flags []string) map[string]string {
	if len(flags) == 0 {
		return nil
	}

	keys := make(map[string]string, len(flags))
	for _, kv := range flags {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(key) == "" {
			FatalErrorRespectJSON("invalid --dedupe-key: expected key=value, got %q", kv)
		}
		key = strings.TrimSpace(key)
		if err := storage.ValidateMetadataKey(key); err != nil {
			FatalErrorRespectJSON("invalid --dedupe-key %q: %v", key, err)
		}
		keys[key] = strings.TrimSpace(value)
	}
	return keys
}

func workflowMetadataFromDedupeKeys(keys map[string]string) json.RawMessage {
	if len(keys) == 0 {
		return nil
	}

	data, err := json.Marshal(keys)
	if err != nil {
		FatalErrorRespectJSON("serializing dedupe metadata: %v", err)
	}
	return json.RawMessage(data)
}

func readPositionalOrFile(cmd *cobra.Command, args []string) string {
	filePath, _ := cmd.Flags().GetString("file")
	if filePath != "" {
		data, err := os.ReadFile(filePath) // #nosec G304 -- explicit user-provided file path
		if err != nil {
			FatalErrorRespectJSON("reading file: %v", err)
		}
		return string(data)
	}
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func resolveCurrentIssueForWorkflow(ctx context.Context) (*RoutedResult, error) {
	currentID := resolveCurrentIssueID(ctx)
	if currentID != "" {
		return resolveAndGetIssueWithRouting(ctx, store, currentID)
	}

	routedStore, routed, err := openRoutedReadStore(ctx, store)
	if err != nil || !routed {
		return nil, err
	}

	currentID = findCurrentIssueIDInStore(ctx, routedStore)
	if currentID == "" {
		_ = routedStore.Close()
		return nil, nil
	}

	result, err := resolveAndGetFromStore(ctx, routedStore, currentID, true)
	if err != nil {
		_ = routedStore.Close()
		return nil, err
	}
	result.closeFn = func() { _ = routedStore.Close() }
	return result, nil
}

func findCurrentIssueIDInStore(ctx context.Context, activeStore *dolt.DoltStore) string {
	if activeStore == nil {
		return ""
	}

	currentActor := getActorWithGit()
	if currentActor != "" {
		status := types.StatusInProgress
		filter := types.IssueFilter{Status: &status, Assignee: &currentActor}
		issues, err := activeStore.SearchIssues(ctx, "", filter)
		if err == nil && len(issues) > 0 {
			return issues[0].ID
		}
	}
	if currentActor != "" {
		status := types.StatusHooked
		filter := types.IssueFilter{Status: &status, Assignee: &currentActor}
		issues, err := activeStore.SearchIssues(ctx, "", filter)
		if err == nil && len(issues) > 0 {
			return issues[0].ID
		}
	}
	return ""
}
