# agentflow

`agentflow` is a repo-driven CLI for spinning up agent workspaces around tasks.

## Current scope

- One task maps to one worktree, one tmux session, and one primary interactive Codex window.
- Repo behavior comes from `.agents/workflow.toml`.
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

## Config files

- Global config: `~/.config/agentflow/config.toml`
- Repo manifest: `.agents/workflow.toml`
- Task state: `~/.local/state/agentflow/tasks/<repo-id>/<task-id>.json`
- Trust cache: `~/.local/state/agentflow/trust/<repo-id>.json`
- Managed env file: `.env.agentflow` by default

## Example manifest

```toml
[repo]
name = "coach-connect"
base_branch = "origin/main"
worktree_root = "../worktrees"
branch_prefix = "feature"
default_surface = "web"

[bootstrap]
commands = ["bun install --frozen-lockfile"]
env_files = [
  { from = ".env.example", to = ".env.local" },
]

[env]
managed_file = ".env.agentflow"

[ports]
enabled = true
key = "VITE_PORT"
start = 4101
end = 4199

[commands]
review = "bun run review:release"
verify_quick = "bun run verify:quick"
verify_web = "bun run verify:web"

[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and any relevant .agents content before acting."
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
