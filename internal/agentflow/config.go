package agentflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const defaultWorktreeRootTemplate = "{{agentflow_state_home}}/worktrees/{{repo_id}}"

func defaultEffectiveConfig() EffectiveConfig {
	return EffectiveConfig{
		Repo: RepoConfig{
			BaseBranch:     "origin/main",
			WorktreeRoot:   defaultWorktreeRootTemplate,
			DefaultSurface: "default",
		},
		Env: EnvConfig{
			Targets: []EnvTargetConfig{{Path: ".env.agentflow"}},
		},
		Ports:    PortsConfig{},
		Commands: map[string]string{},
		Agents: map[string]AgentConfig{
			"default": {
				Runner:       "codex",
				Command:      "codex --no-alt-screen -s workspace-write -a on-request",
				PrimePrompt:  "Read AGENTS.md and any relevant repo instructions before acting.",
				ResumePrompt: "Resume the current task and re-check AGENTS.md if the repo changed.",
			},
		},
		Tmux: TmuxConfig{
			SessionName: "{{repo}}-{{task}}-{{id}}",
			Windows: []TmuxWindowConfig{
				{Name: "editor", Command: "nvim ."},
				{Name: "verify", Command: "clear"},
				{Name: "codex", Agent: "default"},
			},
		},
		Requirements: RequirementsConfig{
			Binaries: []string{"git", "tmux", "codex", "nvim"},
		},
	}
}

func applyGlobalConfig(base EffectiveConfig, cfg GlobalConfig) EffectiveConfig {
	out := base
	if cfg.Defaults.Repo.BaseBranch != "" {
		out.Repo.BaseBranch = cfg.Defaults.Repo.BaseBranch
	}
	if cfg.Defaults.Repo.WorktreeRoot != "" {
		out.Repo.WorktreeRoot = cfg.Defaults.Repo.WorktreeRoot
	}
	if cfg.Defaults.Repo.DefaultSurface != "" {
		out.Repo.DefaultSurface = cfg.Defaults.Repo.DefaultSurface
	}
	if len(cfg.Defaults.Agents) > 0 {
		out.Agents = mergeAgentConfigs(out.Agents, cfg.Defaults.Agents)
	}
	if cfg.Defaults.Tmux.SessionName != "" {
		out.Tmux.SessionName = cfg.Defaults.Tmux.SessionName
	}
	if len(cfg.Defaults.Tmux.Windows) > 0 {
		out.Tmux.Windows = append([]TmuxWindowConfig(nil), cfg.Defaults.Tmux.Windows...)
	}
	if len(cfg.Defaults.Requirements.Binaries) > 0 {
		out.Requirements.Binaries = uniqueStrings(append(out.Requirements.Binaries, cfg.Defaults.Requirements.Binaries...))
	}
	if len(cfg.Defaults.Requirements.MCPServers) > 0 {
		out.Requirements.MCPServers = uniqueStrings(append(out.Requirements.MCPServers, cfg.Defaults.Requirements.MCPServers...))
	}
	return out
}

func applyRepoConfig(base EffectiveConfig, cfg RepoConfigFile) EffectiveConfig {
	out := base
	if cfg.Repo.Name != "" {
		out.Repo.Name = cfg.Repo.Name
	}
	if cfg.Repo.BaseBranch != "" {
		out.Repo.BaseBranch = cfg.Repo.BaseBranch
	}
	if cfg.Repo.BranchPrefix != "" {
		out.Repo.BranchPrefix = cfg.Repo.BranchPrefix
	}
	if cfg.Repo.DefaultSurface != "" {
		out.Repo.DefaultSurface = cfg.Repo.DefaultSurface
	}
	return out
}

