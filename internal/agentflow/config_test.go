package agentflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobalConfigRejectsManifestSections(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[commands]\nverify_quick = \"make test\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, _, err := loadTOMLConfig[GlobalConfig](path)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadRepoConfigRejectsManifestSections(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[env]\ntargets = [{ path = \".env.agentflow\" }]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, _, err := loadTOMLConfig[RepoConfigFile](path)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadManifestRejectsRepoSection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.toml")
	if err := os.WriteFile(path, []byte("[repo]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, _, _, err := loadTOMLConfig[ManifestFile](path)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestResolveRuntimeConfigMergesGlobalRepoAndManifest(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("AGENTFLOW_CONFIG_HOME", configHome)
	t.Setenv("AGENTFLOW_STATE_HOME", filepath.Join(t.TempDir(), "state-home"))

	globalPath := filepath.Join(configHome, "config.toml")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatalf("mkdir global config dir: %v", err)
	}
	globalConfig := `
[defaults.repo]
base_branch = "origin/develop"
worktree_root = "{{agentflow_state_home}}/worktrees/{{repo_id}}"
default_surface = "web"

[defaults.agents.default]
command = "codex --no-alt-screen -s workspace-write -a on-request"

[defaults.requirements]
binaries = ["bun"]
`
	if err := os.WriteFile(globalPath, []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agentflow"), 0o755); err != nil {
		t.Fatalf("mkdir repo config dir: %v", err)
	}
	repoConfig := `
[repo]
name = "agentflow"
base_branch = "main"
branch_prefix = "feature"
default_surface = "cli"
`
	if err := os.WriteFile(filepath.Join(repoRoot, ".agentflow", "config.toml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	manifest := `
[env]
targets = [{ path = ".env.agentflow" }]

[commands]
verify_quick = "go test ./..."

[requirements]
binaries = ["go"]
`
	if err := os.WriteFile(filepath.Join(repoRoot, ".agentflow", "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	runtime, err := resolveRuntimeConfig(repoRoot, "")
	if err != nil {
		t.Fatalf("resolveRuntimeConfig returned error: %v", err)
	}
	if runtime.GlobalConfigPath != globalPath {
		t.Fatalf("unexpected global config path: %q", runtime.GlobalConfigPath)
	}
	if !runtime.RepoConfigExists || !runtime.ManifestExists {
		t.Fatalf("expected repo config and manifest to exist: %+v", runtime)
	}
	if runtime.EffectiveConfig.Repo.BaseBranch != "main" {
		t.Fatalf("expected repo config to override base branch, got %q", runtime.EffectiveConfig.Repo.BaseBranch)
	}
	if runtime.EffectiveConfig.Repo.BranchPrefix != "feature" {
		t.Fatalf("expected repo branch prefix, got %q", runtime.EffectiveConfig.Repo.BranchPrefix)
	}
	if runtime.EffectiveConfig.Repo.DefaultSurface != "cli" {
		t.Fatalf("expected repo config to override default surface, got %q", runtime.EffectiveConfig.Repo.DefaultSurface)
	}
	expectedWorktreeRoot := "{{agentflow_state_home}}/worktrees/{{repo_id}}"
	if runtime.EffectiveConfig.Repo.WorktreeRoot != expectedWorktreeRoot {
		t.Fatalf("expected global config to set worktree root, got %q", runtime.EffectiveConfig.Repo.WorktreeRoot)
	}
	if runtime.EffectiveConfig.Commands["verify_quick"] != "go test ./..." {
		t.Fatalf("expected manifest command, got %q", runtime.EffectiveConfig.Commands["verify_quick"])
	}
	if !contains(runtime.EffectiveConfig.Requirements.Binaries, "bun") || !contains(runtime.EffectiveConfig.Requirements.Binaries, "go") {
		t.Fatalf("expected merged requirements, got %v", runtime.EffectiveConfig.Requirements.Binaries)
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

func TestResolvedGlobalConfigPathUsesOverride(t *testing.T) {
	t.Parallel()

	path, err := ResolvedGlobalConfigPath("../agentflow.toml")
	if err != nil {
		t.Fatalf("ResolvedGlobalConfigPath returned error: %v", err)
	}
	if path != filepath.Clean("../agentflow.toml") {
		t.Fatalf("unexpected path: %q", path)
	}
}

func TestSampleConfigsParse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	globalPath := filepath.Join(dir, "global.toml")
	if err := os.WriteFile(globalPath, []byte(SampleGlobalConfig()), 0o644); err != nil {
		t.Fatalf("write global sample: %v", err)
	}
	globalCfg, _, globalExists, err := loadTOMLConfig[GlobalConfig](globalPath)
	if err != nil {
		t.Fatalf("load global sample: %v", err)
	}
	if !globalExists || globalCfg.Defaults.Repo.WorktreeRoot != defaultWorktreeRootTemplate {
		t.Fatalf("unexpected global sample: %+v", globalCfg)
	}

	repoPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(repoPath, []byte(SampleRepoConfig("/tmp/agentflow")), 0o644); err != nil {
		t.Fatalf("write repo sample: %v", err)
	}
	repoCfg, _, repoExists, err := loadTOMLConfig[RepoConfigFile](repoPath)
	if err != nil {
		t.Fatalf("load repo sample: %v", err)
	}
	if !repoExists || repoCfg.Repo.Name == "" {
		t.Fatalf("unexpected repo sample: %+v", repoCfg)
	}

	manifestPath := filepath.Join(dir, "manifest.toml")
	if err := os.WriteFile(manifestPath, []byte(SampleManifest()), 0o644); err != nil {
		t.Fatalf("write manifest sample: %v", err)
	}
	manifestCfg, _, manifestExists, err := loadTOMLConfig[ManifestFile](manifestPath)
	if err != nil {
		t.Fatalf("load manifest sample: %v", err)
	}
	if !manifestExists || len(manifestCfg.Env.Targets) != 1 {
		t.Fatalf("unexpected manifest sample: %+v", manifestCfg)
	}
}

func TestWriteConfigHelpersWriteExpectedPaths(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("AGENTFLOW_CONFIG_HOME", configHome)

	globalPath, err := WriteGlobalConfig("", false)
	if err != nil {
		t.Fatalf("WriteGlobalConfig returned error: %v", err)
	}
	if globalPath != filepath.Join(configHome, "config.toml") {
		t.Fatalf("unexpected global path: %q", globalPath)
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	repoPath, err := WriteRepoConfig(repoRoot, false)
	if err != nil {
		t.Fatalf("WriteRepoConfig returned error: %v", err)
	}
	if repoPath != filepath.Join(repoRoot, ".agentflow", "config.toml") {
		t.Fatalf("unexpected repo config path: %q", repoPath)
	}

	manifestPath, err := WriteManifest(repoRoot, false)
	if err != nil {
		t.Fatalf("WriteManifest returned error: %v", err)
	}
	if manifestPath != filepath.Join(repoRoot, ".agentflow", "manifest.toml") {
		t.Fatalf("unexpected manifest path: %q", manifestPath)
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
