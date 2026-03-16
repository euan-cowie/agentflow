# Repo Config Reference

`agentflow` uses one checked-in repo config file: [.agentflow/config.toml](/Users/euan-cowie/Projects/agentflow/.agentflow/config.toml).

This file serves two roles:

- repo identity and defaults
- runnable workflow definition for worktrees, tmux, env files, verification, agents, and delivery

Repo config is authoritative for workflow behavior. If the file is missing, `agentflow up` will not invent tmux windows, agent commands, env targets, bootstrap commands, or verify/review commands for you.

Task inputs can be:

- manual titles such as `fix auth flow`
- Linear issue keys such as `AF-123` when `[linear]` is configured
- explicit `manual:...` and `linear:...` refs when disambiguation matters

For task-targeting commands such as `attach`, `codex`, `sync`, `submit`, `land`, `verify`, `review`, `down`, and `repair`, the task argument can be omitted when the command is run inside that tracked task worktree. From the repo root, pass the task explicitly.

Linear credentials resolve in this order:

1. the env var named by `linear.api_key_env` or `LINEAR_API_KEY`
2. the stored credential profile named by `linear.credential_profile`, when configured
3. the legacy stored credential written by `agentflow auth linear login` when no profile is configured

The only built-in defaults that remain are tool-owned mechanics:

- default worktree root template: `{{agentflow_state_home}}/worktrees/{{repo_id}}`
- default tmux session naming template: `{{repo}}-{{task}}-{{id}}`
- repo name fallback: basename of the repo root when `repo.name` is omitted

## Sections

### `[repo]`

Repo identity and task defaults.

Fields:

- `name`: human-friendly repo name used in tmux/session naming
- `base_branch`: branch or ref used as the default worktree base
- `worktree_root`: where agentflow creates task worktrees
- `branch_prefix`: prefix for generated task branches
- `default_surface`: default surface used by `verify` command resolution

`base_branch` should be treated as required for `agentflow up`, even though other commands can still inspect config without it.

### `[bootstrap]`

One-time setup that runs when a new task worktree is created.

Fields:

- `commands`: shell commands run in order after worktree creation
- `env_files`: copy-once env seeding from `{ from, to }`

Use this for install/bootstrap steps, not recurring dev commands.
For dependency-managed repos, declare install/setup here so new worktrees are ready before the agent starts.
Use `env_files` for copy-once templates such as `.env.example -> .env.local`, then layer real local secrets through `env.sync_files` and task-specific values through `env.targets`.

### `[env]`

Agentflow-managed env files.

Fields:

- `targets`: list of `{ path = "..." }`
- `sync_files`: list of `{ from = "...", to = "..." }`

`targets` are agentflow-generated env files. Agentflow may create or rewrite them while preparing a task, and it removes them during teardown.
Use them for dynamic per-task values such as generated ports, callback URLs, or service endpoints that should differ across worktrees.

`sync_files` copy real local env files from the canonical repo root into each task worktree. Agentflow refreshes them on `up`, reconcile, and `repair`, and removes the synced targets during teardown.

Rules:

- `sync_files[*].from` is relative to the canonical repo root
- `sync_files[*].to` is relative to the task worktree
- synced targets must not overlap `env.targets`
- missing `sync_files[*].from` files fail startup instead of silently skipping

This is the layer to use for real local secrets or stable per-developer config that should be the same across every worktree.

This section is optional. If you do not need agentflow-managed env files, omit it entirely.

### `[ports]`

Generated preferred port bindings for managed env targets.

Fields:

- `[[ports.bindings]]`
  - `target`: env target path from `env.targets`
  - `key`: env var key to write
  - `start`: range start
  - `end`: range end

These are preferred free ports, not hard OS reservations.

### `[commands]`

Named repo commands used by agentflow.

Common entries:

- `review`
- `verify_quick`
- `verify_<surface>`

`agentflow verify` resolves surface-specific commands first, then falls back to `verify_quick`.

These commands are also used by the delivery layer:

- `agentflow submit` may create a PR after pushing the branch
- `agentflow land` runs the configured `delivery.preflight` entries in order

