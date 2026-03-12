package agentflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func seedEnvFiles(worktree string, mappings []EnvFileMapping) error {
	for _, mapping := range mappings {
		if mapping.From == "" || mapping.To == "" {
			return fmt.Errorf("bootstrap env_files entries must include from and to")
		}
		source := filepath.Join(worktree, mapping.From)
		target := filepath.Join(worktree, mapping.To)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read env template %s: %w", source, err)
		}
		if err := ensureDir(filepath.Dir(target)); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write env target %s: %w", target, err)
		}
	}
	return nil
}

func writeManagedEnvFile(worktree string, relativePath string, variables map[string]string) (string, error) {
	target := filepath.Join(worktree, relativePath)
	if err := ensureDir(filepath.Dir(target)); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString("# Managed by agentflow. Manual edits may be overwritten.\n")
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(variables[key])
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(target, []byte(builder.String()), 0o644); err != nil {
		return "", err
	}
	return target, nil
}
