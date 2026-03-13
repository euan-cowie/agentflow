package agentflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[unknown]\nvalue = 1\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, _, err := loadTOMLConfig[ConfigFile](path)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestResolveRuntimeConfigMergesRepoConfigWithBuiltins(t *testing.T) {
	t.Setenv("AGENTFLOW_STATE_HOME", filepath.Join(t.TempDir(), "state-home"))

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agentflow"), 0o755); err != nil {
		t.Fatalf("mkdir repo config dir: %v", err)
	}
	config := `
[repo]
name = "agentflow"
base_branch = "main"
branch_prefix = "feature"
default_surface = "cli"

[env]
targets = [{ path = ".env.agentflow" }]

[commands]
verify_quick = "go test ./..."

[requirements]
binaries = ["go"]
`
	if err := os.WriteFile(filepath.Join(repoRoot, ".agentflow", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	runtime, err := resolveRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("resolveRuntimeConfig returned error: %v", err)
	}
	if runtime.ConfigPath != canonicalPath(filepath.Join(repoRoot, ".agentflow", "config.toml")) {
		t.Fatalf("unexpected config path: %q", runtime.ConfigPath)
	}
	if !runtime.ConfigExists {
		t.Fatal("expected repo config to exist")
	}
	if runtime.EffectiveConfig.Repo.BaseBranch != "main" {
		t.Fatalf("expected repo config to override base branch, got %q", runtime.EffectiveConfig.Repo.BaseBranch)
	}
	if runtime.EffectiveConfig.Repo.BranchPrefix != "feature" {
		t.Fatalf("expected branch prefix, got %q", runtime.EffectiveConfig.Repo.BranchPrefix)
	}
	if runtime.EffectiveConfig.Repo.DefaultSurface != "cli" {
		t.Fatalf("expected default surface override, got %q", runtime.EffectiveConfig.Repo.DefaultSurface)
	}
	if runtime.EffectiveConfig.Repo.WorktreeRoot != defaultWorktreeRootTemplate {
		t.Fatalf("expected built-in worktree root, got %q", runtime.EffectiveConfig.Repo.WorktreeRoot)
	}
	if runtime.EffectiveConfig.Commands["verify_quick"] != "go test ./..." {
		t.Fatalf("expected config command, got %q", runtime.EffectiveConfig.Commands["verify_quick"])
	}
	if !contains(runtime.EffectiveConfig.Requirements.Binaries, "git") || !contains(runtime.EffectiveConfig.Requirements.Binaries, "go") {
		t.Fatalf("expected merged requirements, got %v", runtime.EffectiveConfig.Requirements.Binaries)
	}
}

func TestResolveRuntimeConfigRejectsLegacyManifestFile(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agentflow"), 0o755); err != nil {
		t.Fatalf("mkdir repo config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agentflow", "manifest.toml"), []byte("[env]\ntargets = [{ path = \".env.agentflow\" }]\n"), 0o644); err != nil {
		t.Fatalf("write legacy manifest: %v", err)
	}

	_, err := resolveRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected legacy manifest error")
	}
	if !strings.Contains(err.Error(), "merge it into") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkflowFingerprintIgnoresRepoAndRequirementsSections(t *testing.T) {
	t.Parallel()

	base := ConfigFile{
		Repo: RepoConfig{
			Name:       "demo",
			BaseBranch: "main",
		},
		Env: EnvConfig{
			Targets: []EnvTargetConfig{{Path: ".env.agentflow"}},
		},
	}
	first, err := workflowFingerprint(base)
	if err != nil {
		t.Fatalf("workflowFingerprint returned error: %v", err)
	}

	changed := base
	changed.Repo.BaseBranch = "develop"
	changed.Requirements.Binaries = []string{"go"}
	second, err := workflowFingerprint(changed)
	if err != nil {
		t.Fatalf("workflowFingerprint returned error: %v", err)
	}

	if first != second {
		t.Fatalf("expected repo/requirements changes to be ignored, got %q vs %q", first, second)
	}
}

func TestWorkflowFingerprintChangesForWorkflowSections(t *testing.T) {
	t.Parallel()

	cfg := ConfigFile{
		Env: EnvConfig{
			Targets: []EnvTargetConfig{{Path: ".env.agentflow"}},
		},
	}
	first, err := workflowFingerprint(cfg)
	if err != nil {
		t.Fatalf("workflowFingerprint returned error: %v", err)
	}

	cfg.Commands = map[string]string{"verify_quick": "go test ./..."}
	second, err := workflowFingerprint(cfg)
	if err != nil {
		t.Fatalf("workflowFingerprint returned error: %v", err)
	}

	if first == second {
		t.Fatal("expected workflow fingerprint to change when workflow sections change")
	}
}

