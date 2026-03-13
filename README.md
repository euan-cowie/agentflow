# agentflow

`agentflow` is a repo-driven CLI for spinning up agent workspaces around tasks.

## Current scope

- One task maps to one worktree, one tmux session, and one primary interactive Codex window.
- Repo conventions come from `.agentflow/config.toml`.
- Repo workflow comes from `.agentflow/manifest.toml`.
- The CLI owns lifecycle, trust, state, managed env output, worktree creation, and tmux recovery.
- Codex remains the interactive agent.

## Commands

- `agentflow up <task> [--surface ...] [--repo ...]`
- `agentflow attach <task>`
- `agentflow codex <task>`
- `agentflow verify <task> [--surface ...] [--foreground]`
- `agentflow review <task> [--foreground]`
- `agentflow down <task> [--delete-branch]`
- `agentflow list`
- `agentflow doctor`
- `agentflow repair <task>`
- `agentflow config`
- `agentflow config global path|show|write`
- `agentflow config repo path|show|write`
- `agentflow config manifest path|show|write`
- `agentflow config effective show [--format toml|json]`

## Config files

- Global config: `~/.config/agentflow/config.toml`
- Repo config: `.agentflow/config.toml`
- Repo manifest: `.agentflow/manifest.toml`
- Task state: `~/.local/state/agentflow/tasks/<repo-id>/<task-id>.json`
- Trust cache: `~/.local/state/agentflow/trust/<repo-id>.json`
- Default worktrees: `~/.local/state/agentflow/worktrees/<repo-id>/<task-slug>-<taskid6>`
- Managed env files: `.env.agentflow` by default, or multiple `env.targets` in monorepos
- Optional overrides: `AGENTFLOW_STATE_HOME`, `AGENTFLOW_HOME`, `AGENTFLOW_CONFIG_HOME`

Config ownership is:

- Global config: personal machine-local defaults
- Repo config: checked-in repo conventions and identity
- Repo manifest: checked-in executable workflow policy
- Effective config: the merged runtime view shown by `agentflow config effective show`

Merge precedence is domain-specific:

- Repo identity keys: CLI flags, repo config, global defaults, built-ins
- Workflow keys: CLI flags where applicable, repo manifest, global defaults, built-ins

You can inspect all layers with:

```sh
agentflow config
agentflow config global show
agentflow config repo show
agentflow config manifest show
agentflow config effective show
```

Global config is optional. Repo config and manifest are optional too; if they are missing, agentflow falls back to built-ins.

## Example global config

```toml
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
```

## Example repo config

```toml
[repo]
name = "coach-connect"
base_branch = "origin/main"
branch_prefix = "feature"
default_surface = "web"
```

## Example repo manifest

```toml
[bootstrap]
commands = ["bun install --frozen-lockfile"]
env_files = [
  { from = ".env.example", to = ".env.local" },
]

[env]
targets = [
  { path = "apps/web/.env.agentflow" },
  { path = "packages/api/.env.agentflow" },
]

[[ports.bindings]]
target = "apps/web/.env.agentflow"
key = "VITE_PORT"
start = 4101
end = 4199

[[ports.bindings]]
target = "packages/api/.env.agentflow"
key = "PORT"
start = 5101
end = 5199

[commands]
review = "bun run review:release"
verify_quick = "bun run verify:quick"
verify_web = "bun run verify:web"

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
binaries = ["git", "tmux", "codex", "nvim", "bun"]
mcp_servers = ["linear"]
```

## Notes

- Repo-defined commands are trust-gated by manifest fingerprint.
- Existing task identity is anchored to saved state. Manifest drift is additive for tmux windows and current-only for verify/review commands.
- Ports are treated as agentflow-managed preferred ports, not hard socket reservations.
- `worktree_root` supports `{{agentflow_state_home}}`, `{{repo_id}}`, and `{{repo}}`.
- `env.targets` declares the agentflow-managed env files for the task, and `ports.bindings` attaches generated ports to those targets.
