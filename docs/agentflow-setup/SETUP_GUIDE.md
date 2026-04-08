# Agentflow Setup Guide

This guide is meant to be copied into repos that use `agentflow`.

It assumes the current `agentflow` model:

- one task = one branch
- one task = one Git worktree
- one task = one tmux session
- one task = one primary agent window

`agentflow` works best when tasks are independent and short-lived. It is a strong fit for multiple Linear issues in parallel. It is not the ideal workflow for deep stacked branches where task B depends on unpublished task A.

## Goals

Use this guide to:

- set up a repo-local `.agentflow/config.toml`
- define predictable verify and review commands
- make multiple concurrent task worktrees safe and repeatable
- give agents a standard operating workflow

## Prerequisites

Install the binaries your repo needs.

Common baseline:

- `git`
- `tmux`
- `codex`

Optional but common:

- `gh` for GitHub-backed delivery
- `bun`, `pnpm`, `npm`, `go`, or `make` depending on the repo

Authentication:

- run `gh auth status` if `[github]` is enabled
- run `agentflow auth linear status` if `[linear]` is enabled
- if needed, store a named Linear credential with `agentflow auth linear login --profile <name>`

## Core Concepts

### Task

A task is the tracked unit of work. It can come from:

- a manual title such as `fix auth flow`
- a Linear issue key such as `AF-123`

### Surface

A surface is a repo-defined label attached to the task.

Examples:

- `web`
- `api`
- `cli`
- `ios`
- `default`

Today, the main use of a surface is verify command selection:

- `agentflow verify` tries `verify_<surface>` first
- if that does not exist, it falls back to `verify_quick`

If you do not pass `--surface` to `agentflow up`, the task uses `repo.default_surface`.

### Verify

`agentflow verify` runs the repo-defined verification command for the task.

It does not:

- fetch
- rebase
- merge
- create a PR

It does:

- select the right verify command from `[commands]`
- run in the `verify` tmux window when available
- fall back to a foreground run when needed

### Sync

`agentflow sync` updates the task branch against the configured base branch.

It does:

- require a clean worktree, ignoring agentflow-managed env files
- fetch the configured remote
- rebase or merge the task branch on top of the base branch
- optionally push if `--push` is used

It does not run tests by itself.

## Recommended Workflow

### First-time repo setup

1. Add `.agentflow/config.toml`.
2. Add repo instructions in `AGENTS.md`.
3. Run `agentflow doctor`.
4. Fix any missing binaries, auth, or bootstrap issues.

### Daily workflow

1. Start a task:

   ```sh
   agentflow up AF-123
   ```

2. Start another independent task in parallel:

   ```sh
   agentflow up AF-124 --surface api
   ```

3. Re-enter an existing task:

   ```sh
   agentflow attach AF-123
   agentflow codex AF-123
   ```

4. Re-sync before resuming meaningful work:

   ```sh
   agentflow sync AF-123
   agentflow sync --all
   ```

5. Run verification:

   ```sh
   agentflow verify AF-123
   agentflow verify AF-123 --surface web
   ```

6. Open or update a draft PR:

   ```sh
   agentflow submit AF-123 --draft
   ```

7. Land when ready:

   ```sh
   agentflow land AF-123 --watch
   ```

8. Clean up merged tasks:

   ```sh
   agentflow gc
   ```

## Recommended Operating Rules

- Prefer one independent issue per worktree.
- Rebase frequently when using `sync_strategy = "rebase"`.
- Submit early as draft PRs for async review.
- Keep `review` and `verify_*` commands deterministic and non-interactive.
- Use `ports.bindings` if more than one task may run a local server at once.
- Avoid stacked dependent branches unless you are intentionally working outside the main sweet spot of agentflow.

## Config Structure

These sections are the ones most repos need.

### `[repo]`

Use this for repo identity and task defaults.

Typical choices:

- `name = "<repo-name>"`
- `base_branch = "origin/main"`
- `worktree_root = "{{agentflow_state_home}}/worktrees/{{repo_id}}"`
- `branch_prefix = "af"`
- `default_surface = "default"`

### `[bootstrap]`

Use this for one-time setup when a new worktree is created.

Good examples:

- `bun install --frozen-lockfile`
- `pnpm install --frozen-lockfile`
- `go mod download`

Do not put long-running dev servers here.

### `[env]`

Use:

- `sync_files` for copying real local secrets from repo root into each worktree
- `targets` for generated task-local env overlays

### `[[ports.bindings]]`

Use this when concurrent tasks need distinct ports.

### `[delivery]`

Recommended default:

```toml
[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"
```

### `[github]`

