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
			WorktreeRoot: defaultWorktreeRootTemplate,
		},
		Ports:    PortsConfig{},
		Commands: map[string]string{},
		Agents:   map[string]AgentConfig{},
		Tmux: TmuxConfig{
			SessionName: "{{repo}}-{{task}}-{{id}}",
		},
		Delivery:     DeliveryConfig{},
		GitHub:       GitHubConfig{},
		Linear:       LinearConfig{},
		Requirements: RequirementsConfig{},
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
	if len(cfg.Env.SyncFiles) > 0 {
		out.Env.SyncFiles = append([]EnvFileMapping(nil), cfg.Env.SyncFiles...)
	}
	if len(cfg.Ports.Bindings) > 0 {
		out.Ports.Bindings = append([]PortBindingConfig(nil), cfg.Ports.Bindings...)
	}
	if cfg.Delivery.Remote != "" {
		out.Delivery.Remote = cfg.Delivery.Remote
	}
	if cfg.Delivery.SyncStrategy != "" {
		out.Delivery.SyncStrategy = cfg.Delivery.SyncStrategy
	}
	if len(cfg.Delivery.Preflight) > 0 {
		out.Delivery.Preflight = append([]string(nil), cfg.Delivery.Preflight...)
	}
	if cfg.Delivery.Cleanup != "" {
		out.Delivery.Cleanup = cfg.Delivery.Cleanup
	}
	if cfg.GitHub.Enabled {
		out.GitHub.Enabled = true
	}
	if cfg.GitHub.DraftOnSubmit {
		out.GitHub.DraftOnSubmit = true
	}
	if cfg.GitHub.MergeMethod != "" {
		out.GitHub.MergeMethod = cfg.GitHub.MergeMethod
	}
	if cfg.GitHub.AutoMerge {
		out.GitHub.AutoMerge = true
	}
	if cfg.GitHub.DeleteRemoteBranch {
		out.GitHub.DeleteRemoteBranch = true
	}
	if len(cfg.GitHub.Labels) > 0 {
		out.GitHub.Labels = append([]string(nil), cfg.GitHub.Labels...)
	}
	if len(cfg.GitHub.Reviewers) > 0 {
		out.GitHub.Reviewers = append([]string(nil), cfg.GitHub.Reviewers...)
	}
	if cfg.Linear.APIKeyEnv != "" {
		out.Linear.APIKeyEnv = cfg.Linear.APIKeyEnv
	}
	if cfg.Linear.CredentialProfile != "" {
		out.Linear.CredentialProfile = cfg.Linear.CredentialProfile
	}
	if cfg.Linear.IssueSort != "" {
		out.Linear.IssueSort = cfg.Linear.IssueSort
	}
	if len(cfg.Linear.TeamKeys) > 0 {
		out.Linear.TeamKeys = append([]string(nil), cfg.Linear.TeamKeys...)
	}
	if cfg.Linear.PickerScope != "" {
		out.Linear.PickerScope = cfg.Linear.PickerScope
	}
	if cfg.Linear.StartedState != "" {
		out.Linear.StartedState = cfg.Linear.StartedState
	}
	if cfg.Linear.CompletedState != "" {
		out.Linear.CompletedState = cfg.Linear.CompletedState
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
	generatedEnvFiles, err := effectiveGeneratedEnvFiles(cfg)
	if err != nil {
		return err
	}
	syncedEnvFiles, err := effectiveSyncedEnvFiles(cfg)
	if err != nil {
		return err
	}
	portBindings, err := effectivePortBindings(cfg)
	if err != nil {
		return err
	}
	seenTargets := map[string]struct{}{}
	for _, path := range generatedEnvFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			return errors.New("managed env target path must not be empty")
		}
		if err := validateWorktreeRelativePath(path, fmt.Sprintf("env target %q", path)); err != nil {
			return err
		}
		if _, exists := seenTargets[path]; exists {
			return fmt.Errorf("managed env target %q is declared more than once", path)
		}
		seenTargets[path] = struct{}{}
	}
	seenSyncedTargets := map[string]struct{}{}
	for _, mapping := range syncedEnvFiles {
		if err := validateRepoRelativePath(mapping.From, fmt.Sprintf("env sync source %q", mapping.From)); err != nil {
			return err
		}
		if err := validateWorktreeRelativePath(mapping.To, fmt.Sprintf("env sync target %q", mapping.To)); err != nil {
			return err
		}
		if _, exists := seenTargets[mapping.To]; exists {
			return fmt.Errorf("env sync target %q must not overlap env.targets", mapping.To)
		}
		if _, exists := seenSyncedTargets[mapping.To]; exists {
			return fmt.Errorf("env sync target %q is declared more than once", mapping.To)
		}
		seenSyncedTargets[mapping.To] = struct{}{}
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
	if cfg.Delivery.Remote != "" {
		if strings.TrimSpace(cfg.Delivery.Remote) == "" {
			return errors.New("delivery.remote must not be empty")
		}
	}
	if cfg.Delivery.SyncStrategy != "" {
		switch cfg.Delivery.SyncStrategy {
		case "rebase", "merge":
		default:
			return fmt.Errorf("delivery.sync_strategy must be one of rebase or merge")
		}
	}
	if cfg.Delivery.Cleanup != "" {
		switch cfg.Delivery.Cleanup {
		case "async", "manual":
		default:
			return fmt.Errorf("delivery.cleanup must be one of async or manual")
		}
	}
	for _, preflight := range cfg.Delivery.Preflight {
		if strings.TrimSpace(preflight) == "" {
			return errors.New("delivery.preflight entries must not be empty")
		}
	}
	primaryAgents := 0
	referencedAgents := map[string]struct{}{}
	for _, window := range cfg.Tmux.Windows {
		if window.Name == "" {
			return errors.New("tmux window name must not be empty")
		}
		if window.Cwd != "" {
			if err := validateTmuxWindowRelativePath(strings.TrimSpace(window.Cwd), fmt.Sprintf("tmux window %q cwd", window.Name)); err != nil {
				return err
			}
		}
		if window.Command == "" && window.Agent == "" {
			return fmt.Errorf("tmux window %q must declare command or agent", window.Name)
		}
		if window.Command != "" && window.Agent != "" {
			return fmt.Errorf("tmux window %q must not declare both command and agent", window.Name)
		}
		for _, envFile := range window.EnvFiles {
			if err := validateTmuxWindowRelativePath(strings.TrimSpace(envFile), fmt.Sprintf("tmux window %q env file", window.Name)); err != nil {
				return err
			}
		}
		if window.Agent != "" {
			primaryAgents++
			referencedAgents[window.Agent] = struct{}{}
			if _, ok := cfg.Agents[window.Agent]; !ok {
				return fmt.Errorf("tmux window %q references unknown agent %q", window.Name, window.Agent)
			}
		}
	}
	for name, agent := range cfg.Agents {
		_, referenced := referencedAgents[name]
		if agent.Runner != "" && agent.Runner != "codex" {
			return fmt.Errorf("agent %q declares unsupported runner %q", name, agent.Runner)
		}
		if referenced && strings.TrimSpace(agent.Command) == "" {
			return fmt.Errorf("agent %q must declare command", name)
		}
	}
	if primaryAgents > 1 {
		return errors.New("v1 supports at most one tmux agent window")
	}
	if cfg.GitHub.MergeMethod != "" {
		switch cfg.GitHub.MergeMethod {
		case "auto", "squash", "merge", "rebase":
		default:
			return fmt.Errorf("github.merge_method must be one of auto, squash, merge, or rebase")
		}
	}
	if linearConfigured(cfg.Linear) {
		if profile := effectiveLinearCredentialProfile(cfg.Linear); profile != "" {
			if err := validateCredentialProfileName(profile); err != nil {
				return fmt.Errorf("linear.credential_profile %w", err)
			}
		}
		switch sortMode := effectiveLinearIssueSort(cfg.Linear); sortMode {
		case "linear", "identifier", "updated", "state_then_updated":
		default:
			return fmt.Errorf("linear.issue_sort must be one of linear, identifier, updated, or state_then_updated")
		}
		switch scope := effectiveLinearPickerScope(cfg.Linear); scope {
		case "assigned", "team":
		default:
			return fmt.Errorf("linear.picker_scope must be one of assigned or team")
		}
		if effectiveLinearPickerScope(cfg.Linear) == "team" && len(cfg.Linear.TeamKeys) == 0 {
			return errors.New("linear.team_keys must be configured when linear.picker_scope = \"team\"")
		}
		for _, key := range cfg.Linear.TeamKeys {
			if strings.TrimSpace(key) == "" {
				return errors.New("linear.team_keys must not contain empty entries")
			}
		}
	}
	return nil
}

