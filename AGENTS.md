# Agentflow Repo Instructions

- When changing the CLI surface or config schema, read and follow [.agentflow/skills/cli-doc-sync/SKILL.md](/Users/euan-cowie/Projects/agentflow/.agentflow/skills/cli-doc-sync/SKILL.md).
- Treat these paths as CLI/docs-sync triggers:
  - `cmd/agentflow/**`
  - `internal/cli/**`
  - `internal/agentflow/config*.go`
  - `internal/agentflow/model.go`
  - `.agentflow/config.toml`
- Keep [README.md](/Users/euan-cowie/Projects/agentflow/README.md), [docs/config.md](/Users/euan-cowie/Projects/agentflow/docs/config.md), and `internal/cli/testdata/*.golden` in sync with the current interface.
