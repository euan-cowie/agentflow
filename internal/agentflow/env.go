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

func writeManagedEnvFiles(worktree string, targetPaths []string, valuesByTarget map[string]map[string]string) ([]string, error) {
	written := make([]string, 0, len(targetPaths))
	for _, target := range uniqueStrings(targetPaths) {
		vars := valuesByTarget[target]
		if vars == nil {
			vars = map[string]string{}
		}
		path, err := writeManagedEnvFile(worktree, target, vars)
		if err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

func portBindingValues(bindings []PortBindingState) map[string]map[string]string {
	values := make(map[string]map[string]string)
	for _, binding := range bindings {
		if _, ok := values[binding.Target]; !ok {
			values[binding.Target] = map[string]string{}
		}
		values[binding.Target][binding.Key] = fmt.Sprintf("%d", binding.Port)
	}
	return values
}

func buildTaskEnvState(cfg WorkflowConfig) ([]string, []PortBindingState, error) {
	targets, err := effectiveManagedEnvFiles(cfg)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := effectivePortBindings(cfg)
	if err != nil {
		return nil, nil, err
	}
	reserved := map[int]struct{}{}
	stateBindings := make([]PortBindingState, 0, len(bindings))
	for _, binding := range bindings {
		port, err := preferredPortAllocator(binding.Start, binding.End, reserved)
		if err != nil {
			return nil, nil, err
		}
		reserved[port] = struct{}{}
		stateBindings = append(stateBindings, PortBindingState{
			Target: binding.Target,
			Key:    binding.Key,
			Port:   port,
		})
	}
	return targets, stateBindings, nil
}