func linearConfigured(cfg LinearConfig) bool {
	return strings.TrimSpace(cfg.APIKeyEnv) != "" ||
		strings.TrimSpace(cfg.CredentialProfile) != "" ||
		len(cfg.TeamKeys) > 0 ||
		strings.TrimSpace(cfg.PickerScope) != "" ||
		strings.TrimSpace(cfg.StartedState) != "" ||
		strings.TrimSpace(cfg.CompletedState) != ""
}

func effectiveLinearAPIKeyEnv(cfg LinearConfig) string {
	if value := strings.TrimSpace(cfg.APIKeyEnv); value != "" {
		return value
	}
	return "LINEAR_API_KEY"
}

func effectiveLinearCredentialProfile(cfg LinearConfig) string {
	return strings.TrimSpace(cfg.CredentialProfile)
}

func effectiveLinearIssueSort(cfg LinearConfig) string {
	if value := strings.TrimSpace(cfg.IssueSort); value != "" {
		return strings.ToLower(value)
	}
	return "state_then_updated"
}

func effectiveLinearPickerScope(cfg LinearConfig) string {
	if value := strings.TrimSpace(cfg.PickerScope); value != "" {
		return value
	}
	return "assigned"
}

func effectiveGeneratedEnvFiles(cfg EffectiveConfig) ([]string, error) {
	if len(cfg.Env.Targets) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(cfg.Env.Targets))
	for _, target := range cfg.Env.Targets {
		path := strings.TrimSpace(target.Path)
		if path == "" {
			return nil, errors.New("env target path must not be empty")
		}
		paths = append(paths, normalizeRelativePath(path))
	}
	return uniqueStrings(paths), nil
}