### `[agents]`

Agent profiles used by tmux windows.

Common fields:

- `runner`
- `command`
- `prime_prompt`
- `resume_prompt`

Today `runner` is effectively Codex-only.

Agent profiles are explicit now. If a tmux window references an agent, that agent must declare its command in repo config.

`prime_prompt` and `resume_prompt` are active startup messages, not passive metadata:

- `prime_prompt` is sent to Codex when agentflow creates the agent window for a new task
- `resume_prompt` is sent when agentflow recreates or resumes that agent window later

Agentflow also appends task context (`Task`, `Task ID`, and `Worktree`) to those prompts before launching Codex.
It also appends workflow context (`Surface`, resolved `Verify`, and `Review` commands) plus saved Linear issue context when available.

This means a repo that declares an agent-backed tmux window will start Codex during `agentflow up`. If the prompt tells Codex to read files or inspect the repo, Codex will start doing that immediately. If you want a quieter startup, keep the prompt minimal and explicitly tell Codex to wait for the next instruction.

### `[tmux]`

Session naming and window layout.

Fields:

- `session_name`
- `[[tmux.windows]]`
  - `name`
  - `cwd`
  - `command` or `agent`
  - `env_files`

V1 supports at most one agent-backed window.

For `agentflow up`, `tmux.windows` should be treated as required. Agentflow no longer injects default `editor`, `verify`, or `codex` windows behind your back.

`cwd` is relative to the task worktree. Use it when a window should start in an app subdirectory such as `apps/web`.

`env_files` are sourced in order before the window command runs. Paths are relative to the task worktree, which makes it easy to layer a stable app env file such as `apps/web/.env.local` with a generated overlay such as `apps/web/.env.agentflow`.

Because agentflow sources these files through `sh`, keep them shell-compatible (`KEY=value` style) if you plan to load them through `tmux.windows[*].env_files`.

If one of those windows is agent-backed, `agentflow up` now scaffolds the tmux session first, syncs tracker state, and only then launches the agent in the prepared window. `agentflow attach` only reconnects to the already-running session; it does not create a second startup prompt on its own.

### `[delivery]`

Branch landing and cleanup behavior.

Fields:

- `remote`
- `sync_strategy`
- `preflight`
- `cleanup`

Current supported values:

- `sync_strategy`: `rebase` or `merge`
- `cleanup`: `async` or `manual`

Behavior:

- `agentflow sync` fetches `delivery.remote` and updates the task branch against the task base branch using `delivery.sync_strategy`
- `agentflow land` runs each `delivery.preflight` entry before it enables merge
- `agentflow gc` is the async cleanup path when `delivery.cleanup = "async"`

When `delivery.preflight` is omitted, agentflow currently defaults to `["review", "verify"]`.

### `[github]`

Optional GitHub CLI integration for PR-backed delivery.

Fields:

- `enabled`
- `draft_on_submit`
- `merge_method`
- `auto_merge`
- `delete_remote_branch`
- `labels`
- `reviewers`

Behavior:

- if `enabled = true`, `agentflow submit` creates or reuses a PR with `gh`
- `agentflow status` includes PR/check/merge metadata
- `agentflow land` uses `gh pr ready` and `gh pr merge`
- `agentflow gc` deletes the remote branch only after local task cleanup succeeds when `delete_remote_branch = true`

Current supported values:

- `merge_method`: `auto`, `squash`, `merge`, or `rebase`
- `merge_method = "auto"` is GitHub-policy-aware:
  it omits strategy flags when the base branch requires a merge queue,
  prefers `squash` or `rebase` when linear history is required,
  and otherwise falls back to a regular merge when that is allowed
- explicit `merge_method` values fail fast if the base branch policy disallows them

### `[linear]`

Optional Linear issue integration for issue-backed task creation.

Fields:

- `api_key_env`
- `credential_profile`
- `issue_sort`
- `team_keys`
- `picker_scope`
- `started_state`
- `completed_state`

Behavior:

