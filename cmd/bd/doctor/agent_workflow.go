package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// CheckAgentWorkflowConcurrency warns about agent-facing workflows that are
// likely to create duplicate work or trigger avoidable write contention.
func CheckAgentWorkflowConcurrency(repoPath string) DoctorCheck {
	var findings []string
	var fixes []string

	configPath := findConfigPath(repoPath)
	if configPath != "" {
		v := viper.New()
		v.SetConfigType("yaml")
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err == nil {
			if strings.EqualFold(v.GetString("issue_id_mode"), "counter") {
				findings = append(findings,
					"issue_id_mode=counter configured: sequential IDs are human-friendly but create more write contention under multi-agent parallelism")
				fixes = append(fixes, "Use hash IDs for multi-agent repos: bd config set issue_id_mode hash")
			}
		}
	}

	docFiles := []string{
		filepath.Join(repoPath, "AGENTS.md"),
		filepath.Join(repoPath, "CLAUDE.md"),
		filepath.Join(repoPath, ".codex", "instructions.md"),
		filepath.Join(repoPath, ".claude", "CLAUDE.md"),
		filepath.Join(repoPath, "claude.local.md"),
		filepath.Join(repoPath, ".claude", "claude.local.md"),
	}
	var unsafeDocs []string
	for _, docFile := range docFiles {
		data, err := os.ReadFile(docFile) // #nosec G304 -- controlled project paths
		if err != nil {
			continue
		}
		content := strings.ToLower(string(data))
		hasRawCreate := strings.Contains(content, "bd create")
		hasRawUpdate := strings.Contains(content, "bd update")
		hasRawClose := strings.Contains(content, "bd close")
		hasSaferWorkflow := strings.Contains(content, "bd discover") ||
			strings.Contains(content, "bd ensure") ||
			strings.Contains(content, "bd claim-ready") ||
			strings.Contains(content, "bd update-current") ||
			strings.Contains(content, "bd close-current")
		if (hasRawCreate || hasRawUpdate || hasRawClose) && !hasSaferWorkflow {
			unsafeDocs = append(unsafeDocs, filepath.Base(docFile))
		}
	}
	if len(unsafeDocs) > 0 {
		findings = append(findings,
			fmt.Sprintf("agent docs mention raw mutation commands but not agent-safe helpers in: %s", strings.Join(unsafeDocs, ", ")))
		fixes = append(fixes, "Teach agents to prefer bd discover, bd ensure, bd claim-ready, bd update-current, and bd close-current in repo docs")
	}

	if len(findings) == 0 {
		return DoctorCheck{
			Name:     "Agent Workflow Concurrency",
			Status:   StatusOK,
			Message:  "Agent workflow guidance looks concurrency-aware",
			Category: CategoryIntegration,
		}
	}

	return DoctorCheck{
		Name:     "Agent Workflow Concurrency",
		Status:   StatusWarning,
		Message:  fmt.Sprintf("Found %d agent workflow concurrency risk(s)", len(findings)),
		Detail:   strings.Join(findings, "\n"),
		Fix:      strings.Join(fixes, "\n"),
		Category: CategoryIntegration,
	}
}
