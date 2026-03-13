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

func TestWriteManagedEnvFilesSupportsMultipleTargets(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	written, err := writeManagedEnvFiles(worktree, []string{
		"apps/web/.env.agentflow",
		"packages/api/.env.agentflow",
	}, map[string]map[string]string{
		"apps/web/.env.agentflow": {
			"VITE_PORT": "4101",
		},
		"packages/api/.env.agentflow": {
			"PORT": "5101",
		},
	})
	if err != nil {
		t.Fatalf("writeManagedEnvFiles returned error: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("expected two written files, got %d", len(written))
	}

	webData, err := os.ReadFile(filepath.Join(worktree, "apps/web/.env.agentflow"))
	if err != nil {
		t.Fatalf("read web env file: %v", err)
	}
	if !strings.Contains(string(webData), "VITE_PORT=4101") {
		t.Fatalf("unexpected web env content: %q", string(webData))
	}

	apiData, err := os.ReadFile(filepath.Join(worktree, "packages/api/.env.agentflow"))
	if err != nil {
		t.Fatalf("read api env file: %v", err)
	}
	if !strings.Contains(string(apiData), "PORT=5101") {
		t.Fatalf("unexpected api env content: %q", string(apiData))
	}
}

func TestRemoveManagedEnvFilesRemovesDeclaredTargets(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	if _, err := writeManagedEnvFiles(worktree, []string{
		"apps/web/.env.agentflow",
		"packages/api/.env.agentflow",
	}, map[string]map[string]string{
		"apps/web/.env.agentflow": {
			"VITE_PORT": "4101",
		},
		"packages/api/.env.agentflow": {
			"PORT": "5101",
		},
	}); err != nil {
		t.Fatalf("writeManagedEnvFiles returned error: %v", err)
	}

	if err := removeManagedEnvFiles(worktree, []string{
		"apps/web/.env.agentflow",
		"packages/api/.env.agentflow",
	}); err != nil {
		t.Fatalf("removeManagedEnvFiles returned error: %v", err)
	}

	for _, target := range []string{
		"apps/web/.env.agentflow",
		"packages/api/.env.agentflow",
	} {
		if _, err := os.Stat(filepath.Join(worktree, target)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", target, err)
		}
	}
}

func TestRemoveManagedEnvFilesRejectsEscape(t *testing.T) {
	t.Parallel()

	worktree := t.TempDir()
	err := removeManagedEnvFiles(worktree, []string{"../outside/.env.agentflow"})
	if err == nil {
		t.Fatal("expected removeManagedEnvFiles to reject paths outside the worktree")
	}
	if !strings.Contains(err.Error(), "escapes worktree") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildTaskEnvStateAllocatesBindingsPerTarget(t *testing.T) {
	cfg := defaultEffectiveConfig()
	cfg.Env.Targets = []EnvTargetConfig{
		{Path: "apps/web/.env.agentflow"},
		{Path: "packages/api/.env.agentflow"},
	}
	cfg.Ports.Bindings = []PortBindingConfig{
		{Target: "apps/web/.env.agentflow", Key: "VITE_PORT", Start: 34001, End: 34100},
		{Target: "packages/api/.env.agentflow", Key: "PORT", Start: 35001, End: 35100},
	}
	originalAllocator := preferredPortAllocator
	preferredPortAllocator = func(start, end int, reserved map[int]struct{}) (int, error) {
		for port := start; port <= end; port++ {
			if _, exists := reserved[port]; exists {
				continue
			}
			return port, nil
		}
		return 0, nil
	}
	defer func() {
		preferredPortAllocator = originalAllocator
	}()

	targets, bindings, err := buildTaskEnvState(cfg)
	if err != nil {
		t.Fatalf("buildTaskEnvState returned error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected two targets, got %d", len(targets))
	}
	if len(bindings) != 2 {
		t.Fatalf("expected two bindings, got %d", len(bindings))
	}
	if bindings[0].Target == bindings[1].Target {
		t.Fatalf("expected bindings to target different files, got %+v", bindings)
	}
	if bindings[0].Port == 0 || bindings[1].Port == 0 {
		t.Fatalf("expected allocated ports, got %+v", bindings)
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
