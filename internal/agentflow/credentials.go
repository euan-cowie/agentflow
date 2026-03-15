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
	"sort"
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

type StoredCredentialInfo struct {
	Provider  string
	Profile   string
	Legacy    bool
	UpdatedAt time.Time
}

type CredentialStatus struct {
	Provider  string
	Source    string
	Available bool
	UpdatedAt time.Time
	Profile   string
}

func NewCredentialStore(stateRoot string) *CredentialStore {
	return &CredentialStore{root: filepath.Join(stateRoot, "credentials")}
}

func (s *CredentialStore) path(provider string) string {
	return filepath.Join(s.root, provider+".json")
}

func (s *CredentialStore) profileDir(provider string) string {
	return filepath.Join(s.root, provider)
}

func (s *CredentialStore) profilePath(provider, profile string) (string, error) {
	profile = normalizeCredentialProfile(profile)
	if err := validateCredentialProfileName(profile); err != nil {
		return "", err
	}
	return filepath.Join(s.profileDir(provider), profile+".json"), nil
}

func (s *CredentialStore) Load(provider string) (StoredCredential, error) {
	return loadStoredCredential(s.path(provider))
}

func (s *CredentialStore) LoadProfile(provider, profile string) (StoredCredential, error) {
	path, err := s.profilePath(provider, profile)
	if err != nil {
		return StoredCredential{}, err
	}
	return loadStoredCredential(path)
}

func loadStoredCredential(path string) (StoredCredential, error) {
	data, err := os.ReadFile(path)
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
	return saveStoredCredential(s.path(provider), token, now)
}

func (s *CredentialStore) SaveProfile(provider, profile, token string, now time.Time) error {
	path, err := s.profilePath(provider, profile)
	if err != nil {
		return err
	}
	return saveStoredCredential(path, token, now)
}

func saveStoredCredential(path, token string, now time.Time) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("credential token must not be empty")
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
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
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (s *CredentialStore) Delete(provider string) error {
	return deleteStoredCredential(s.path(provider))
}

func (s *CredentialStore) DeleteProfile(provider, profile string) error {
	path, err := s.profilePath(provider, profile)
	if err != nil {
		return err
	}
	return deleteStoredCredential(path)
}

func deleteStoredCredential(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *CredentialStore) ListProfiles(provider string) ([]StoredCredentialInfo, error) {
	dir := s.profileDir(provider)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	profiles := make([]StoredCredentialInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		profile := strings.TrimSuffix(entry.Name(), ".json")
		if err := validateCredentialProfileName(profile); err != nil {
			return nil, fmt.Errorf("invalid stored credential profile %q: %w", profile, err)
		}
		record, err := loadStoredCredential(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, StoredCredentialInfo{
			Provider:  provider,
			Profile:   profile,
			UpdatedAt: record.UpdatedAt,
		})
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Profile < profiles[j].Profile
	})
	return profiles, nil
}

func (a *App) resolveLinearCredential(cfg LinearConfig) (string, CredentialStatus, error) {
	envName := effectiveLinearAPIKeyEnv(cfg)
	profile := effectiveLinearCredentialProfile(cfg)
	if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
		return token, CredentialStatus{
			Provider:  credentialProviderLinear,
			Source:    "env:" + envName,
			Available: true,
			Profile:   profile,
		}, nil
	}

	if profile != "" {
		record, err := a.creds.LoadProfile(credentialProviderLinear, profile)
		if err == nil {
			return strings.TrimSpace(record.Token), CredentialStatus{
				Provider:  credentialProviderLinear,
				Source:    "stored:profile",
				Available: strings.TrimSpace(record.Token) != "",
				UpdatedAt: record.UpdatedAt,
				Profile:   profile,
			}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", CredentialStatus{}, err
		}
		return "", CredentialStatus{
			Provider:  credentialProviderLinear,
			Source:    "missing",
			Available: false,
			Profile:   profile,
		}, fmt.Errorf("Linear API key missing for profile %q: set %s or run `agentflow auth linear login --profile %s`", profile, envName, profile)
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

func (a *App) SaveLinearCredential(ctx context.Context, profile, token string) (CredentialStatus, error) {
	profile = normalizeCredentialProfile(profile)
	if profile != "" {
		if err := validateCredentialProfileName(profile); err != nil {
			return CredentialStatus{}, fmt.Errorf("invalid credential profile %q: %w", profile, err)
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		var err error
		prompt := "Linear API key: "
		if profile != "" {
			prompt = fmt.Sprintf("Linear API key for profile %q: ", profile)
		}
		token, err = readSecretPrompt(a.stdin, a.stdout, prompt)
		if err != nil {
			return CredentialStatus{}, err
		}
	}

	if err := a.linear.Viewer(ctx, token); err != nil {
		return CredentialStatus{}, err
	}
	if profile != "" {
		if err := a.creds.SaveProfile(credentialProviderLinear, profile, token, a.now()); err != nil {
			return CredentialStatus{}, err
		}
		return CredentialStatus{
			Provider:  credentialProviderLinear,
			Source:    "stored:profile",
			Available: true,
			UpdatedAt: a.now(),
			Profile:   profile,
		}, nil
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

func (a *App) DeleteLinearCredential(profile string) error {
	profile = normalizeCredentialProfile(profile)
	if profile != "" {
		if err := validateCredentialProfileName(profile); err != nil {
			return fmt.Errorf("invalid credential profile %q: %w", profile, err)
		}
		return a.creds.DeleteProfile(credentialProviderLinear, profile)
	}
	return a.creds.Delete(credentialProviderLinear)
}

func (a *App) ListLinearCredentials() ([]StoredCredentialInfo, error) {
	credentials := make([]StoredCredentialInfo, 0)
	record, err := a.creds.Load(credentialProviderLinear)
	if err == nil {
		credentials = append(credentials, StoredCredentialInfo{
			Provider:  credentialProviderLinear,
			Legacy:    true,
			UpdatedAt: record.UpdatedAt,
		})
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	profiles, err := a.creds.ListProfiles(credentialProviderLinear)
	if err != nil {
		return nil, err
	}
	credentials = append(credentials, profiles...)
	return credentials, nil
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

func describeCredentialStatus(status CredentialStatus) string {
	if status.Profile != "" {
		return fmt.Sprintf("profile=%s source=%s", status.Profile, status.Source)
	}
	return status.Source
}

func normalizeCredentialProfile(profile string) string {
	return strings.TrimSpace(profile)
}

func validateCredentialProfileName(profile string) error {
	profile = normalizeCredentialProfile(profile)
	if profile == "" {
		return errors.New("must not be empty")
	}
	for _, r := range profile {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == '_' {
			continue
		}
		return errors.New("must contain only ASCII letters, digits, dot, dash, or underscore")
	}
	return nil
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
