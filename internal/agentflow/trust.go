package agentflow

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TrustStore struct {
	root string
}

func NewTrustStore(stateRoot string) *TrustStore {
	return &TrustStore{root: filepath.Join(stateRoot, "trust")}
}

func (t *TrustStore) path(repoID string) string {
	return filepath.Join(t.root, repoID+".json")
}

func (t *TrustStore) IsTrusted(repoID, repoRoot, fingerprint string) (bool, error) {
	data, err := os.ReadFile(t.path(repoID))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var record TrustRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return false, err
	}
	return record.RepoID == repoID && record.RepoRoot == repoRoot && record.WorkflowFingerprint == fingerprint, nil
}

func (t *TrustStore) Save(record TrustRecord) error {
	if err := ensureDir(t.root); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.path(record.RepoID), append(data, '\n'), 0o644)
}

func (t *TrustStore) EnsureTrusted(repoID, repoRoot, configPath, fingerprint string, entries []string, input io.Reader, output io.Writer) (bool, error) {
	if fingerprint == "" || len(entries) == 0 {
		return true, nil
	}
	trusted, err := t.IsTrusted(repoID, repoRoot, fingerprint)
	if err != nil {
		return false, err
	}
	if trusted {
		return true, nil
	}

	if _, err := fmt.Fprintf(output, "Trust repo workflow for %s?\n", repoRoot); err != nil {
		return false, err
	}
	if _, err := fmt.Fprintf(output, "Repo config: %s\n", configPath); err != nil {
		return false, err
	}
	if _, err := io.WriteString(output, "Repo-defined side effects:\n"); err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(output, "  - %s\n", entry); err != nil {
			return false, err
		}
	}
	if _, err := io.WriteString(output, "Type 'yes' to trust this repo workflow: "); err != nil {
		return false, err
	}
	reader := bufio.NewReader(input)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "yes" && answer != "y" {
		return false, errors.New("repo trust declined")
	}
	record := TrustRecord{
		RepoRoot:            repoRoot,
		RepoID:              repoID,
		WorkflowFingerprint: fingerprint,
		AcceptedAt:          time.Now().UTC(),
	}
	if err := t.Save(record); err != nil {
		return false, err
	}
	return true, nil
}
