# agentflow

`agentflow` is a repo-driven CLI for spinning up agent workspaces around tasks.

## Current scope

- One task maps to one worktree, one tmux session, and one primary interactive Codex window.
- Repo workflow lives in `.agentflow/config.toml`.
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
- `agentflow config path`
- `agentflow config show`
- `agentflow config write [--force]`
- `agentflow config effective [--format toml|json]`

## Config files

- Repo config: `.agentflow/config.toml`
- Task state: `~/.local/state/agentflow/tasks/<repo-id>/<task-id>.json`
- Trust cache: `~/.local/state/agentflow/trust/<repo-id>.json`
- Default worktrees: `~/.local/state/agentflow/worktrees/<repo-id>/<task-slug>-<taskid6>`
- Managed env files: `.env.agentflow` by default, or multiple `env.targets` in monorepos
- Optional state overrides: `AGENTFLOW_STATE_HOME`, `AGENTFLOW_HOME`

Config ownership is:

- Repo config: checked-in repo identity and workflow
- Effective config: the merged runtime view shown by `agentflow config effective`

You can inspect the repo config with:

```sh
agentflow config
agentflow config show
agentflow config effective
```

Repo config is authoritative for workflow behavior. If `.agentflow/config.toml` is missing, `agentflow up` will refuse to invent tmux, agent, env, bootstrap, or command behavior.
The only remaining built-in defaults are tool-owned mechanics such as the default worktree root template and tmux session naming template.

For a section-by-section config reference, see [docs/config.md](/Users/euan-cowie/Projects/agentflow/docs/config.md).

Legacy note:

- `.agentflow/manifest.toml` is no longer supported.
- If it exists, agentflow will fail and tell you to merge it into `.agentflow/config.toml`.

## Example repo config

```toml
[repo]
name = "coach-connect"
base_branch = "origin/main"
worktree_root = "{{agentflow_state_home}}/worktrees/{{repo_id}}"
branch_prefix = "feature"
default_surface = "web"

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
prime_prompt = "Read AGENTS.md and any relevant repo instructions, then wait for my next instruction."
resume_prompt = "Resume the task, re-check local instructions if the repo changed, then wait for my next instruction."

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

Important behavior note:

- `prime_prompt` is sent to Codex when `agentflow up` creates the agent window
- `resume_prompt` is sent if agentflow later recreates or resumes that window
- agentflow appends task context before launch, so a non-empty prompt will usually cause Codex to start working immediately
- `agentflow attach` only reconnects to the tmux session that `up` already started

## Notes

- Repo-defined workflow is trust-gated by the workflow fingerprint of `.agentflow/config.toml`.
- The trust prompt is workflow-based, not command-only. It lists repo-defined side effects such as file writes and commands that agentflow will execute.
- Trust is requested before `agentflow up` creates task state, worktrees, or tmux sessions. Declining trust should leave no task behind.
- Changes to `repo.*` and `requirements.*` do not invalidate trust or count as config drift.
- Existing task identity is anchored to saved state. Config drift is additive for tmux windows and current-only for verify/review commands.
- Ports are treated as agentflow-managed preferred ports, not hard socket reservations.
- `worktree_root` supports `{{agentflow_state_home}}`, `{{repo_id}}`, and `{{repo}}`.
- `env.targets` declares the agentflow-managed env files for the task, and `ports.bindings` attaches generated ports to those targets.
- Runtime workflow does not fall back to implicit tmux windows or agent commands; declare them explicitly in `.agentflow/config.toml`.
- Repo-local Codex guidance for CLI/docs sync lives in [AGENTS.md](/Users/euan-cowie/Projects/agentflow/AGENTS.md) and [.agentflow/skills/cli-doc-sync/SKILL.md](/Users/euan-cowie/Projects/agentflow/.agentflow/skills/cli-doc-sync/SKILL.md).