func applyManifest(base EffectiveConfig, manifest ManifestFile) EffectiveConfig {
	out := base
	if len(manifest.Bootstrap.Commands) > 0 {
		out.Bootstrap.Commands = append([]string(nil), manifest.Bootstrap.Commands...)
	}
	if len(manifest.Bootstrap.EnvFiles) > 0 {
		out.Bootstrap.EnvFiles = append([]EnvFileMapping(nil), manifest.Bootstrap.EnvFiles...)
	}
	if len(manifest.Env.Targets) > 0 {
		out.Env.Targets = append([]EnvTargetConfig(nil), manifest.Env.Targets...)
	}
	if len(manifest.Ports.Bindings) > 0 {
		out.Ports.Bindings = append([]PortBindingConfig(nil), manifest.Ports.Bindings...)
	}
	if len(manifest.Commands) > 0 {
		if out.Commands == nil {
			out.Commands = map[string]string{}
		}
		for key, value := range manifest.Commands {
			out.Commands[key] = value
		}
	}
	if len(manifest.Agents) > 0 {
		out.Agents = mergeAgentConfigs(out.Agents, manifest.Agents)
	}
	if manifest.Tmux.SessionName != "" {
		out.Tmux.SessionName = manifest.Tmux.SessionName
	}
	if len(manifest.Tmux.Windows) > 0 {
		out.Tmux.Windows = append([]TmuxWindowConfig(nil), manifest.Tmux.Windows...)
	}
	if len(manifest.Requirements.Binaries) > 0 {
		out.Requirements.Binaries = uniqueStrings(append(out.Requirements.Binaries, manifest.Requirements.Binaries...))
	}
	if len(manifest.Requirements.MCPServers) > 0 {
		out.Requirements.MCPServers = uniqueStrings(append(out.Requirements.MCPServers, manifest.Requirements.MCPServers...))
	}
	return out
}

func mergeAgentConfigs(base, override map[string]AgentConfig) map[string]AgentConfig {
	out := map[string]AgentConfig{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		existing := out[key]
		if value.Runner != "" {
			existing.Runner = value.Runner
		}
		if value.Command != "" {
			existing.Command = value.Command
		}
		if value.PrimePrompt != "" {
			existing.PrimePrompt = value.PrimePrompt
		}
		if value.ResumePrompt != "" {
			existing.ResumePrompt = value.ResumePrompt
		}
		out[key] = existing
	}
	return out
}

func loadTOMLConfig[T any](path string) (T, string, bool, error) {
	var cfg T
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, "", false, nil
	}
	if err != nil {
		return cfg, "", false, fmt.Errorf("read config %s: %w", path, err)
	}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, "", false, fmt.Errorf("decode config %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return cfg, hex.EncodeToString(sum[:]), true, nil
}

func resolveRuntimeConfig(repoRoot string, configOverridePath string) (RuntimeConfig, error) {
	canonicalRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		canonicalRoot = repoRoot
	}

	stateRoot, err := stateRootPath()
	if err != nil {
		return RuntimeConfig{}, err
	}
	globalPath, err := ResolvedGlobalConfigPath(configOverridePath)
	if err != nil {
		return RuntimeConfig{}, err
	}
	repoConfigPath := ResolvedRepoConfigPath(canonicalRoot)
	manifestPath := ResolvedManifestPath(canonicalRoot)

	globalCfg, _, globalExists, err := loadTOMLConfig[GlobalConfig](globalPath)
	if err != nil {
		return RuntimeConfig{}, err
	}
	repoCfg, repoFingerprint, repoExists, err := loadTOMLConfig[RepoConfigFile](repoConfigPath)
	if err != nil {
		return RuntimeConfig{}, err
	}
	manifestCfg, manifestFingerprint, manifestExists, err := loadTOMLConfig[ManifestFile](manifestPath)
	if err != nil {
		return RuntimeConfig{}, err
	}

	effective := defaultEffectiveConfig()
	effective = applyGlobalConfig(effective, globalCfg)
	effective = applyRepoConfig(effective, repoCfg)
	effective = applyManifest(effective, manifestCfg)
	if effective.Repo.Name == "" {
		effective.Repo.Name = filepath.Base(canonicalRoot)
	}
	if strings.TrimSpace(effective.Repo.WorktreeRoot) == "" {
		effective.Repo.WorktreeRoot = defaultWorktreeRootTemplate
	}
	if err := validateEffectiveConfig(effective); err != nil {
		return RuntimeConfig{}, err
	}

	return RuntimeConfig{
		RepoRoot:              canonicalRoot,
		RepoID:                repoID(canonicalRoot),
		RepoConfigPath:        repoConfigPath,
		RepoConfigExists:      repoExists,
		RepoConfigFingerprint: repoFingerprint,
		ManifestPath:          manifestPath,
		ManifestExists:        manifestExists,
		ManifestFingerprint:   manifestFingerprint,
		GlobalConfigPath:      globalPath,
		GlobalConfigExists:    globalExists,
		StateRoot:             stateRoot,
		GlobalConfig:          globalCfg,
		RepoConfig:            repoCfg,
		Manifest:              manifestCfg,
		EffectiveConfig:       effective,
	}, nil
}

