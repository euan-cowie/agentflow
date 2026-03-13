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

func applyConfigFile(base EffectiveConfig, cfg ConfigFile) EffectiveConfig {
	out := base
	if cfg.Repo.Name != "" {
		out.Repo.Name = cfg.Repo.Name
	}
	if cfg.Repo.BaseBranch != "" {
		out.Repo.BaseBranch = cfg.Repo.BaseBranch
	}
	if cfg.Repo.WorktreeRoot != "" {
		out.Repo.WorktreeRoot = cfg.Repo.WorktreeRoot
	}
	if cfg.Repo.BranchPrefix != "" {
		out.Repo.BranchPrefix = cfg.Repo.BranchPrefix
	}
	if cfg.Repo.DefaultSurface != "" {
		out.Repo.DefaultSurface = cfg.Repo.DefaultSurface
	}
	if len(cfg.Bootstrap.Commands) > 0 {
		out.Bootstrap.Commands = append([]string(nil), cfg.Bootstrap.Commands...)
	}
	if len(cfg.Bootstrap.EnvFiles) > 0 {
		out.Bootstrap.EnvFiles = append([]EnvFileMapping(nil), cfg.Bootstrap.EnvFiles...)
	}
	if len(cfg.Env.Targets) > 0 {
		out.Env.Targets = append([]EnvTargetConfig(nil), cfg.Env.Targets...)
	}
	if len(cfg.Ports.Bindings) > 0 {
		out.Ports.Bindings = append([]PortBindingConfig(nil), cfg.Ports.Bindings...)
	}
	if len(cfg.Commands) > 0 {
		if out.Commands == nil {
			out.Commands = map[string]string{}
		}
		for key, value := range cfg.Commands {
			out.Commands[key] = value
		}
	}
	if len(cfg.Agents) > 0 {
		out.Agents = mergeAgentConfigs(out.Agents, cfg.Agents)
	}
	if cfg.Tmux.SessionName != "" {
		out.Tmux.SessionName = cfg.Tmux.SessionName
	}
	if len(cfg.Tmux.Windows) > 0 {
		out.Tmux.Windows = append([]TmuxWindowConfig(nil), cfg.Tmux.Windows...)
	}
	if len(cfg.Requirements.Binaries) > 0 {
		out.Requirements.Binaries = uniqueStrings(append(out.Requirements.Binaries, cfg.Requirements.Binaries...))
	}
	if len(cfg.Requirements.MCPServers) > 0 {
		out.Requirements.MCPServers = uniqueStrings(append(out.Requirements.MCPServers, cfg.Requirements.MCPServers...))
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

func resolveRuntimeConfig(repoRoot string) (RuntimeConfig, error) {
	canonicalRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		canonicalRoot = repoRoot
	}

	stateRoot, err := stateRootPath()
	if err != nil {
		return RuntimeConfig{}, err
	}
	configPath := ResolvedConfigPath(canonicalRoot)
	legacyManifestPath := ResolvedLegacyManifestPath(canonicalRoot)
	if _, err := os.Stat(legacyManifestPath); err == nil {
		return RuntimeConfig{}, fmt.Errorf("legacy repo manifest found at %s; merge it into %s and remove the manifest file", legacyManifestPath, configPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RuntimeConfig{}, err
	}

	cfg, _, exists, err := loadTOMLConfig[ConfigFile](configPath)
	if err != nil {
		return RuntimeConfig{}, err
	}

	effective := defaultEffectiveConfig()
	effective = applyConfigFile(effective, cfg)
	if effective.Repo.Name == "" {
		effective.Repo.Name = filepath.Base(canonicalRoot)
	}
	if strings.TrimSpace(effective.Repo.WorktreeRoot) == "" {
		effective.Repo.WorktreeRoot = defaultWorktreeRootTemplate
	}
	if err := validateEffectiveConfig(effective); err != nil {
		return RuntimeConfig{}, err
	}

	fingerprint, err := workflowFingerprint(cfg)
	if err != nil {
		return RuntimeConfig{}, err
	}

	return RuntimeConfig{
		RepoRoot:            canonicalRoot,
		RepoID:              repoID(canonicalRoot),
		ConfigPath:          configPath,
		ConfigExists:        exists,
		WorkflowFingerprint: fingerprint,
		StateRoot:           stateRoot,
		Config:              cfg,
		EffectiveConfig:     effective,
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

type workflowConfig struct {
	Bootstrap BootstrapConfig        `json:"bootstrap"`
	Env       EnvConfig              `json:"env"`
	Ports     PortsConfig            `json:"ports"`
	Commands  map[string]string      `json:"commands"`
	Agents    map[string]AgentConfig `json:"agents"`
	Tmux      TmuxConfig             `json:"tmux"`
}

func workflowFingerprint(cfg ConfigFile) (string, error) {
	if !hasWorkflowConfig(cfg) {
		return "", nil
	}
	data, err := json.Marshal(workflowConfig{
		Bootstrap: cfg.Bootstrap,
		Env:       cfg.Env,
		Ports:     cfg.Ports,
		Commands:  cfg.Commands,
		Agents:    cfg.Agents,
		Tmux:      cfg.Tmux,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func hasWorkflowConfig(cfg ConfigFile) bool {
	return len(cfg.Bootstrap.Commands) > 0 ||
		len(cfg.Bootstrap.EnvFiles) > 0 ||
		len(cfg.Env.Targets) > 0 ||
		len(cfg.Ports.Bindings) > 0 ||
		len(cfg.Commands) > 0 ||
		len(cfg.Agents) > 0 ||
		cfg.Tmux.SessionName != "" ||
		len(cfg.Tmux.Windows) > 0
}

func workflowTrustEntries(cfg ConfigFile) []string {
	entries := make([]string, 0)
	for _, command := range cfg.Bootstrap.Commands {
		if strings.TrimSpace(command) != "" {
			entries = append(entries, "bootstrap command: "+command)
		}
	}
	for _, mapping := range cfg.Bootstrap.EnvFiles {
		if mapping.From != "" && mapping.To != "" {
			entries = append(entries, fmt.Sprintf("bootstrap env file: %s -> %s", mapping.From, mapping.To))
		}
	}
	for _, target := range cfg.Env.Targets {
		if strings.TrimSpace(target.Path) != "" {
			entries = append(entries, "managed env target: "+target.Path)
		}
	}
	for _, binding := range cfg.Ports.Bindings {
		if binding.Target != "" && binding.Key != "" {
			entries = append(entries, fmt.Sprintf("port binding: %s -> %s [%d-%d]", binding.Key, binding.Target, binding.Start, binding.End))
		}
	}
	for name, command := range cfg.Commands {
		if strings.TrimSpace(command) != "" {
			entries = append(entries, fmt.Sprintf("command %s: %s", name, command))
		}
	}
	for name, agent := range cfg.Agents {
		if strings.TrimSpace(agent.Command) != "" {
			entries = append(entries, fmt.Sprintf("agent %s: %s", name, agent.Command))
		}
	}
	if strings.TrimSpace(cfg.Tmux.SessionName) != "" {
		entries = append(entries, "tmux session: "+cfg.Tmux.SessionName)
	}
	for _, window := range cfg.Tmux.Windows {
		switch {
		case strings.TrimSpace(window.Command) != "":
			entries = append(entries, fmt.Sprintf("tmux window %s: %s", window.Name, window.Command))
		case strings.TrimSpace(window.Agent) != "":
			entries = append(entries, fmt.Sprintf("tmux window %s: agent %s", window.Name, window.Agent))
		}
	}
	return uniqueStrings(entries)
}

func ResolvedConfigPath(repoRoot string) string {
	return filepath.Join(filepath.Clean(repoRoot), ".agentflow", "config.toml")
}

func ResolvedLegacyManifestPath(repoRoot string) string {
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

type renderConfig struct {
	Repo         renderRepoConfig             `toml:"repo" json:"repo"`
	Bootstrap    *renderBootstrapConfig       `toml:"bootstrap,omitempty" json:"bootstrap,omitempty"`
	Env          *EnvConfig                   `toml:"env,omitempty" json:"env,omitempty"`
	Ports        *renderPortsConfig           `toml:"ports,omitempty" json:"ports,omitempty"`
	Commands     map[string]string            `toml:"commands,omitempty" json:"commands,omitempty"`
	Agents       map[string]renderAgentConfig `toml:"agents,omitempty" json:"agents,omitempty"`
	Tmux         *renderTmuxConfig            `toml:"tmux,omitempty" json:"tmux,omitempty"`
	Requirements *renderRequirementsConfig    `toml:"requirements,omitempty" json:"requirements,omitempty"`
}

type renderRepoConfig struct {
	Name           string `toml:"name" json:"name"`
	BaseBranch     string `toml:"base_branch" json:"base_branch"`
	WorktreeRoot   string `toml:"worktree_root" json:"worktree_root"`
	BranchPrefix   string `toml:"branch_prefix,omitempty" json:"branch_prefix,omitempty"`
	DefaultSurface string `toml:"default_surface" json:"default_surface"`
}

type renderAgentConfig struct {
	Runner       string `toml:"runner,omitempty" json:"runner,omitempty"`
	Command      string `toml:"command,omitempty" json:"command,omitempty"`
	PrimePrompt  string `toml:"prime_prompt,omitempty" json:"prime_prompt,omitempty"`
	ResumePrompt string `toml:"resume_prompt,omitempty" json:"resume_prompt,omitempty"`
}

type renderTmuxConfig struct {
	SessionName string                   `toml:"session_name,omitempty" json:"session_name,omitempty"`
	Windows     []renderTmuxWindowConfig `toml:"windows,omitempty" json:"windows,omitempty"`
}

type renderTmuxWindowConfig struct {
	Name    string `toml:"name" json:"name"`
	Command string `toml:"command,omitempty" json:"command,omitempty"`
	Agent   string `toml:"agent,omitempty" json:"agent,omitempty"`
}

type renderBootstrapConfig struct {
	Commands []string         `toml:"commands,omitempty" json:"commands,omitempty"`
	EnvFiles []EnvFileMapping `toml:"env_files,omitempty" json:"env_files,omitempty"`
}

type renderPortsConfig struct {
	Bindings []PortBindingConfig `toml:"bindings,omitempty" json:"bindings,omitempty"`
}

type renderRequirementsConfig struct {
	Binaries   []string `toml:"binaries,omitempty" json:"binaries,omitempty"`
	MCPServers []string `toml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
}

func RenderEffectiveConfig(cfg EffectiveConfig, format string) (string, error) {
	rendered := buildRenderableEffectiveConfig(cfg)
	switch format {
	case "", "toml":
		var buf bytes.Buffer
		encoder := toml.NewEncoder(&buf)
		if err := encoder.Encode(rendered); err != nil {
			return "", err
		}
		return buf.String(), nil
	case "json":
		data, err := json.MarshalIndent(rendered, "", "  ")
		if err != nil {
			return "", err
		}
		return string(append(data, '\n')), nil
	default:
		return "", fmt.Errorf("unsupported format %q", format)
	}
}

func buildRenderableEffectiveConfig(cfg EffectiveConfig) renderConfig {
	rendered := renderConfig{
		Repo: renderRepoConfig{
			Name:           cfg.Repo.Name,
			BaseBranch:     cfg.Repo.BaseBranch,
			WorktreeRoot:   cfg.Repo.WorktreeRoot,
			BranchPrefix:   cfg.Repo.BranchPrefix,
			DefaultSurface: cfg.Repo.DefaultSurface,
		},
	}
	if len(cfg.Bootstrap.Commands) > 0 || len(cfg.Bootstrap.EnvFiles) > 0 {
		bootstrap := renderBootstrapConfig{
			Commands: cfg.Bootstrap.Commands,
			EnvFiles: cfg.Bootstrap.EnvFiles,
		}
		rendered.Bootstrap = &bootstrap
	}
	if len(cfg.Env.Targets) > 0 {
		env := cfg.Env
		rendered.Env = &env
	}
	if len(cfg.Ports.Bindings) > 0 {
		ports := renderPortsConfig{
			Bindings: cfg.Ports.Bindings,
		}
		rendered.Ports = &ports
	}
	if len(cfg.Commands) > 0 {
		rendered.Commands = cfg.Commands
	}
	if len(cfg.Agents) > 0 {
		rendered.Agents = make(map[string]renderAgentConfig, len(cfg.Agents))
		for name, agent := range cfg.Agents {
			rendered.Agents[name] = renderAgentConfig{
				Runner:       agent.Runner,
				Command:      agent.Command,
				PrimePrompt:  agent.PrimePrompt,
				ResumePrompt: agent.ResumePrompt,
			}
		}
	}
	if cfg.Tmux.SessionName != "" || len(cfg.Tmux.Windows) > 0 {
		tmux := renderTmuxConfig{
			SessionName: cfg.Tmux.SessionName,
			Windows:     make([]renderTmuxWindowConfig, 0, len(cfg.Tmux.Windows)),
		}
		for _, window := range cfg.Tmux.Windows {
			tmux.Windows = append(tmux.Windows, renderTmuxWindowConfig{
				Name:    window.Name,
				Command: window.Command,
				Agent:   window.Agent,
			})
		}
		rendered.Tmux = &tmux
	}
	if len(cfg.Requirements.Binaries) > 0 || len(cfg.Requirements.MCPServers) > 0 {
		requirements := renderRequirementsConfig{
			Binaries:   cfg.Requirements.Binaries,
			MCPServers: cfg.Requirements.MCPServers,
		}
		rendered.Requirements = &requirements
	}
	return rendered
}

func SampleConfig(repoRoot string) string {
	repoName := slugify(filepath.Base(repoRoot))
	if repoName == "" {
		repoName = "repo"
	}
	return fmt.Sprintf(strings.TrimSpace(`
# Checked-in repo workflow for agentflow.

[repo]
name = %q
base_branch = "origin/main"
default_surface = "default"

[env]
targets = [{ path = ".env.agentflow" }]

[commands]
review = "make review"
verify_quick = "make test"

[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and any relevant repo instructions before acting."
resume_prompt = "Resume the current task and re-check local instructions if the repo changed."

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
`)+"\n", repoName)
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

func WriteConfig(repoRoot string, force bool) (string, error) {
	return writeConfigFile(ResolvedConfigPath(repoRoot), SampleConfig(repoRoot), force)
}