func effectiveSyncedEnvFiles(cfg EffectiveConfig) ([]EnvFileMapping, error) {
	if len(cfg.Env.SyncFiles) == 0 {
		return nil, nil
	}
	mappings := make([]EnvFileMapping, 0, len(cfg.Env.SyncFiles))
	for _, mapping := range cfg.Env.SyncFiles {
		from := strings.TrimSpace(mapping.From)
		to := strings.TrimSpace(mapping.To)
		if from == "" || to == "" {
			return nil, errors.New("env sync files must include from and to")
		}
		mappings = append(mappings, EnvFileMapping{
			From: normalizeRelativePath(from),
			To:   normalizeRelativePath(to),
		})
	}
	return mappings, nil
}

func effectiveManagedEnvFiles(cfg EffectiveConfig) ([]string, error) {
	paths, err := effectiveGeneratedEnvFiles(cfg)
	if err != nil {
		return nil, err
	}
	mappings, err := effectiveSyncedEnvFiles(cfg)
	if err != nil {
		return nil, err
	}
	for _, mapping := range mappings {
		paths = append(paths, mapping.To)
	}
	return uniqueStrings(paths), nil
}

func effectivePortBindings(cfg EffectiveConfig) ([]PortBindingConfig, error) {
	bindings := make([]PortBindingConfig, 0, len(cfg.Ports.Bindings))
	for _, binding := range cfg.Ports.Bindings {
		binding.Target = strings.TrimSpace(binding.Target)
		if binding.Target != "" {
			binding.Target = normalizeRelativePath(binding.Target)
		}
		binding.Key = strings.TrimSpace(binding.Key)
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

type workflowConfig struct {
	Bootstrap BootstrapConfig        `json:"bootstrap"`
	Env       EnvConfig              `json:"env"`
	Ports     PortsConfig            `json:"ports"`
	Delivery  DeliveryConfig         `json:"delivery"`
	GitHub    GitHubConfig           `json:"github"`
	Linear    LinearConfig           `json:"linear"`
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
		Delivery:  cfg.Delivery,
		GitHub:    cfg.GitHub,
		Linear:    cfg.Linear,
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
		len(cfg.Env.SyncFiles) > 0 ||
		len(cfg.Ports.Bindings) > 0 ||
		cfg.Delivery.Remote != "" ||
		cfg.Delivery.SyncStrategy != "" ||
		len(cfg.Delivery.Preflight) > 0 ||
		cfg.Delivery.Cleanup != "" ||
		cfg.GitHub.Enabled ||
		cfg.GitHub.DraftOnSubmit ||
		cfg.GitHub.MergeMethod != "" ||
		cfg.GitHub.AutoMerge ||
		cfg.GitHub.DeleteRemoteBranch ||
		len(cfg.GitHub.Labels) > 0 ||
		len(cfg.GitHub.Reviewers) > 0 ||
		linearConfigured(cfg.Linear) ||
		len(cfg.Commands) > 0 ||
		len(cfg.Agents) > 0 ||
		cfg.Tmux.SessionName != "" ||
		len(cfg.Tmux.Windows) > 0
}

func workflowTrustEntries(cfg ConfigFile) []string {
	entries := make([]string, 0)
	for _, command := range cfg.Bootstrap.Commands {
		if strings.TrimSpace(command) != "" {
			entries = append(entries, "run bootstrap command: "+command)
		}
	}
	for _, mapping := range cfg.Bootstrap.EnvFiles {
		if mapping.From != "" && mapping.To != "" {
			entries = append(entries, fmt.Sprintf("copy bootstrap env file: %s -> %s", mapping.From, mapping.To))
		}
	}
	for _, target := range cfg.Env.Targets {
		if strings.TrimSpace(target.Path) != "" {
			entries = append(entries, "write managed env file: "+target.Path)
		}
	}
	for _, mapping := range cfg.Env.SyncFiles {
		if strings.TrimSpace(mapping.From) != "" && strings.TrimSpace(mapping.To) != "" {
			entries = append(entries, fmt.Sprintf("sync local env file: %s -> %s", mapping.From, mapping.To))
		}
	}
	for _, binding := range cfg.Ports.Bindings {
		if binding.Target != "" && binding.Key != "" {
			entries = append(entries, fmt.Sprintf("write preferred port binding: %s -> %s [%d-%d]", binding.Key, binding.Target, binding.Start, binding.End))
		}
	}
	if cfg.Delivery.Remote != "" {
		strategy := cfg.Delivery.SyncStrategy
		if strategy == "" {
			strategy = "rebase"
		}
		entries = append(entries, fmt.Sprintf("sync task branches against %s using %s", cfg.Delivery.Remote, strategy))
	}
	for name, command := range cfg.Commands {
		if strings.TrimSpace(command) != "" {
			entries = append(entries, fmt.Sprintf("run command %s: %s", name, command))
		}
	}
	for name, agent := range cfg.Agents {
		if strings.TrimSpace(agent.Command) != "" {
			entries = append(entries, fmt.Sprintf("run agent %s: %s", name, agent.Command))
		}
	}
	for _, window := range cfg.Tmux.Windows {
		if strings.TrimSpace(window.Command) != "" {
			entries = append(entries, fmt.Sprintf("run tmux window %s: %s", window.Name, window.Command))
		}
		for _, envFile := range window.EnvFiles {
			if strings.TrimSpace(envFile) != "" {
				entries = append(entries, fmt.Sprintf("source tmux window %s env file: %s", window.Name, envFile))
			}
		}
	}
	if cfg.GitHub.Enabled {
		entries = append(entries, "create, inspect, and merge pull requests with gh")
	}
	if linearConfigured(cfg.Linear) {
		details := fmt.Sprintf("read and update Linear issues using %s", effectiveLinearAPIKeyEnv(cfg.Linear))
		if profile := effectiveLinearCredentialProfile(cfg.Linear); profile != "" {
			details += fmt.Sprintf(" or stored Linear profile %s", profile)
		}
		entries = append(entries, details)
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
	Env          *renderEnvConfig             `toml:"env,omitempty" json:"env,omitempty"`
	Ports        *renderPortsConfig           `toml:"ports,omitempty" json:"ports,omitempty"`
	Delivery     *DeliveryConfig              `toml:"delivery,omitempty" json:"delivery,omitempty"`
	GitHub       *GitHubConfig                `toml:"github,omitempty" json:"github,omitempty"`
	Linear       *renderLinearConfig          `toml:"linear,omitempty" json:"linear,omitempty"`
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
	Name     string   `toml:"name" json:"name"`
	Cwd      string   `toml:"cwd,omitempty" json:"cwd,omitempty"`
	Command  string   `toml:"command,omitempty" json:"command,omitempty"`
	Agent    string   `toml:"agent,omitempty" json:"agent,omitempty"`
	EnvFiles []string `toml:"env_files,omitempty" json:"env_files,omitempty"`
}

type renderBootstrapConfig struct {
	Commands []string         `toml:"commands,omitempty" json:"commands,omitempty"`
	EnvFiles []EnvFileMapping `toml:"env_files,omitempty" json:"env_files,omitempty"`
}

type renderEnvConfig struct {
	Targets   []EnvTargetConfig `toml:"targets,omitempty" json:"targets,omitempty"`
	SyncFiles []EnvFileMapping  `toml:"sync_files,omitempty" json:"sync_files,omitempty"`
}

type renderLinearConfig struct {
	APIKeyEnv         string   `toml:"api_key_env,omitempty" json:"api_key_env,omitempty"`
	CredentialProfile string   `toml:"credential_profile,omitempty" json:"credential_profile,omitempty"`
	IssueSort         string   `toml:"issue_sort,omitempty" json:"issue_sort,omitempty"`
	TeamKeys          []string `toml:"team_keys,omitempty" json:"team_keys,omitempty"`
	PickerScope       string   `toml:"picker_scope,omitempty" json:"picker_scope,omitempty"`
	StartedState      string   `toml:"started_state,omitempty" json:"started_state,omitempty"`
	CompletedState    string   `toml:"completed_state,omitempty" json:"completed_state,omitempty"`
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
	if len(cfg.Env.Targets) > 0 || len(cfg.Env.SyncFiles) > 0 {
		env := renderEnvConfig{
			Targets:   cfg.Env.Targets,
			SyncFiles: cfg.Env.SyncFiles,
		}
		rendered.Env = &env
	}
	if len(cfg.Ports.Bindings) > 0 {
		ports := renderPortsConfig{
			Bindings: cfg.Ports.Bindings,
		}
		rendered.Ports = &ports
	}
	if cfg.Delivery.Remote != "" || cfg.Delivery.SyncStrategy != "" || len(cfg.Delivery.Preflight) > 0 || cfg.Delivery.Cleanup != "" {
		delivery := cfg.Delivery
		rendered.Delivery = &delivery
	}
	if cfg.GitHub.Enabled || cfg.GitHub.DraftOnSubmit || cfg.GitHub.MergeMethod != "" || cfg.GitHub.AutoMerge || cfg.GitHub.DeleteRemoteBranch || len(cfg.GitHub.Labels) > 0 || len(cfg.GitHub.Reviewers) > 0 {
		github := cfg.GitHub
		rendered.GitHub = &github
	}
	if linearConfigured(cfg.Linear) {
		linear := renderLinearConfig{
			APIKeyEnv:         effectiveLinearAPIKeyEnv(cfg.Linear),
			CredentialProfile: effectiveLinearCredentialProfile(cfg.Linear),
			IssueSort:         effectiveLinearIssueSort(cfg.Linear),
			TeamKeys:          cfg.Linear.TeamKeys,
			PickerScope:       effectiveLinearPickerScope(cfg.Linear),
			StartedState:      cfg.Linear.StartedState,
			CompletedState:    cfg.Linear.CompletedState,
		}
		rendered.Linear = &linear
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
				Name:     window.Name,
				Cwd:      window.Cwd,
				Command:  window.Command,
				Agent:    window.Agent,
				EnvFiles: window.EnvFiles,
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

# [bootstrap]
# commands = ["bun install --frozen-lockfile"]

[env]
targets = [{ path = ".env.agentflow" }]
# sync_files = [{ from = ".env", to = ".env" }]

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"

[commands]
review = "make review"
verify_quick = "make test"

[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and any relevant repo instructions, inspect the task context and relevant files, identify the likely verification path for the current surface, send a short status update with your plan, then wait for confirmation before editing."
resume_prompt = "Resume the current task, re-check AGENTS.md and local instructions if needed, inspect the current task state and recent changes, send a short status update with your next-step plan, then wait for confirmation before editing."

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

func validateTmuxWindowRelativePath(path string, label string) error {
	return validateWorktreeRelativePath(path, label)
}

func validateWorktreeRelativePath(path string, label string) error {
	return validateRelativePath(path, label, "worktree")
}

func validateRepoRelativePath(path string, label string) error {
	return validateRelativePath(path, label, "repo root")
}

func validateRelativePath(path string, label string, rootName string) error {
	if path == "" {
		return fmt.Errorf("%s must not be empty", label)
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("%s must be relative to the %s", label, rootName)
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%s must not escape the %s", label, rootName)
	}
	return nil
}

func normalizeRelativePath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
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