func validateEffectiveConfig(cfg EffectiveConfig) error {
	if cfg.Repo.BaseBranch == "" {
		return errors.New("repo.base_branch must not be empty")
	}
	managedFiles, err := effectiveManagedEnvFiles(cfg)
	if err != nil {
		return err
	}
	if len(managedFiles) == 0 {
		return errors.New("env.targets must not be empty")
	}
	portBindings, err := effectivePortBindings(cfg)
	if err != nil {
		return err
	}
	seenTargets := map[string]struct{}{}
	for _, path := range managedFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			return errors.New("managed env target path must not be empty")
		}
		if _, exists := seenTargets[path]; exists {
			return fmt.Errorf("managed env target %q is declared more than once", path)
		}
		seenTargets[path] = struct{}{}
	}
	for _, binding := range portBindings {
		if strings.TrimSpace(binding.Target) == "" {
			return errors.New("port binding target must not be empty")
		}
		if _, ok := seenTargets[binding.Target]; !ok {
			return fmt.Errorf("port binding target %q must also be declared in env.targets", binding.Target)
		}
		if strings.TrimSpace(binding.Key) == "" {
			return fmt.Errorf("port binding for target %q must declare key", binding.Target)
		}
		if binding.Start == 0 || binding.End == 0 || binding.End < binding.Start {
			return fmt.Errorf("port binding %q must describe a valid range", binding.Key)
		}
	}
	if len(cfg.Tmux.Windows) == 0 {
		return errors.New("tmux.windows must not be empty")
	}
	primaryAgents := 0
	for _, window := range cfg.Tmux.Windows {
		if window.Name == "" {
			return errors.New("tmux window name must not be empty")
		}
		if window.Command == "" && window.Agent == "" {
			return fmt.Errorf("tmux window %q must declare command or agent", window.Name)
		}
		if window.Command != "" && window.Agent != "" {
			return fmt.Errorf("tmux window %q must not declare both command and agent", window.Name)
		}
		if window.Agent != "" {
			primaryAgents++
			if _, ok := cfg.Agents[window.Agent]; !ok {
				return fmt.Errorf("tmux window %q references unknown agent %q", window.Name, window.Agent)
			}
		}
	}
	if primaryAgents > 1 {
		return errors.New("v1 supports at most one tmux agent window")
	}
	return nil
}

