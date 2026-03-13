---
name: cli-doc-sync
description: Keep agentflow CLI and config documentation in sync with the current interface. Use when changing command names, flags, help output, config layout, config semantics, or any user-facing CLI/config behavior under cmd/agentflow, internal/cli, internal/agentflow/config*.go, internal/agentflow/model.go, or .agentflow/config.toml.
---

# CLI Doc Sync

## Overview

Keep the CLI interface, config docs, and help snapshots aligned whenever agentflow's user-facing behavior changes.

## Workflow

1. Identify whether the change affects any of:
   - top-level commands or subcommands
   - flags or help text
   - repo config layout or semantics
   - effective config rendering
   - trust/config drift wording
2. Update the user docs:
   - edit `README.md` for command lists, examples, and high-level behavior
   - edit `docs/config.md` for config layout, section meanings, and trust/drift semantics
3. Update CLI help snapshots if the interface changed:
   - run `UPDATE_GOLDEN=1 GOCACHE=/tmp/agentflow-go-cache go test ./internal/cli`
4. Re-run the full verification pass:
   - `GOCACHE=/tmp/agentflow-go-cache go test ./...`
   - `GOCACHE=/tmp/agentflow-go-cache go build ./cmd/agentflow`
5. Spot-check the live output when the change is user-visible:
   - `GOCACHE=/tmp/agentflow-go-cache go run ./cmd/agentflow --help`
   - `GOCACHE=/tmp/agentflow-go-cache go run ./cmd/agentflow config --help`
   - `GOCACHE=/tmp/agentflow-go-cache go run ./cmd/agentflow config effective`

## What To Update

- Update `README.md` when:
  - commands or flags changed
  - examples changed
  - workflow/trust wording changed
- Update `docs/config.md` when:
  - config sections changed
  - a section gained new semantics
  - trust or config drift rules changed
  - defaults changed in a way users need to know
- Update `internal/cli/testdata/*.golden` when:
  - help output changed
  - command organization changed

## Guardrails

- Do not leave the README command list out of sync with Cobra help output.
- Do not update golden files without checking whether the change is intended.
- Prefer documenting the current interface over preserving old wording.
- If config behavior changed, explain both the shape and the purpose of each section in `docs/config.md`.