- when configured, `agentflow up` with no task opens a full-screen issue picker
- `agentflow issues list` prints the same issue set and ordering that the `up` picker uses
- `agentflow list` continues to show local tracked tasks, not tracker issues
- bare issue keys such as `AF-123` resolve to Linear tasks
- successful first-time `agentflow up` moves the issue into a started workflow state
- `agentflow submit` links the PR back to the issue when a PR URL exists
- `agentflow land`, `agentflow status`, and `agentflow gc` can mark merged work complete once agentflow observes the merge

Credential workflow:

- `agentflow auth linear login` validates and stores the legacy reusable Linear API key
- `agentflow auth linear login --profile <name>` validates and stores a named Linear credential profile
- `agentflow auth linear list` shows the stored legacy credential plus any named Linear profiles
- `agentflow auth linear status` shows whether resolution is using env, a named profile, or the legacy stored credential
- `agentflow auth linear logout` deletes the legacy stored credential
- `agentflow auth linear logout --profile <name>` deletes one named Linear credential profile

Current supported values:

- `issue_sort`: `linear`, `identifier`, `updated`, or `state_then_updated`
- `issue_sort = "state_then_updated"` puts active issues first, then sorts by most recent updates
- `picker_scope`: `assigned` or `team`
- `picker_scope = "assigned"` lists the viewer's assigned, non-completed issues
- `picker_scope = "team"` requires `team_keys`

Defaults:

- `api_key_env`: `LINEAR_API_KEY`
- `issue_sort`: `state_then_updated`
- `picker_scope`: `assigned`

`credential_profile` is optional. When set, the repo opts into that named local Linear profile and no longer falls back to the legacy global stored credential.

`started_state` and `completed_state` are optional workflow state names. If omitted, agentflow falls back to the first Linear workflow state with type `started` or `completed` for that issue's team.

### `[requirements]`

Repo requirements checked by `agentflow doctor`.

Fields:

- `binaries`
- `mcp_servers`

These affect doctor output but do not change trust or config drift behavior.

When GitHub delivery is enabled, `agentflow doctor` also reports the detected base-branch merge policy and warns when merge-queue repos need CI coverage for `merge_group` or `gh-readonly-queue/*` refs.
When the repo root contains `bun.lock`, `package-lock.json`, `pnpm-lock.yaml`, or `yarn.lock` and `[bootstrap].commands` is empty, `agentflow doctor` adds a bootstrap-readiness advisory.

## Trust And Drift

Agentflow computes a workflow fingerprint from these sections:

- `bootstrap`
- `env`
- `ports`
- `delivery`
- `github`
- `linear`
- `commands`
- `agents`
- `tmux`

Changes to those sections:

- invalidate repo trust
- show up as config drift on existing tasks

The trust prompt is intentionally narrower than “all workflow config”. It should describe only the repo-defined side effects that agentflow will carry out, such as:

- commands it will run
- tmux window commands it will launch
- agent commands it will launch
- synced local env files, managed env files, or port bindings it will write
- branch sync and push behavior
- GitHub PR operations when enabled
- Linear issue reads and updates when enabled

For `agentflow up`, trust is requested before the tool creates the task record, worktree, or tmux session. Declining or interrupting the trust prompt should not leave a new task behind.

Agentflow does **not** include these in the workflow fingerprint:

- `repo`
- `requirements`

So changing `base_branch`, `default_surface`, or doctor-only requirements does not trigger a new trust prompt by itself.

## Effective Config

Use:

```sh
agentflow config effective
```

This prints the merged runtime config after tool-owned defaults and repo config are applied. It is useful for checking naming/storage defaults and confirming that the workflow declared in `.agentflow/config.toml` is exactly what agentflow will use.

## Current Repo Example

The current repo config is:

```toml
[repo]
name = "agentflow"
base_branch = "main"
default_surface = "cli"

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

[linear]
api_key_env = "LINEAR_API_KEY"

[commands]
review = "go test ./..."
verify_quick = "go test ./..."
verify_cli = "go test ./..."

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
binaries = ["git", "tmux", "codex", "nvim", "go"]
```
