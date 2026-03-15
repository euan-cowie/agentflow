package agentflow

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	credentialProviderLinear = "linear"
)

type CredentialStore struct {
	root string
}

type StoredCredential struct {
	Token     string    `json:"token"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CredentialStatus struct {
	Provider  string
	Source    string
	Available bool
	UpdatedAt time.Time
}

func NewCredentialStore(stateRoot string) *CredentialStore {
	return &CredentialStore{root: filepath.Join(stateRoot, "credentials")}
}

func (s *CredentialStore) path(provider string) string {
	return filepath.Join(s.root, provider+".json")
}

func (s *CredentialStore) Load(provider string) (StoredCredential, error) {
	data, err := os.ReadFile(s.path(provider))
	if err != nil {
		return StoredCredential{}, err
	}
	var record StoredCredential
	if err := json.Unmarshal(data, &record); err != nil {
		return StoredCredential{}, fmt.Errorf("decode credential: %w", err)
	}
	return record, nil
}

func (s *CredentialStore) Save(provider, token string, now time.Time) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("credential token must not be empty")
	}
	if err := ensureDir(s.root); err != nil {
		return err
	}
	record := StoredCredential{
		Token:     token,
		UpdatedAt: now.UTC(),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credential: %w", err)
	}
	return os.WriteFile(s.path(provider), append(data, '\n'), 0o600)
}

func (s *CredentialStore) Delete(provider string) error {
	err := os.Remove(s.path(provider))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (a *App) resolveLinearCredential(cfg LinearConfig) (string, CredentialStatus, error) {
	envName := effectiveLinearAPIKeyEnv(cfg)
	if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
		return token, CredentialStatus{
			Provider:  credentialProviderLinear,
			Source:    "env:" + envName,
			Available: true,
		}, nil
	}

	record, err := a.creds.Load(credentialProviderLinear)
	if err == nil {
		return strings.TrimSpace(record.Token), CredentialStatus{
			Provider:  credentialProviderLinear,
			Source:    "stored",
			Available: strings.TrimSpace(record.Token) != "",
			UpdatedAt: record.UpdatedAt,
		}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", CredentialStatus{}, err
	}

	return "", CredentialStatus{
		Provider:  credentialProviderLinear,
		Source:    "missing",
		Available: false,
	}, fmt.Errorf("Linear API key missing: set %s or run `agentflow auth linear login`", envName)
}

func (a *App) SaveLinearCredential(ctx context.Context, token string) (CredentialStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		var err error
		token, err = readSecretPrompt(a.stdin, a.stdout, "Linear API key: ")
		if err != nil {
			return CredentialStatus{}, err
		}
	}

	if err := a.linear.Viewer(ctx, token); err != nil {
		return CredentialStatus{}, err
	}
	if err := a.creds.Save(credentialProviderLinear, token, a.now()); err != nil {
		return CredentialStatus{}, err
	}
	return CredentialStatus{
		Provider:  credentialProviderLinear,
		Source:    "stored",
		Available: true,
		UpdatedAt: a.now(),
	}, nil
}

func (a *App) DeleteLinearCredential() error {
	return a.creds.Delete(credentialProviderLinear)
}

func (a *App) LinearCredentialStatus(ctx context.Context, repoArg string) (CredentialStatus, error) {
	cfg := LinearConfig{}
	runtime, err := a.loadRuntime(ctx, repoArg)
	if err == nil {
		cfg = runtime.EffectiveConfig.Linear
	} else if strings.TrimSpace(repoArg) != "" {
		return CredentialStatus{}, err
	}
	_, status, err := a.resolveLinearCredential(cfg)
	if err != nil && !status.Available {
		return status, nil
	}
	return status, err
}

func readSecretPrompt(input io.Reader, output io.Writer, prompt string) (string, error) {
	if _, err := io.WriteString(output, prompt); err != nil {
		return "", err
	}
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		value, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := io.WriteString(output, "\n"); writeErr != nil && err == nil {
			err = writeErr
		}
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(value))
		if token == "" {
			return "", errors.New("credential token must not be empty")
		}
		return token, nil
	}

	reader := bufio.NewReader(input)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	token := strings.TrimSpace(value)
	if token == "" {
		return "", errors.New("credential token must not be empty")
	}
	return token, nil
}
