package agentflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofrs/flock"
)

type StateStore struct {
	root string
}

func NewStateStore(root string) *StateStore {
	return &StateStore{root: root}
}

func (s *StateStore) taskDir(repoID string) string {
	return filepath.Join(s.root, "tasks", repoID)
}

func (s *StateStore) taskPath(repoID, taskID string) string {
	return filepath.Join(s.taskDir(repoID), taskID+".json")
}

func (s *StateStore) runDir(repoID, taskID string) string {
	return filepath.Join(s.root, "runs", repoID, taskID)
}

func (s *StateStore) ensureRoot() error {
	return ensureDir(s.root)
}

func (s *StateStore) Save(state TaskState) error {
	if err := ensureDir(s.taskDir(state.RepoID)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task state: %w", err)
	}
	return os.WriteFile(s.taskPath(state.RepoID, state.TaskID), append(data, '\n'), 0o644)
}

func (s *StateStore) Load(repoID, taskID string) (TaskState, error) {
	data, err := os.ReadFile(s.taskPath(repoID, taskID))
	if err != nil {
		return TaskState{}, err
	}
	var state TaskState
	if err := json.Unmarshal(data, &state); err != nil {
		return TaskState{}, fmt.Errorf("decode task state: %w", err)
	}
	return state, nil
}

func (s *StateStore) Delete(repoID, taskID string) error {
	err := os.Remove(s.taskPath(repoID, taskID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *StateStore) List(repoID string) ([]TaskState, error) {
	base := filepath.Join(s.root, "tasks")
	var dirs []string
	if repoID != "" {
		dirs = []string{filepath.Join(base, repoID)}
	} else {
		entries, err := os.ReadDir(base)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, filepath.Join(base, entry.Name()))
			}
		}
	}

	var states []TaskState
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			var state TaskState
			if err := json.Unmarshal(data, &state); err != nil {
				return nil, err
			}
			states = append(states, state)
		}
	}

	sort.Slice(states, func(i, j int) bool {
		return states[i].UpdatedAt.After(states[j].UpdatedAt)
	})
	return states, nil
}

func (s *StateStore) NewRunLogPath(repoID, taskID, name string, now time.Time) (string, error) {
	dir := s.runDir(repoID, taskID)
	if err := ensureDir(dir); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s-%s.log", now.UTC().Format("20060102-150405"), slugify(name))
	return filepath.Join(dir, filename), nil
}

func (s *StateStore) RepoLock(repoID string) (*flock.Flock, error) {
	lockDir := filepath.Join(s.root, "locks")
	if err := ensureDir(lockDir); err != nil {
		return nil, err
	}
	return flock.New(filepath.Join(lockDir, repoID+".lock")), nil
}