func effectiveManagedEnvFiles(cfg EffectiveConfig) ([]string, error) {
	paths := make([]string, 0, len(cfg.Env.Targets))
	for _, target := range cfg.Env.Targets {
		path := strings.TrimSpace(target.Path)
		if path == "" {
			return nil, errors.New("env target path must not be empty")
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil, errors.New("env.targets must not be empty")
	}
	return uniqueStrings(paths), nil
}

func effectivePortBindings(cfg EffectiveConfig) ([]PortBindingConfig, error) {
	return append([]PortBindingConfig(nil), cfg.Ports.Bindings...), nil
}

func manifestExecutableEntries(cfg ManifestFile) []string {
	entries := make([]string, 0)
	for _, command := range cfg.Bootstrap.Commands {
		entries = append(entries, command)
	}
	for _, command := range cfg.Commands {
		entries = append(entries, command)
	}
	for _, agent := range cfg.Agents {
		if agent.Command != "" {
			entries = append(entries, agent.Command)
		}
	}
	for _, window := range cfg.Tmux.Windows {
		if window.Command != "" {
			entries = append(entries, window.Command)
		}
	}
	return uniqueStrings(entries)
}

func ResolvedGlobalConfigPath(configOverride string) (string, error) {
	if strings.TrimSpace(configOverride) != "" {
		return filepath.Clean(configOverride), nil
	}
	return globalConfigPath()
}

func ResolvedRepoConfigPath(repoRoot string) string {
	return filepath.Join(filepath.Clean(repoRoot), ".agentflow", "config.toml")
}

func ResolvedManifestPath(repoRoot string) string {
	return filepath.Join(filepath.Clean(repoRoot), ".agentflow", "manifest.toml")
}

func ReadConfigFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func RenderEffectiveConfig(cfg EffectiveConfig, format string) (string, error) {
	switch format {
	case "", "toml":
		var buf bytes.Buffer
		encoder := toml.NewEncoder(&buf)
		if err := encoder.Encode(cfg); err != nil {
			return "", err
		}
		return buf.String(), nil
	case "json":
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return "", err
		}
		return string(append(data, '\n')), nil
	default:
		return "", fmt.Errorf("unsupported format %q", format)
	}
}

func SampleGlobalConfig() string {
	return strings.TrimSpace(`
# Personal defaults for agentflow on this machine.

[defaults.repo]
base_branch = "origin/main"
worktree_root = "{{agentflow_state_home}}/worktrees/{{repo_id}}"
default_surface = "default"

[defaults.agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and any relevant repo instructions before acting."
resume_prompt = "Resume the current task and re-check AGENTS.md if the repo changed."

[defaults.tmux]
session_name = "{{repo}}-{{task}}-{{id}}"

[[defaults.tmux.windows]]
name = "editor"
command = "nvim ."

[[defaults.tmux.windows]]
name = "verify"
command = "clear"

[[defaults.tmux.windows]]
name = "codex"
agent = "default"

[defaults.requirements]
binaries = ["git", "tmux", "codex", "nvim"]
`) + "\n"
}

func SampleRepoConfig(repoRoot string) string {
	repoName := slugify(filepath.Base(repoRoot))
	if repoName == "" {
		repoName = "repo"
	}
	return fmt.Sprintf(strings.TrimSpace(`
# Checked-in repo conventions for agentflow.

[repo]
name = %q
base_branch = "origin/main"
default_surface = "default"
`)+"\n", repoName)
}

func SampleManifest() string {
	return strings.TrimSpace(`
# Checked-in executable workflow policy for agentflow.

[env]
targets = [{ path = ".env.agentflow" }]

[commands]
review = "make review"
verify_quick = "make test"

[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and any relevant repo instructions before acting."
resume_prompt = "Resume the task and re-check local instructions if the repo changed."

[tmux]
session_name = "{{repo}}-{{task}}-{{id}}"

[[tmux.windows]]
name = "editor"
command = "nvim ."

[[tmux.windows]]
name = "verify"
command = "clear"

[[tmux.windows]]
name = "codex"
agent = "default"

[requirements]
binaries = ["git", "tmux", "codex", "nvim"]
`) + "\n"
}

func writeConfigFile(path string, contents string, force bool) (string, error) {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("config already exists at %s", path)
		}
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func WriteGlobalConfig(configOverride string, force bool) (string, error) {
	path, err := ResolvedGlobalConfigPath(configOverride)
	if err != nil {
		return "", err
	}
	return writeConfigFile(path, SampleGlobalConfig(), force)
}

func WriteRepoConfig(repoRoot string, force bool) (string, error) {
	return writeConfigFile(ResolvedRepoConfigPath(repoRoot), SampleRepoConfig(repoRoot), force)
}

func WriteManifest(repoRoot string, force bool) (string, error) {
	return writeConfigFile(ResolvedManifestPath(repoRoot), SampleManifest(), force)
}

func InitGlobalConfig(configOverride string, force bool) (string, error) {
	return WriteGlobalConfig(configOverride, force)
}
