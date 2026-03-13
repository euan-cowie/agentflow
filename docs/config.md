# Repo Config Reference

`agentflow` uses one checked-in repo config file: [.agentflow/config.toml](/Users/euan-cowie/Projects/agentflow/.agentflow/config.toml).

This file serves two roles:

- repo identity and defaults
- runnable workflow definition for worktrees, tmux, env files, verification, and agents

If the file is missing, agentflow falls back to built-in defaults.

## Sections

### `[repo]`

Repo identity and task defaults.

Fields:

- `name`: human-friendly repo name used in tmux/session naming
- `base_branch`: branch or ref used as the default worktree base
- `worktree_root`: where agentflow creates task worktrees
- `branch_prefix`: prefix for generated task branches
- `default_surface`: default surface used by `verify` command resolution

### `[bootstrap]`

One-time setup that runs when a new task worktree is created.

Fields:

- `commands`: shell commands run in order after worktree creation
- `env_files`: copy-once env seeding from `{ from, to }`

Use this for install/bootstrap steps, not recurring dev commands.

### `[env]`

Agentflow-managed env files.

Fields:

- `targets`: list of `{ path = "..." }`

These files are owned by agentflow. It may create or rewrite them while preparing a task, and it removes them during teardown.

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

### `[agents]`

Agent profiles used by tmux windows.

Common fields:

- `runner`
- `command`
- `prime_prompt`
- `resume_prompt`

Today `runner` is effectively Codex-only.

### `[tmux]`

Session naming and window layout.

Fields:

- `session_name`
- `[[tmux.windows]]`
  - `name`
  - `command` or `agent`

V1 supports at most one agent-backed window.

### `[requirements]`

Repo requirements checked by `agentflow doctor`.

Fields:

- `binaries`
- `mcp_servers`

These affect doctor output but do not change trust or config drift behavior.

## Trust And Drift

Agentflow computes a workflow fingerprint from these sections:

- `bootstrap`
- `env`
- `ports`
- `commands`
- `agents`
- `tmux`

Changes to those sections:

- invalidate repo trust
- show up as config drift on existing tasks

Agentflow does **not** include these in the workflow fingerprint:

- `repo`
- `requirements`

So changing `base_branch`, `default_surface`, or doctor-only requirements does not trigger a new trust prompt by itself.

## Effective Config

Use:

```sh
agentflow config effective
```

This prints the merged runtime config after built-in defaults and repo config are applied. It is useful for checking what agentflow will actually use, especially when some sections are omitted from `.agentflow/config.toml`.

## Current Repo Example

The current repo config is:

```toml
[repo]
name = "agentflow"
base_branch = "main"
default_surface = "cli"

[env]
targets = [{ path = ".env.agentflow" }]

[commands]
review = "go test ./..."
verify_quick = "go test ./..."
verify_cli = "go test ./..."

[requirements]
binaries = ["git", "tmux", "codex", "nvim", "go"]
```
