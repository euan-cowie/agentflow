package agentflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const defaultWorktreeRootTemplate = "{{agentflow_state_home}}/worktrees/{{repo_id}}"

func defaultWorkflowConfig() WorkflowConfig {
	return WorkflowConfig{
		Repo: RepoConfig{
			BaseBranch:     "origin/main",
			WorktreeRoot:   defaultWorktreeRootTemplate,
			DefaultSurface: "default",
		},
		Env: EnvConfig{
			ManagedFile: ".env.agentflow",
		},
		Ports: PortsConfig{
			Key:   "AGENTFLOW_PORT",
			Start: 4101,
			End:   4199,
		},
		Commands: map[string]string{},
		Agents: map[string]AgentConfig{
			"default": {
				Runner:       "codex",
				Command:      "codex --no-alt-screen -s workspace-write -a on-request",
				PrimePrompt:  "Read AGENTS.md and any relevant .agents content before acting.",
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

func mergeWorkflowConfig(base, override WorkflowConfig) WorkflowConfig {
	out := base

	if override.Repo.Name != "" {
		out.Repo.Name = override.Repo.Name
	}
	if override.Repo.BaseBranch != "" {
		out.Repo.BaseBranch = override.Repo.BaseBranch
	}
	if override.Repo.WorktreeRoot != "" {
		out.Repo.WorktreeRoot = override.Repo.WorktreeRoot
	}
	if override.Repo.BranchPrefix != "" {
		out.Repo.BranchPrefix = override.Repo.BranchPrefix
	}
	if override.Repo.DefaultSurface != "" {
		out.Repo.DefaultSurface = override.Repo.DefaultSurface
	}

	if len(override.Bootstrap.Commands) > 0 {
		out.Bootstrap.Commands = append([]string(nil), override.Bootstrap.Commands...)
	}
	if len(override.Bootstrap.EnvFiles) > 0 {
		out.Bootstrap.EnvFiles = append([]EnvFileMapping(nil), override.Bootstrap.EnvFiles...)
	}

	if override.Env.ManagedFile != "" {
		out.Env.ManagedFile = override.Env.ManagedFile
	}
	if len(override.Env.Targets) > 0 {
		out.Env.Targets = append([]EnvTargetConfig(nil), override.Env.Targets...)
	}

	if override.Ports.Enabled {
		out.Ports.Enabled = true
	}
	if override.Ports.File != "" {
		out.Ports.File = override.Ports.File
	}
	if override.Ports.Key != "" {
		out.Ports.Key = override.Ports.Key
	}
	if override.Ports.Start != 0 {
		out.Ports.Start = override.Ports.Start
	}
	if override.Ports.End != 0 {
		out.Ports.End = override.Ports.End
	}
	if len(override.Ports.Bindings) > 0 {
		out.Ports.Bindings = append([]PortBindingConfig(nil), override.Ports.Bindings...)
	}

	if len(override.Commands) > 0 {
		if out.Commands == nil {
			out.Commands = map[string]string{}
		}
		for key, value := range override.Commands {
			out.Commands[key] = value
		}
	}

	if len(override.Agents) > 0 {
		if out.Agents == nil {
			out.Agents = map[string]AgentConfig{}
		}
		for key, value := range override.Agents {
			baseAgent := out.Agents[key]
			if value.Runner != "" {
				baseAgent.Runner = value.Runner
			}
			if value.Command != "" {
				baseAgent.Command = value.Command
			}
			if value.PrimePrompt != "" {
				baseAgent.PrimePrompt = value.PrimePrompt
			}
			if value.ResumePrompt != "" {
				baseAgent.ResumePrompt = value.ResumePrompt
			}
			out.Agents[key] = baseAgent
		}
	}

	if override.Tmux.SessionName != "" {
		out.Tmux.SessionName = override.Tmux.SessionName
	}
	if len(override.Tmux.Windows) > 0 {
		out.Tmux.Windows = append([]TmuxWindowConfig(nil), override.Tmux.Windows...)
	}

	if len(override.Requirements.Binaries) > 0 {
		out.Requirements.Binaries = uniqueStrings(append(out.Requirements.Binaries, override.Requirements.Binaries...))
	}
	if len(override.Requirements.MCPServers) > 0 {
		out.Requirements.MCPServers = uniqueStrings(append(out.Requirements.MCPServers, override.Requirements.MCPServers...))
	}

	return out
}

func loadWorkflowConfig(path string) (WorkflowConfig, string, bool, error) {
	var cfg WorkflowConfig
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
	cfgPath := configOverridePath
	if cfgPath == "" {
		cfgPath, err = globalConfigPath()
		if err != nil {
			return RuntimeConfig{}, err
		}
	}

	builtins := defaultWorkflowConfig()
	globalCfg, _, _, err := loadWorkflowConfig(cfgPath)
	if err != nil {
		return RuntimeConfig{}, err
	}

	manifestPath := filepath.Join(canonicalRoot, ".agents", "workflow.toml")
	manifestCfg, manifestFingerprint, manifestExists, err := loadWorkflowConfig(manifestPath)
	if err != nil {
		return RuntimeConfig{}, err
	}

	merged := mergeWorkflowConfig(builtins, globalCfg)
	merged = mergeWorkflowConfig(merged, manifestCfg)

	if merged.Repo.Name == "" {
		merged.Repo.Name = filepath.Base(canonicalRoot)
	}
	if merged.Ports.File == "" && len(merged.Env.Targets) == 0 {
		merged.Ports.File = merged.Env.ManagedFile
	}
	if strings.TrimSpace(merged.Repo.WorktreeRoot) == "" {
		merged.Repo.WorktreeRoot = defaultWorktreeRootTemplate
	}

	runtime := RuntimeConfig{
		RepoRoot:            canonicalRoot,
		RepoID:              repoID(canonicalRoot),
		ManifestPath:        manifestPath,
		ManifestExists:      manifestExists,
		ManifestFingerprint: manifestFingerprint,
		GlobalConfigPath:    cfgPath,
		StateRoot:           stateRoot,
		Config:              merged,
		ManifestConfig:      manifestCfg,
	}
	if err := validateWorkflowConfig(runtime.Config); err != nil {
		return RuntimeConfig{}, err
	}
	return runtime, nil
}

func validateWorkflowConfig(cfg WorkflowConfig) error {
	if cfg.Repo.BaseBranch == "" {
		return errors.New("repo.base_branch must not be empty")
	}
	if cfg.Env.ManagedFile == "" {
		return errors.New("env.managed_file must not be empty")
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
	seenBindingTargets := map[string]struct{}{}
	for _, binding := range portBindings {
		if strings.TrimSpace(binding.Target) == "" {
			return errors.New("port binding target must not be empty")
		}
		if strings.TrimSpace(binding.Key) == "" {
			return fmt.Errorf("port binding for target %q must declare key", binding.Target)
		}
		if binding.Start == 0 || binding.End == 0 || binding.End < binding.Start {
			return fmt.Errorf("port binding %q must describe a valid range", binding.Key)
		}
		seenBindingTargets[binding.Target] = struct{}{}
	}
	for target := range seenBindingTargets {
		if _, ok := seenTargets[target]; !ok {
			return fmt.Errorf("port binding target %q must also be declared in env.targets or env.managed_file", target)
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

func effectiveManagedEnvFiles(cfg WorkflowConfig) ([]string, error) {
	paths := make([]string, 0)
	for _, target := range cfg.Env.Targets {
		path := strings.TrimSpace(target.Path)
		if path == "" {
			return nil, errors.New("env target path must not be empty")
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		path := strings.TrimSpace(cfg.Env.ManagedFile)
		if path == "" {
			return nil, errors.New("env.managed_file must not be empty")
		}
		paths = append(paths, path)
	}
	for _, binding := range cfg.Ports.Bindings {
		target := strings.TrimSpace(binding.Target)
		if target != "" {
			paths = append(paths, target)
		}
	}
	return uniqueStrings(paths), nil
}

func effectivePortBindings(cfg WorkflowConfig) ([]PortBindingConfig, error) {
	if len(cfg.Ports.Bindings) > 0 {
		return append([]PortBindingConfig(nil), cfg.Ports.Bindings...), nil
	}
	if !cfg.Ports.Enabled {
		return nil, nil
	}
	target := strings.TrimSpace(cfg.Ports.File)
	if target == "" && len(cfg.Env.Targets) > 0 {
		target = strings.TrimSpace(cfg.Env.Targets[0].Path)
	}
	if target == "" {
		target = strings.TrimSpace(cfg.Env.ManagedFile)
	}
	if target == "" {
		return nil, errors.New("ports.file or env.managed_file must be set when legacy ports are enabled")
	}
	return []PortBindingConfig{{
		Target: target,
		Key:    cfg.Ports.Key,
		Start:  cfg.Ports.Start,
		End:    cfg.Ports.End,
	}}, nil
}

func manifestExecutableEntries(cfg WorkflowConfig) []string {
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