Use this when `submit` should create PRs and `land` should use GitHub merge flow.

Recommended default:

```toml
[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true
```

### `[linear]`

Use this for issue-backed task creation and issue state syncing.

Recommended default:

```toml
[linear]
api_key_env = "LINEAR_API_KEY"
credential_profile = "<profile>"
picker_scope = "assigned"
issue_sort = "state_then_updated"
```

### `[commands]`

This is where you define repo command structure.

Minimal shape:

```toml
[commands]
review = "<repo-wide review command>"
verify_quick = "<fast default verification>"
verify_default = "<default-surface verification>"
verify_web = "<web verification>"
verify_api = "<api verification>"
verify_cli = "<cli verification>"
```

Rules:

- `review` is used by `agentflow review`
- `verify_quick` is the generic fallback
- `verify_<surface>` is optional and only needed for surfaces you actually use

### `[agents.default]`

Use a conservative prompt that tells the agent to inspect first and edit second.

Recommended profile:

```toml
[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and repo instructions, inspect the task context and relevant files, identify the likely verification path for the current surface, send a short status update with your plan, then wait for confirmation before editing."
resume_prompt = "Resume the current task, re-check AGENTS.md and local instructions if needed, inspect the current state and recent changes, send a short status update with your next-step plan, then wait for confirmation before editing."
```

### `[tmux]`

Keep the baseline simple:

- `editor`
- `verify`
- `codex`

Add app-specific windows only when they genuinely help.

## Standard Config Template

```toml
[repo]
name = "<repo-name>"
base_branch = "origin/main"
worktree_root = "{{agentflow_state_home}}/worktrees/{{repo_id}}"
branch_prefix = "af"
default_surface = "default"

[bootstrap]
commands = ["<install command>"]
env_files = [
  { from = ".env.example", to = ".env.local" },
]

[env]
sync_files = [
  { from = ".env", to = ".env" },
]
targets = [
  { path = ".env.agentflow" },
]

[[ports.bindings]]
target = ".env.agentflow"
key = "PORT"
start = 4101
end = 4199

[delivery]
remote = "origin"
sync_strategy = "rebase"
preflight = ["review", "verify"]
cleanup = "async"

[github]
enabled = true
draft_on_submit = true
merge_method = "auto"
auto_merge = true
delete_remote_branch = true
labels = ["agentflow"]
reviewers = []

[linear]
api_key_env = "LINEAR_API_KEY"
credential_profile = "<profile>"
picker_scope = "assigned"
issue_sort = "state_then_updated"

[commands]
review = "<repo-wide review command>"
verify_quick = "<fast default verification>"
verify_default = "<default surface verification>"
verify_web = "<web verification>"
verify_api = "<api verification>"
verify_cli = "<cli verification>"

[agents.default]
runner = "codex"
command = "codex --no-alt-screen -s workspace-write -a on-request"
prime_prompt = "Read AGENTS.md and repo instructions, inspect the task context and relevant files, identify the likely verification path for the current surface, send a short status update with your plan, then wait for confirmation before editing."
resume_prompt = "Resume the current task, re-check AGENTS.md and local instructions if needed, inspect the current state and recent changes, send a short status update with your next-step plan, then wait for confirmation before editing."

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
binaries = ["git", "tmux", "codex", "gh"]
mcp_servers = ["linear"]
```

## Agent Instructions Snippet

This snippet works well in `AGENTS.md`:

```md
## Agentflow

Use the repo's `.agentflow/config.toml` workflow.

Rules:

- One task = one branch = one worktree = one tmux session.
- Prefer `agentflow up <issue-key>` to start work.
- Run `agentflow sync <task>` before resuming older work.
- Run `agentflow verify <task>` before `agentflow submit`.
- Use `agentflow submit <task> --draft` for early async review.
- Use `agentflow land <task> --watch` for final landing.
- Use `agentflow gc` to clean merged work.
- A task surface is a repo-defined label used mainly to choose `verify_<surface>`.
- Avoid stacked task branches unless explicitly requested.
```

## Included Examples

See the `examples/` directory next to this guide for complete sample configs:

- `minimal-go.toml`
- `js-monorepo.toml`
- `multi-surface-app.toml`
- `team-linear.toml`

## Opinionated Defaults

If you want one standard for most repos, use:

- `base_branch = "origin/main"`
- `branch_prefix = "af"`
- `default_surface = "default"`
- `sync_strategy = "rebase"`
- `preflight = ["review", "verify"]`
- `cleanup = "async"`
- `github.merge_method = "auto"`
- `github.auto_merge = true`
- `linear.picker_scope = "assigned"`
- `linear.issue_sort = "state_then_updated"`
