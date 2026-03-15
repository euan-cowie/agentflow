package agentflow

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestResolveLinearCredentialPrefersEnvVar(t *testing.T) {
	app, _, _ := newTestApp(t)
	if err := app.creds.Save(credentialProviderLinear, "stored-token", app.now()); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	if err := app.creds.SaveProfile(credentialProviderLinear, "acme", "profile-token", app.now()); err != nil {
		t.Fatalf("save profile credential: %v", err)
	}
	t.Setenv("LINEAR_API_KEY", "env-token")

	token, status, err := app.resolveLinearCredential(LinearConfig{CredentialProfile: "acme"})
	if err != nil {
		t.Fatalf("resolveLinearCredential returned error: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("expected env token, got %q", token)
	}
	if status.Source != "env:LINEAR_API_KEY" || status.Profile != "acme" {
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

func TestResolveLinearCredentialUsesConfiguredProfile(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	if err := app.creds.SaveProfile(credentialProviderLinear, "acme", "profile-token", app.now()); err != nil {
		t.Fatalf("save profile credential: %v", err)
	}

	token, status, err := app.resolveLinearCredential(LinearConfig{CredentialProfile: "acme"})
	if err != nil {
		t.Fatalf("resolveLinearCredential returned error: %v", err)
	}
	if token != "profile-token" {
		t.Fatalf("expected profile token, got %q", token)
	}
	if status.Source != "stored:profile" || status.Profile != "acme" || !status.Available {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestResolveLinearCredentialConfiguredProfileDoesNotFallBackToLegacy(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	if err := app.creds.Save(credentialProviderLinear, "legacy-token", app.now()); err != nil {
		t.Fatalf("save legacy credential: %v", err)
	}

	_, status, err := app.resolveLinearCredential(LinearConfig{CredentialProfile: "acme"})
	if err == nil {
		t.Fatal("expected missing named profile error")
	}
	if status.Available {
		t.Fatalf("expected unavailable status, got %+v", status)
	}
	if status.Profile != "acme" || status.Source != "missing" {
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

	status, err := app.SaveLinearCredential(context.Background(), "", "stored-token")
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

func TestSaveLinearCredentialProfileValidatesBeforePersisting(t *testing.T) {
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

	status, err := app.SaveLinearCredential(context.Background(), "acme", "profile-token")
	if err != nil {
		t.Fatalf("SaveLinearCredential returned error: %v", err)
	}
	if status.Source != "stored:profile" || status.Profile != "acme" {
		t.Fatalf("unexpected status: %+v", status)
	}
	record, err := app.creds.LoadProfile(credentialProviderLinear, "acme")
	if err != nil {
		t.Fatalf("load credential profile: %v", err)
	}
	if record.Token != "profile-token" {
		t.Fatalf("expected stored profile token, got %+v", record)
	}
}

func TestSaveLinearCredentialRejectsInvalidProfileName(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	app.linear = NewLinearOps(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("expected invalid profile name to fail before calling Linear")
			return nil, nil
		}),
	})

	_, err := app.SaveLinearCredential(context.Background(), "../acme", "profile-token")
	if err == nil {
		t.Fatal("expected invalid profile name error")
	}
	if !strings.Contains(err.Error(), "invalid credential profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListLinearCredentialsIncludesLegacyAndProfiles(t *testing.T) {
	t.Parallel()

	app, _, _ := newTestApp(t)
	if err := app.creds.Save(credentialProviderLinear, "legacy-token", app.now()); err != nil {
		t.Fatalf("save legacy credential: %v", err)
	}
	if err := app.creds.SaveProfile(credentialProviderLinear, "beta", "beta-token", app.now()); err != nil {
		t.Fatalf("save beta profile: %v", err)
	}
	if err := app.creds.SaveProfile(credentialProviderLinear, "acme", "acme-token", app.now()); err != nil {
		t.Fatalf("save acme profile: %v", err)
	}

	credentials, err := app.ListLinearCredentials()
	if err != nil {
		t.Fatalf("ListLinearCredentials returned error: %v", err)
	}
	if len(credentials) != 3 {
		t.Fatalf("expected 3 credentials, got %+v", credentials)
	}
	if !credentials[0].Legacy {
		t.Fatalf("expected legacy credential first, got %+v", credentials)
	}
	if credentials[1].Profile != "acme" || credentials[2].Profile != "beta" {
		t.Fatalf("expected named profiles sorted by name, got %+v", credentials)
	}
}

func TestLinearCredentialStatusLoadsRepoConfigFromCurrentDirectory(t *testing.T) {
	repo := initCommittedRepo(t)
	writeTestRepoConfig(t, repo, testRepoWorkflowConfig+`

[linear]
api_key_env = "CUSTOM_LINEAR_TOKEN"
credential_profile = "acme"
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
	if status.Profile != "acme" {
		t.Fatalf("expected repo-configured profile, got %+v", status)
	}
	if !status.Available {
		t.Fatalf("expected credential to be available, got %+v", status)
	}
}
