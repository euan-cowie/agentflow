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
	Targets []EnvTargetConfig `toml:"targets" json:"targets"`
}

type EnvTargetConfig struct {
	Path string `toml:"path" json:"path"`
}

type PortsConfig struct {
	Bindings []PortBindingConfig `toml:"bindings" json:"bindings"`
}

type DeliveryConfig struct {
	Remote       string   `toml:"remote" json:"remote"`
	SyncStrategy string   `toml:"sync_strategy" json:"sync_strategy"`
	Preflight    []string `toml:"preflight" json:"preflight"`
	Cleanup      string   `toml:"cleanup" json:"cleanup"`
}

type GitHubConfig struct {
	Enabled            bool     `toml:"enabled" json:"enabled"`
	DraftOnSubmit      bool     `toml:"draft_on_submit" json:"draft_on_submit"`
	MergeMethod        string   `toml:"merge_method" json:"merge_method"`
	AutoMerge          bool     `toml:"auto_merge" json:"auto_merge"`
	DeleteRemoteBranch bool     `toml:"delete_remote_branch" json:"delete_remote_branch"`
	Labels             []string `toml:"labels" json:"labels"`
	Reviewers          []string `toml:"reviewers" json:"reviewers"`
}

type LinearConfig struct {
	APIKeyEnv         string   `toml:"api_key_env" json:"api_key_env"`
	CredentialProfile string   `toml:"credential_profile" json:"credential_profile"`
	TeamKeys          []string `toml:"team_keys" json:"team_keys"`
	PickerScope       string   `toml:"picker_scope" json:"picker_scope"`
	StartedState      string   `toml:"started_state" json:"started_state"`
	CompletedState    string   `toml:"completed_state" json:"completed_state"`
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

type ConfigFile struct {
	Repo         RepoConfig             `toml:"repo" json:"repo"`
	Bootstrap    BootstrapConfig        `toml:"bootstrap" json:"bootstrap"`
	Env          EnvConfig              `toml:"env" json:"env"`
	Ports        PortsConfig            `toml:"ports" json:"ports"`
	Delivery     DeliveryConfig         `toml:"delivery" json:"delivery"`
	GitHub       GitHubConfig           `toml:"github" json:"github"`
	Linear       LinearConfig           `toml:"linear" json:"linear"`
	Commands     map[string]string      `toml:"commands" json:"commands"`
	Agents       map[string]AgentConfig `toml:"agents" json:"agents"`
	Tmux         TmuxConfig             `toml:"tmux" json:"tmux"`
	Requirements RequirementsConfig     `toml:"requirements" json:"requirements"`
}

type EffectiveConfig = ConfigFile

type ConfigFileInfo struct {
	Path   string
	Exists bool
}

type ConfigOverview struct {
	Repo ConfigFileInfo
}

type RuntimeConfig struct {
	RepoRoot            string
	RepoID              string
	ConfigPath          string
	ConfigExists        bool
	WorkflowFingerprint string
	StateRoot           string
	Trusted             bool
	Config              ConfigFile
	EffectiveConfig     EffectiveConfig
}

type TaskRef struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	ID     string `json:"id,omitempty"`
	URL    string `json:"url,omitempty"`
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
	WorkflowFingerprint string             `json:"workflow_fingerprint,omitempty"`
	IssueState          string             `json:"issue_state,omitempty"`
	Delivery            TaskDeliveryState  `json:"delivery,omitempty"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type TaskDeliveryState struct {
	State             string    `json:"state,omitempty"`
	Remote            string    `json:"remote,omitempty"`
	RemoteBranch      string    `json:"remote_branch,omitempty"`
	BaseRef           string    `json:"base_ref,omitempty"`
	LastBaseSHA       string    `json:"last_base_sha,omitempty"`
	LastHeadSHA       string    `json:"last_head_sha,omitempty"`
	PullRequestNumber int       `json:"pull_request_number,omitempty"`
	PullRequestURL    string    `json:"pull_request_url,omitempty"`
	PullRequestState  string    `json:"pull_request_state,omitempty"`
	LastSyncedAt      time.Time `json:"last_synced_at,omitempty"`
	LastSubmittedAt   time.Time `json:"last_submitted_at,omitempty"`
	MergedAt          time.Time `json:"merged_at,omitempty"`
}

type PortBindingState struct {
	Target string `json:"target"`
	Key    string `json:"key"`
	Port   int    `json:"port"`
}

type TaskSummary struct {
	TaskID      string
	TaskTitle   string
	RepoRoot    string
	Worktree    string
	Branch      string
	Session     string
	Surface     string
	Status      string
	ConfigDrift bool
	LogPath     string
	Issue       string
	IssueURL    string
	IssueState  string
	Delivery    TaskDeliveryState
	Dirty       bool
	Ahead       int
	Behind      int
	ChecksState string
	MergeState  string
}

type TaskStatus struct {
	TaskID        string
	TaskTitle     string
	RepoRoot      string
	Worktree      string
	Branch        string
	Session       string
	Surface       string
	Status        string
	FailureReason string
	ConfigDrift   bool
	Issue         string
	IssueURL      string
	IssueState    string
	Delivery      TaskDeliveryState
	Dirty         bool
	Ahead         int
	Behind        int
	ChecksState   string
	MergeState    string
}

type TrustRecord struct {
	RepoRoot            string    `json:"repo_root"`
	RepoID              string    `json:"repo_id"`
	WorkflowFingerprint string    `json:"workflow_fingerprint"`
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

const (
	DeliveryStateLocal     = "local"
	DeliveryStateDraft     = "draft"
	DeliveryStateSubmitted = "submitted"
	DeliveryStateQueued    = "queued"
	DeliveryStateMerged    = "merged"
	DeliveryStateClosed    = "closed"
	DeliveryStateBlocked   = "blocked"
)

func (s TaskState) EffectiveManagedEnvFiles() []string {
	return append([]string(nil), uniqueStrings(s.ManagedEnvFiles)...)
}

func (s TaskState) EffectivePortBindings() []PortBindingState {
	return append([]PortBindingState(nil), s.PortBindings...)
}
