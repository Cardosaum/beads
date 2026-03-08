package main

import (
	"github.com/spf13/cobra"
)

var agentCurrentIssueCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current workflow issue as JSON",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		currentResult, err := resolveCurrentIssueForWorkflow(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("resolving current issue: %v", err)
		}
		if currentResult != nil {
			defer currentResult.Close()
		}

		status := "none"
		if currentResult != nil && currentResult.Issue != nil {
			status = "ok"
		}
		outputJSON(map[string]interface{}{
			"status":  status,
			"issue":   currentResultIssue(currentResult),
			"context": getWorkflowContext(),
		})
	},
}

var agentClaimReadyCmd = &cobra.Command{
	Use:   "claim-ready",
	Short: "Claim ready work with machine-oriented JSON output",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, runClaimReadyCommand)
	},
}

var agentEnsureCmd = &cobra.Command{
	Use:   "ensure [title]",
	Short: "Ensure active work with machine-oriented JSON output",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, runEnsureCommand)
	},
}

var agentDiscoverCmd = &cobra.Command{
	Use:   "discover [parent-id] [title]",
	Short: "Create discovered work with machine-oriented JSON output",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, func(cmd *cobra.Command, args []string) {
			runDiscover(cmd, args, false)
		})
	},
}

var agentDiscoverCurrentCmd = &cobra.Command{
	Use:   "discover-current [title]",
	Short: "Create discovered work from the current issue with machine-oriented JSON output",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, func(cmd *cobra.Command, args []string) {
			runDiscover(cmd, args, true)
		})
	},
}

var agentNoteCurrentCmd = &cobra.Command{
	Use:   "note-current [text]",
	Short: "Append notes to the current issue with machine-oriented JSON output",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, runNoteCurrentCommand)
	},
}

var agentUpdateCurrentCmd = &cobra.Command{
	Use:   "update-current",
	Short: "Update the current issue with machine-oriented JSON output",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, runUpdateCurrentCommand)
	},
}

var agentCloseCurrentCmd = &cobra.Command{
	Use:   "close-current",
	Short: "Close the current issue with machine-oriented JSON output",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runWithForcedJSON(cmd, args, runCloseCurrentCommand)
	},
}

func init() {
	registerWorkflowCreateFlags(agentEnsureCmd)
	registerWorkflowCreateFlags(agentDiscoverCmd)
	registerWorkflowCreateFlags(agentDiscoverCurrentCmd)
	agentNoteCurrentCmd.Flags().StringP("file", "f", "", "Read note text from file")
	registerUpdateFlags(agentUpdateCurrentCmd)
	registerCloseFlags(agentCloseCurrentCmd)

	agentClaimReadyCmd.Flags().Int("priority", 0, "Filter to a specific priority (0-4)")
	agentClaimReadyCmd.Flags().StringSlice("label", nil, "Require labels (AND semantics)")
	agentClaimReadyCmd.Flags().String("type", "", "Filter by issue type")
	agentClaimReadyCmd.Flags().Bool("include-assigned", false, "Consider ready issues even if already assigned")

	agentCmd.AddCommand(agentCurrentIssueCmd)
	agentCmd.AddCommand(agentClaimReadyCmd)
	agentCmd.AddCommand(agentEnsureCmd)
	agentCmd.AddCommand(agentDiscoverCmd)
	agentCmd.AddCommand(agentDiscoverCurrentCmd)
	agentCmd.AddCommand(agentNoteCurrentCmd)
	agentCmd.AddCommand(agentUpdateCurrentCmd)
	agentCmd.AddCommand(agentCloseCurrentCmd)
}

func runWithForcedJSON(cmd *cobra.Command, args []string, run func(cmd *cobra.Command, args []string)) {
	prev := jsonOutput
	jsonOutput = true
	defer func() {
		jsonOutput = prev
	}()
	run(cmd, args)
}

func currentResultIssue(result *RoutedResult) interface{} {
	if result == nil {
		return nil
	}
	return result.Issue
}