func TestWorkflowTrustEntriesIncludeSideEffectfulWorkflow(t *testing.T) {
	t.Parallel()

	cfg := ConfigFile{
		Bootstrap: BootstrapConfig{
			Commands: []string{"bun install"},
			EnvFiles: []EnvFileMapping{{From: ".env.example", To: ".env.local"}},
		},
		Env: EnvConfig{
			Targets: []EnvTargetConfig{{Path: ".env.agentflow"}},
		},
		Ports: PortsConfig{
			Bindings: []PortBindingConfig{{Target: ".env.agentflow", Key: "PORT", Start: 4101, End: 4199}},
		},
		Commands: map[string]string{"verify_quick": "go test ./..."},
		Agents: map[string]AgentConfig{
			"default": {Command: "codex --no-alt-screen"},
		},
		Tmux: TmuxConfig{
			SessionName: "{{repo}}-{{task}}",
			Windows:     []TmuxWindowConfig{{Name: "editor", Command: "nvim ."}},
		},
	}

	entries := workflowTrustEntries(cfg)
	expected := []string{
		"bootstrap command: bun install",
		"bootstrap env file: .env.example -> .env.local",
		"managed env target: .env.agentflow",
		"port binding: PORT -> .env.agentflow [4101-4199]",
		"command verify_quick: go test ./...",
		"agent default: codex --no-alt-screen",
		"tmux session: {{repo}}-{{task}}",
		"tmux window editor: nvim .",
	}
	for _, want := range expected {
		if !contains(entries, want) {
			t.Fatalf("expected trust entries to include %q, got %v", want, entries)
		}
	}
}

func TestEffectiveManagedEnvFilesUsesDeclaredTargets(t *testing.T) {
	t.Parallel()

	cfg := defaultEffectiveConfig()
	cfg.Env.Targets = []EnvTargetConfig{
		{Path: "apps/web/.env.agentflow"},
		{Path: "apps/mobile/.env.agentflow"},
	}

	files, err := effectiveManagedEnvFiles(cfg)
	if err != nil {
		t.Fatalf("effectiveManagedEnvFiles returned error: %v", err)
	}
	expected := []string{
		"apps/web/.env.agentflow",
		"apps/mobile/.env.agentflow",
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

func TestEffectivePortBindingsReturnsDeclaredBindings(t *testing.T) {
	t.Parallel()

	cfg := defaultEffectiveConfig()
	cfg.Ports.Bindings = []PortBindingConfig{
		{Target: "apps/web/.env.agentflow", Key: "VITE_PORT", Start: 4101, End: 4199},
	}

	bindings, err := effectivePortBindings(cfg)
	if err != nil {
		t.Fatalf("effectivePortBindings returned error: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected one binding, got %d", len(bindings))
	}
	if bindings[0].Target != "apps/web/.env.agentflow" {
		t.Fatalf("expected binding target to be preserved, got %q", bindings[0].Target)
	}
}

func TestValidateEffectiveConfigRejectsUndeclaredBindingTarget(t *testing.T) {
	t.Parallel()

	cfg := defaultEffectiveConfig()
	cfg.Env.Targets = []EnvTargetConfig{{Path: "apps/web/.env.agentflow"}}
	cfg.Ports.Bindings = []PortBindingConfig{
		{Target: "packages/api/.env.agentflow", Key: "PORT", Start: 5101, End: 5199},
	}

	err := validateEffectiveConfig(cfg)
	if err == nil {
		t.Fatal("expected undeclared binding target to fail validation")
	}
}

func TestResolveWorktreeRootUsesRepoRootRelativePaths(t *testing.T) {
	t.Parallel()

	cfg := defaultEffectiveConfig()
	cfg.Repo.WorktreeRoot = "../sandbox-worktrees"
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	runtime := RuntimeConfig{
		RepoRoot:        repoRoot,
		RepoID:          "repo-1234abcd",
		StateRoot:       filepath.Join(t.TempDir(), "state"),
		EffectiveConfig: cfg,
	}

	path, err := resolveWorktreeRoot(runtime)
	if err != nil {
		t.Fatalf("resolveWorktreeRoot returned error: %v", err)
	}

	expected := canonicalPath(filepath.Join(repoRoot, "../sandbox-worktrees"))
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
		EffectiveConfig: EffectiveConfig{
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

	expected := canonicalPath(filepath.Join(stateRoot, "worktrees", "agentflow-840751fc"))
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestStateRootRespectsEnvironment(t *testing.T) {
	t.Setenv("AGENTFLOW_STATE_HOME", filepath.Join(t.TempDir(), "af-state"))

	stateRoot, err := stateRootPath()
	if err != nil {
		t.Fatalf("stateRootPath returned error: %v", err)
	}

	if stateRoot != filepath.Clean(os.Getenv("AGENTFLOW_STATE_HOME")) {
		t.Fatalf("unexpected state root: %q", stateRoot)
	}
}

func TestSampleConfigParses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(SampleConfig("/tmp/agentflow")), 0o644); err != nil {
		t.Fatalf("write sample config: %v", err)
	}
	cfg, _, exists, err := loadTOMLConfig[ConfigFile](path)
	if err != nil {
		t.Fatalf("load sample config: %v", err)
	}
	if !exists || cfg.Repo.Name == "" || len(cfg.Env.Targets) != 1 {
		t.Fatalf("unexpected sample config: %+v", cfg)
	}
}

func TestWriteConfigWritesExpectedPath(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	path, err := WriteConfig(repoRoot, false)
	if err != nil {
		t.Fatalf("WriteConfig returned error: %v", err)
	}
	if path != filepath.Join(repoRoot, ".agentflow", "config.toml") {
		t.Fatalf("unexpected config path: %q", path)
	}
}

func TestRenderEffectiveConfigOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	cfg := defaultEffectiveConfig()
	content, err := RenderEffectiveConfig(cfg, "toml")
	if err != nil {
		t.Fatalf("RenderEffectiveConfig returned error: %v", err)
	}
	for _, unexpected := range []string{
		"agent = ''",
		"command = ''",
		"bindings = []",
		"mcp_servers = []",
	} {
		if strings.Contains(content, unexpected) {
			t.Fatalf("expected rendered config to omit %q, got:\n%s", unexpected, content)
		}
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
