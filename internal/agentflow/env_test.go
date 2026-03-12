package agentflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteManagedEnvFileSortedAndStable(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	path, err := writeManagedEnvFile(worktree, ".env.agentflow", map[string]string{
		"VITE_PORT": "4101",
		"FOO":       "bar",
	})
	if err != nil {
		t.Fatalf("writeManagedEnvFile returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read managed env file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Managed by agentflow") {
		t.Fatalf("expected managed file header, got %q", content)
	}
	if strings.Index(content, "FOO=bar") > strings.Index(content, "VITE_PORT=4101") {
		t.Fatalf("expected sorted key order, got %q", content)
	}
}

func TestSeedEnvFilesCopiesOnlyWhenMissing(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	source := filepath.Join(worktree, ".env.example")
	target := filepath.Join(worktree, ".env.local")

	if err := os.WriteFile(source, []byte("FIRST=1\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := seedEnvFiles(worktree, []EnvFileMapping{{From: ".env.example", To: ".env.local"}}); err != nil {
		t.Fatalf("seedEnvFiles returned error: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "FIRST=1\n" {
		t.Fatalf("unexpected seeded content: %q", string(data))
	}

	if err := os.WriteFile(target, []byte("USER=1\n"), 0o644); err != nil {
		t.Fatalf("overwrite target: %v", err)
	}
	if err := seedEnvFiles(worktree, []EnvFileMapping{{From: ".env.example", To: ".env.local"}}); err != nil {
		t.Fatalf("seedEnvFiles returned error: %v", err)
	}
	data, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target after reseed: %v", err)
	}
	if string(data) != "USER=1\n" {
		t.Fatalf("expected existing file to remain unchanged, got %q", string(data))
	}
}
