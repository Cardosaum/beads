package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAgentWorkflowConcurrency_OK(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("issue_id_mode: hash\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("Use bd discover and bd claim-ready.\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	check := CheckAgentWorkflowConcurrency(tmpDir)
	if check.Status != StatusOK {
		t.Fatalf("expected ok, got %s: %s", check.Status, check.Detail)
	}
}

func TestCheckAgentWorkflowConcurrency_WarnsOnCounterModeAndRawCreateDocs(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("issue_id_mode: counter\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("Run bd create for discovered work.\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	check := CheckAgentWorkflowConcurrency(tmpDir)
	if check.Status != StatusWarning {
		t.Fatalf("expected warning, got %s", check.Status)
	}
	if !strings.Contains(check.Detail, "issue_id_mode=counter") {
		t.Fatalf("expected counter mode detail, got %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "AGENTS.md") {
		t.Fatalf("expected AGENTS.md detail, got %q", check.Detail)
	}
	if !strings.Contains(check.Fix, "bd discover") || !strings.Contains(check.Fix, "issue_id_mode hash") {
		t.Fatalf("expected fix guidance, got %q", check.Fix)
	}
}
