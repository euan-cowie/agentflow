package agentflow

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStoreSaveLoadAndList(t *testing.T) {
	t.Parallel()

	store := NewStateStore(t.TempDir())
	now := time.Now().UTC()
	state := TaskState{
		TaskID:      "abc123",
		TaskRef:     TaskRef{Title: "Fix auth"},
		Status:      StatusReady,
		RepoRoot:    "/tmp/repo",
		RepoID:      "repo-1234",
		Branch:      "feature/fix-auth-abc123",
		CreatedAt:   now,
		UpdatedAt:   now,
		TmuxSession: "repo-fix-auth-abc123",
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	loaded, err := store.Load(state.RepoID, state.TaskID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.TaskID != state.TaskID || loaded.Branch != state.Branch {
		t.Fatalf("unexpected loaded state: %+v", loaded)
	}

	states, err := store.List(state.RepoID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected one state, got %d", len(states))
	}
}

func TestTrustStoreCachesByFingerprint(t *testing.T) {
	t.Parallel()

	stateRoot := t.TempDir()
	store := NewTrustStore(stateRoot)
	var output bytes.Buffer
	input := bytes.NewBufferString("yes\n")

	ok, err := store.EnsureTrusted("repo-1234", "/tmp/repo", "/tmp/repo/.agents/workflow.toml", "fingerprint-a", []string{"bun install"}, input, &output)
	if err != nil {
		t.Fatalf("EnsureTrusted returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected repo to become trusted")
	}

	trusted, err := store.IsTrusted("repo-1234", "/tmp/repo", "fingerprint-a")
	if err != nil {
		t.Fatalf("IsTrusted returned error: %v", err)
	}
	if !trusted {
		t.Fatal("expected trusted fingerprint to match")
	}

	trusted, err = store.IsTrusted("repo-1234", "/tmp/repo", "fingerprint-b")
	if err != nil {
		t.Fatalf("IsTrusted returned error: %v", err)
	}
	if trusted {
		t.Fatal("expected trust to invalidate on fingerprint drift")
	}
}

func TestResolveCommandFallsBackToSavedSurfaceThenQuick(t *testing.T) {
	t.Parallel()

	cfg := defaultWorkflowConfig()
	cfg.Commands["verify_web"] = "bun run verify:web"
	cfg.Commands["verify_quick"] = "bun run verify:quick"

	state := TaskState{Surface: "web"}
	command, name, err := resolveCommand(cfg, state, "verify", "")
	if err != nil {
		t.Fatalf("resolveCommand returned error: %v", err)
	}
	if name != "verify_web" || command != "bun run verify:web" {
		t.Fatalf("unexpected verify command resolution: %s %s", name, command)
	}

	delete(cfg.Commands, "verify_web")
	command, name, err = resolveCommand(cfg, state, "verify", "")
	if err != nil {
		t.Fatalf("resolveCommand fallback returned error: %v", err)
	}
	if name != "verify_quick" || command != "bun run verify:quick" {
		t.Fatalf("unexpected verify fallback: %s %s", name, command)
	}
}

func TestStateStoreNewRunLogPathCreatesDirectory(t *testing.T) {
	t.Parallel()

	store := NewStateStore(t.TempDir())
	path, err := store.NewRunLogPath("repo-1234", "task-1234", "verify web", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewRunLogPath returned error: %v", err)
	}
	if filepath.Base(path) != "19700101-000000-verify-web.log" {
		t.Fatalf("unexpected log filename: %q", filepath.Base(path))
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("expected log directory to exist: %v", err)
	}
}

func TestTaskStateEffectiveManagedEnvFilesAndBindingsSupportLegacyAndCurrentShapes(t *testing.T) {
	t.Parallel()

	current := TaskState{
		ManagedEnvFiles: []string{"apps/web/.env.agentflow", "packages/api/.env.agentflow"},
		PortBindings: []PortBindingState{
			{Target: "apps/web/.env.agentflow", Key: "VITE_PORT", Port: 4101},
			{Target: "packages/api/.env.agentflow", Key: "PORT", Port: 5101},
		},
	}
	if len(current.EffectiveManagedEnvFiles()) != 2 {
		t.Fatalf("expected current state to expose both managed env files, got %v", current.EffectiveManagedEnvFiles())
	}
	if len(current.EffectivePortBindings()) != 2 {
		t.Fatalf("expected current state to expose both bindings, got %v", current.EffectivePortBindings())
	}

	legacy := TaskState{
		ManagedEnvFile: ".env.agentflow",
		AllocatedPort:  4101,
		PortKey:        "VITE_PORT",
	}
	files := legacy.EffectiveManagedEnvFiles()
	if len(files) != 1 || files[0] != ".env.agentflow" {
		t.Fatalf("unexpected legacy managed env files: %v", files)
	}
	bindings := legacy.EffectivePortBindings()
	if len(bindings) != 1 || bindings[0].Key != "VITE_PORT" || bindings[0].Port != 4101 {
		t.Fatalf("unexpected legacy bindings: %+v", bindings)
	}
}
