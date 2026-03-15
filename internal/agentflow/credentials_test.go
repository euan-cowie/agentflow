package agentflow

import (
	"context"
	"net/http"
	"testing"
)

func TestResolveLinearCredentialPrefersEnvVar(t *testing.T) {
	app, _, _ := newTestApp(t)
	if err := app.creds.Save(credentialProviderLinear, "stored-token", app.now()); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	t.Setenv("LINEAR_API_KEY", "env-token")

	token, status, err := app.resolveLinearCredential(LinearConfig{})
	if err != nil {
		t.Fatalf("resolveLinearCredential returned error: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("expected env token, got %q", token)
	}
	if status.Source != "env:LINEAR_API_KEY" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestResolveLinearCredentialFallsBackToStoredToken(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	if err := app.creds.Save(credentialProviderLinear, "stored-token", app.now()); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	token, status, err := app.resolveLinearCredential(LinearConfig{})
	if err != nil {
		t.Fatalf("resolveLinearCredential returned error: %v", err)
	}
	if token != "stored-token" {
		t.Fatalf("expected stored token, got %q", token)
	}
	if status.Source != "stored" || !status.Available {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestSaveLinearCredentialValidatesBeforePersisting(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	app.linear = NewLinearOps(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{"id": "viewer-1"},
				},
			}), nil
		}),
	})
	app.linear.endpoint = "https://example.invalid/graphql"

	status, err := app.SaveLinearCredential(context.Background(), "stored-token")
	if err != nil {
		t.Fatalf("SaveLinearCredential returned error: %v", err)
	}
	if status.Source != "stored" {
		t.Fatalf("unexpected status: %+v", status)
	}
	record, err := app.creds.Load(credentialProviderLinear)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if record.Token != "stored-token" {
		t.Fatalf("expected stored token, got %+v", record)
	}
}

func TestLinearCredentialStatusLoadsRepoConfigFromCurrentDirectory(t *testing.T) {
	repo := initCommittedRepo(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "CUSTOM_LINEAR_TOKEN"
`)
	t.Chdir(repo)
	app, _, _ := newTestApp(t)
	t.Setenv("CUSTOM_LINEAR_TOKEN", "repo-token")

	status, err := app.LinearCredentialStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("LinearCredentialStatus returned error: %v", err)
	}
	if status.Source != "env:CUSTOM_LINEAR_TOKEN" {
		t.Fatalf("expected repo-configured credential source, got %+v", status)
	}
	if !status.Available {
		t.Fatalf("expected credential to be available, got %+v", status)
	}
}
