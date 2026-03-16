# agentflow

`agentflow` is a repo-driven CLI for spinning up agent workspaces around tasks.

## Current scope

- One task maps to one worktree, one tmux session, and one primary interactive Codex window.
- Repo workflow lives in `.agentflow/config.toml`.
- The CLI owns lifecycle, trust, state, managed env output, worktree creation, and tmux recovery.
- Codex remains the interactive agent.

## Commands

- `agentflow up [task] [--surface ...] [--repo ...]`
- `agentflow auth linear login|logout|status|list`
- `agentflow attach <task>`
- `agentflow status [task]`
- `agentflow codex <task>`
- `agentflow issues list`
- `agentflow sync <task> [--all] [--push]`
- `agentflow submit <task> [--draft|--ready]`
- `agentflow land <task> [--watch]`
- `agentflow verify <task> [--surface ...] [--foreground]`
- `agentflow review <task> [--foreground]`
- `agentflow down <task> [--delete-branch] [--force]`
- `agentflow list [--verbose]`
- `agentflow gc [task]`
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
Delivery commands also use repo-configured defaults from `[delivery]` when present.

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

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"

[linear]
api_key_env = "LINEAR_API_KEY"
credential_profile = "acme"
issue_sort = "state_then_updated"

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
prime_prompt = "Read AGENTS.md and any relevant repo instructions, inspect the task context and relevant files, identify the likely verification path for the current surface, send a short status update with your plan, then wait for confirmation before editing."
resume_prompt = "Resume the task, re-check local instructions if needed, inspect the current task state and recent changes, send a short status update with your next-step plan, then wait for confirmation before editing."

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

Task inputs now support:

- manual titles such as `fix auth flow`
- Linear issue keys such as `AF-123` when `[linear]` is configured
- explicit prefixes `manual:...` and `linear:...` when you need to disambiguate

Linear credentials resolve in this order:

1. the env var named by `linear.api_key_env` or `LINEAR_API_KEY`
2. the stored credential profile named by `linear.credential_profile`, when configured
3. the legacy stored credential written by `agentflow auth linear login` when no profile is configured

Important behavior note:

- `prime_prompt` is sent to Codex when `agentflow up` creates the agent window
- `resume_prompt` is sent if agentflow later recreates or resumes that window
- agentflow appends workflow context, Linear issue context when available, and task/worktree context before launch
- a proactive prompt should make Codex inspect and plan first, not start editing blindly
- `agentflow attach` only reconnects to the tmux session that `up` already started

## Async delivery flow

The delivery layer sits on top of the existing task lifecycle:

1. `agentflow up [task]` creates the worktree, tmux session, and agent window.
2. `agentflow status [task]` shows local branch health plus PR/check state when GitHub integration is enabled.
3. `agentflow sync <task>` fetches the configured remote and rebases or merges the task branch onto the configured base branch.
4. `agentflow submit <task>` pushes the task branch, creates or reuses a PR when `[github].enabled = true`, and links the PR back to Linear for issue-backed tasks.
5. `agentflow land <task>` runs preflight commands, syncs the branch, pushes it, picks a GitHub-compatible merge strategy when `github.merge_method = "auto"`, defers to merge queue requirements when necessary, and marks the linked Linear issue complete once agentflow observes the merge.
6. `agentflow gc [task]` removes merged task worktrees, tmux sessions, and local branches.

GitHub automation is optional. If `[github].enabled` is omitted or false, `submit` still pushes the branch but `land` will refuse to continue.
When `[linear]` is configured, running `agentflow up` without a task opens a full-screen issue picker over your active Linear issues.
`agentflow list` remains the local task list, while `agentflow issues list` shows the same ordered issue set that the `up` picker uses.
`agentflow down` accepts a tracked issue key like `TGG-132`, an explicit ref like `linear:TGG-132`, or an exact tracked task title; `--force` discards dirty worktree changes after a confirmation prompt.

## Notes

- Repo-defined workflow is trust-gated by the workflow fingerprint of `.agentflow/config.toml`.
- The trust prompt is workflow-based, not command-only. It lists repo-defined side effects such as file writes and commands that agentflow will execute.
- Trust is requested before `agentflow up` creates task state, worktrees, or tmux sessions. Declining trust should leave no task behind.
- Changes to `repo.*` and `requirements.*` do not invalidate trust or count as config drift.
- Existing task identity is anchored to saved state. Config drift is additive for tmux windows and current-only for verify/review commands.
- Ports are treated as agentflow-managed preferred ports, not hard socket reservations.
- `worktree_root` supports `{{agentflow_state_home}}`, `{{repo_id}}`, and `{{repo}}`.
- `env.targets` declares the agentflow-managed env files for the task, and `ports.bindings` attaches generated ports to those targets.
- `[delivery]` configures branch sync, preflight, and async cleanup behavior.
- `[github]` enables optional `gh` integration for PR creation, checks, and merge automation.
- `github.merge_method = "auto"` is GitHub-policy-aware: it prefers queue-compatible behavior first, then a linear-history-safe method when required, and otherwise falls back to regular merge.
- `[linear]` enables optional Linear issue selection plus started/completed state sync for issue-backed tasks.
- `linear.issue_sort` controls the default ordering shared by `agentflow up` and `agentflow issues list`; the default is `state_then_updated`.
- `agentflow auth linear login --profile <name>` stores a reusable named Linear API key locally for repos that pin `[linear].credential_profile`.
- `agentflow auth linear list` shows the stored legacy credential plus any named Linear profiles.
- `agentflow doctor` reports GitHub merge policy details, warns when merge-queue repos need CI coverage for `merge_group` or `gh-readonly-queue/*` refs, and advises dependency-managed repos to configure `[bootstrap].commands` when a JS lockfile is present.
- repos with dependency lockfiles should declare `[bootstrap].commands` so new task worktrees land in a ready-to-run environment.
- Runtime workflow does not fall back to implicit tmux windows or agent commands; declare them explicitly in `.agentflow/config.toml`.
- Repo-local Codex guidance for CLI/docs sync lives in [AGENTS.md](/Users/euan-cowie/Projects/agentflow/AGENTS.md) and [.agentflow/skills/cli-doc-sync/SKILL.md](/Users/euan-cowie/Projects/agentflow/.agentflow/skills/cli-doc-sync/SKILL.md).
