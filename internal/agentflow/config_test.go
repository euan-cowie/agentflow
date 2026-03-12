package agentflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkflowConfigRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.toml")
	if err := os.WriteFile(path, []byte("[repo]\nname = \"demo\"\nunknown = \"nope\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, _, err := loadWorkflowConfig(path)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestMergeWorkflowConfigPrecedence(t *testing.T) {
	t.Parallel()

	builtins := defaultWorkflowConfig()
	global := WorkflowConfig{
		Repo: RepoConfig{
			DefaultSurface: "web",
		},
		Requirements: RequirementsConfig{
			Binaries: []string{"bun"},
		},
	}
	repo := WorkflowConfig{
		Repo: RepoConfig{
			BaseBranch:   "origin/develop",
			WorktreeRoot: "../sandbox-worktrees",
		},
		Env: EnvConfig{
			ManagedFile: ".env.repo-agentflow",
		},
	}

	merged := mergeWorkflowConfig(mergeWorkflowConfig(builtins, global), repo)
	if merged.Repo.BaseBranch != "origin/develop" {
		t.Fatalf("expected repo config to override base branch, got %q", merged.Repo.BaseBranch)
	}
	if merged.Repo.DefaultSurface != "web" {
		t.Fatalf("expected global config to set default surface, got %q", merged.Repo.DefaultSurface)
	}
	if merged.Env.ManagedFile != ".env.repo-agentflow" {
		t.Fatalf("expected repo config to override managed env file, got %q", merged.Env.ManagedFile)
	}
	if !contains(merged.Requirements.Binaries, "bun") {
		t.Fatalf("expected merged requirements to include bun, got %v", merged.Requirements.Binaries)
	}
}

func TestEffectiveManagedEnvFilesIncludesTargetsAndBindingTargets(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkflowConfig()
	cfg.Env.Targets = []EnvTargetConfig{
		{Path: "apps/web/.env.agentflow"},
		{Path: "apps/mobile/.env.agentflow"},
	}
	cfg.Ports.Bindings = []PortBindingConfig{
		{Target: "packages/api/.env.agentflow", Key: "PORT", Start: 5101, End: 5199},
	}

	files, err := effectiveManagedEnvFiles(cfg)
	if err != nil {
		t.Fatalf("effectiveManagedEnvFiles returned error: %v", err)
	}
	expected := []string{
		"apps/web/.env.agentflow",
		"apps/mobile/.env.agentflow",
		"packages/api/.env.agentflow",
	}
	if len(files) != len(expected) {
		t.Fatalf("expected %d managed env files, got %d (%v)", len(expected), len(files), files)
	}
	for _, want := range expected {
		if !contains(files, want) {
			t.Fatalf("expected managed env files to include %q, got %v", want, files)
		}
	}
}

func TestEffectivePortBindingsLegacyFallsBackToFirstEnvTarget(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkflowConfig()
	cfg.Env.Targets = []EnvTargetConfig{
		{Path: "apps/web/.env.agentflow"},
		{Path: "apps/mobile/.env.agentflow"},
	}
	cfg.Ports.Enabled = true
	cfg.Ports.File = ""
	cfg.Ports.Key = "VITE_PORT"
	cfg.Ports.Start = 4101
	cfg.Ports.End = 4199

	bindings, err := effectivePortBindings(cfg)
	if err != nil {
		t.Fatalf("effectivePortBindings returned error: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected one binding, got %d", len(bindings))
	}
	if bindings[0].Target != "apps/web/.env.agentflow" {
		t.Fatalf("expected legacy ports to target first env target, got %q", bindings[0].Target)
	}
}

func TestResolveWorktreeRootUsesRepoRootRelativePaths(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkflowConfig()
	cfg.Repo.WorktreeRoot = "../sandbox-worktrees"
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	runtime := RuntimeConfig{
		RepoRoot:  repoRoot,
		RepoID:    "repo-1234abcd",
		StateRoot: filepath.Join(t.TempDir(), "state"),
		Config:    cfg,
	}

	path, err := resolveWorktreeRoot(runtime)
	if err != nil {
		t.Fatalf("resolveWorktreeRoot returned error: %v", err)
	}

	expected := filepath.Clean(filepath.Join(repoRoot, "../sandbox-worktrees"))
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestResolveWorktreeRootUsesDeterministicStateHomeDefault(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	stateRoot := filepath.Join(t.TempDir(), "state")

	runtime := RuntimeConfig{
		RepoRoot:  repoRoot,
		RepoID:    "agentflow-840751fc",
		StateRoot: stateRoot,
		Config: WorkflowConfig{
			Repo: RepoConfig{
				Name:         "agentflow",
				WorktreeRoot: defaultWorktreeRootTemplate,
			},
		},
	}

	path, err := resolveWorktreeRoot(runtime)
	if err != nil {
		t.Fatalf("resolveWorktreeRoot returned error: %v", err)
	}

	expected := filepath.Join(stateRoot, "worktrees", "agentflow-840751fc")
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestStateAndConfigRootsRespectEnvironment(t *testing.T) {
	t.Setenv("AGENTFLOW_STATE_HOME", filepath.Join(t.TempDir(), "af-state"))
	t.Setenv("AGENTFLOW_CONFIG_HOME", filepath.Join(t.TempDir(), "af-config"))

	stateRoot, err := stateRootPath()
	if err != nil {
		t.Fatalf("stateRootPath returned error: %v", err)
	}
	configPath, err := globalConfigPath()
	if err != nil {
		t.Fatalf("globalConfigPath returned error: %v", err)
	}

	if stateRoot != filepath.Clean(os.Getenv("AGENTFLOW_STATE_HOME")) {
		t.Fatalf("unexpected state root: %q", stateRoot)
	}
	expectedConfig := filepath.Join(filepath.Clean(os.Getenv("AGENTFLOW_CONFIG_HOME")), "config.toml")
	if configPath != expectedConfig {
		t.Fatalf("unexpected config path: %q", configPath)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
