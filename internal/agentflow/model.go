package agentflow

import "time"

type RepoConfig struct {
	Name           string `toml:"name" json:"name"`
	BaseBranch     string `toml:"base_branch" json:"base_branch"`
	WorktreeRoot   string `toml:"worktree_root" json:"worktree_root"`
	BranchPrefix   string `toml:"branch_prefix" json:"branch_prefix"`
	DefaultSurface string `toml:"default_surface" json:"default_surface"`
}

type EnvFileMapping struct {
	From string `toml:"from" json:"from"`
	To   string `toml:"to" json:"to"`
}

type BootstrapConfig struct {
	Commands []string         `toml:"commands" json:"commands"`
	EnvFiles []EnvFileMapping `toml:"env_files" json:"env_files"`
}

type EnvConfig struct {
	ManagedFile string            `toml:"managed_file" json:"managed_file"`
	Targets     []EnvTargetConfig `toml:"targets" json:"targets"`
}

type EnvTargetConfig struct {
	Path string `toml:"path" json:"path"`
}

type PortsConfig struct {
	Enabled  bool                `toml:"enabled" json:"enabled"`
	File     string              `toml:"file" json:"file"`
	Key      string              `toml:"key" json:"key"`
	Start    int                 `toml:"start" json:"start"`
	End      int                 `toml:"end" json:"end"`
	Bindings []PortBindingConfig `toml:"bindings" json:"bindings"`
}

type PortBindingConfig struct {
	Target string `toml:"target" json:"target"`
	Key    string `toml:"key" json:"key"`
	Start  int    `toml:"start" json:"start"`
	End    int    `toml:"end" json:"end"`
}

type AgentConfig struct {
	Runner       string `toml:"runner" json:"runner"`
	Command      string `toml:"command" json:"command"`
	PrimePrompt  string `toml:"prime_prompt" json:"prime_prompt"`
	ResumePrompt string `toml:"resume_prompt" json:"resume_prompt"`
}

type TmuxWindowConfig struct {
	Name    string `toml:"name" json:"name"`
	Command string `toml:"command" json:"command"`
	Agent   string `toml:"agent" json:"agent"`
}

type TmuxConfig struct {
	SessionName string             `toml:"session_name" json:"session_name"`
	Windows     []TmuxWindowConfig `toml:"windows" json:"windows"`
}

type RequirementsConfig struct {
	Binaries   []string `toml:"binaries" json:"binaries"`
	MCPServers []string `toml:"mcp_servers" json:"mcp_servers"`
}

type WorkflowConfig struct {
	Repo         RepoConfig             `toml:"repo" json:"repo"`
	Bootstrap    BootstrapConfig        `toml:"bootstrap" json:"bootstrap"`
	Env          EnvConfig              `toml:"env" json:"env"`
	Ports        PortsConfig            `toml:"ports" json:"ports"`
	Commands     map[string]string      `toml:"commands" json:"commands"`
	Agents       map[string]AgentConfig `toml:"agents" json:"agents"`
	Tmux         TmuxConfig             `toml:"tmux" json:"tmux"`
	Requirements RequirementsConfig     `toml:"requirements" json:"requirements"`
}

type RuntimeConfig struct {
	RepoRoot            string
	RepoID              string
	ManifestPath        string
	ManifestExists      bool
	ManifestFingerprint string
	GlobalConfigPath    string
	StateRoot           string
	Trusted             bool
	Config              WorkflowConfig
	ManifestConfig      WorkflowConfig
}

type TaskRef struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Slug   string `json:"slug"`
}

type TaskState struct {
	TaskID              string             `json:"task_id"`
	TaskRef             TaskRef            `json:"task_ref"`
	Status              string             `json:"status"`
	FailureReason       string             `json:"failure_reason,omitempty"`
	RepoRoot            string             `json:"repo_root"`
	RepoID              string             `json:"repo_id"`
	WorktreePath        string             `json:"worktree_path"`
	Branch              string             `json:"branch"`
	BaseBranch          string             `json:"base_branch"`
	Surface             string             `json:"surface"`
	TmuxSession         string             `json:"tmux_session"`
	PrimaryAgentWindow  string             `json:"primary_agent_window"`
	CodexSessionID      string             `json:"codex_session_id,omitempty"`
	PortBindings        []PortBindingState `json:"port_bindings,omitempty"`
	ManagedEnvFiles     []string           `json:"managed_env_files,omitempty"`
	AllocatedPort       int                `json:"allocated_port,omitempty"`
	PortKey             string             `json:"port_key,omitempty"`
	ManagedEnvFile      string             `json:"managed_env_file"`
	ManifestFingerprint string             `json:"manifest_fingerprint,omitempty"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type PortBindingState struct {
	Target string `json:"target"`
	Key    string `json:"key"`
	Port   int    `json:"port"`
}

type TaskSummary struct {
	TaskID        string
	RepoRoot      string
	Worktree      string
	Branch        string
	Session       string
	Surface       string
	Status        string
	ManifestDrift bool
	LogPath       string
}

type TrustRecord struct {
	RepoRoot            string    `json:"repo_root"`
	RepoID              string    `json:"repo_id"`
	ManifestFingerprint string    `json:"manifest_fingerprint"`
	AcceptedAt          time.Time `json:"accepted_at"`
}

type WorktreeInfo struct {
	Path      string
	Head      string
	BranchRef string
	Locked    bool
	Prunable  bool
}

type DoctorCheck struct {
	Name    string
	OK      bool
	Details string
}

const (
	StatusCreating = "creating"
	StatusReady    = "ready"
	StatusBroken   = "broken"
	StatusDeleting = "deleting"
)

func (s TaskState) EffectiveManagedEnvFiles() []string {
	if len(s.ManagedEnvFiles) > 0 {
		return append([]string(nil), uniqueStrings(s.ManagedEnvFiles)...)
	}
	if s.ManagedEnvFile != "" {
		return []string{s.ManagedEnvFile}
	}
	return nil
}

func (s TaskState) EffectivePortBindings() []PortBindingState {
	if len(s.PortBindings) > 0 {
		return append([]PortBindingState(nil), s.PortBindings...)
	}
	if s.AllocatedPort == 0 {
		return nil
	}
	target := s.ManagedEnvFile
	key := s.PortKey
	if key == "" {
		key = "AGENTFLOW_PORT"
	}
	return []PortBindingState{{
		Target: target,
		Key:    key,
		Port:   s.AllocatedPort,
	}}
}
