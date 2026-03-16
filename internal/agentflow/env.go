package agentflow

import (
	"errors"
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
		source, err := resolveManagedPath(worktree, mapping.From, fmt.Sprintf("bootstrap env source %q", mapping.From), "worktree")
		if err != nil {
			return err
		}
		target, err := resolveManagedPath(worktree, mapping.To, fmt.Sprintf("bootstrap env target %q", mapping.To), "worktree")
		if err != nil {
			return err
		}
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
	target, err := resolveManagedPath(worktree, relativePath, fmt.Sprintf("managed env target %q", relativePath), "worktree")
	if err != nil {
		return "", err
	}
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

func syncEnvFiles(repoRoot string, worktree string, mappings []EnvFileMapping) ([]string, error) {
	written := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		if mapping.From == "" || mapping.To == "" {
			return nil, fmt.Errorf("env sync files must include from and to")
		}
		source, err := resolveManagedPath(repoRoot, mapping.From, fmt.Sprintf("env sync source %q", mapping.From), "repo root")
		if err != nil {
			return nil, err
		}
		target, err := resolveManagedPath(worktree, mapping.To, fmt.Sprintf("env sync target %q", mapping.To), "worktree")
		if err != nil {
			return nil, err
		}
		sourceInfo, err := os.Stat(source)
		if err != nil {
			return nil, fmt.Errorf("stat env sync source %s: %w", source, err)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read env sync source %s: %w", source, err)
		}
		if err := ensureDir(filepath.Dir(target)); err != nil {
			return nil, err
		}
		mode := sourceInfo.Mode().Perm()
		if err := writeFileAtomically(target, data, mode); err != nil {
			return nil, fmt.Errorf("write env sync target %s: %w", target, err)
		}
		written = append(written, target)
	}
	return written, nil
}

func syncGeneratedEnvFiles(worktree string, targetPaths []string, bindings []PortBindingState) error {
	_, err := writeManagedEnvFiles(worktree, targetPaths, portBindingValues(bindings))
	return err
}

func prepareManagedEnvFiles(repoRoot string, worktree string, synced []EnvFileMapping, bootstrap []EnvFileMapping, generated []string, bindings []PortBindingState) error {
	if _, err := syncEnvFiles(repoRoot, worktree, synced); err != nil {
		return err
	}
	if err := seedEnvFiles(worktree, bootstrap); err != nil {
		return err
	}
	return syncGeneratedEnvFiles(worktree, generated, bindings)
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

func removeManagedEnvFiles(worktree string, targetPaths []string) error {
	for _, target := range uniqueStrings(targetPaths) {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		path, err := resolveManagedPath(worktree, target, fmt.Sprintf("managed env target %q", target), "worktree")
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove managed env target %q: %w", target, err)
		}
	}
	return nil
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

func buildTaskEnvState(cfg EffectiveConfig) ([]string, []EnvFileMapping, []PortBindingState, error) {
	targets, err := effectiveGeneratedEnvFiles(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	syncedFiles, err := effectiveSyncedEnvFiles(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	bindings, err := effectivePortBindings(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	reserved := map[int]struct{}{}
	stateBindings := make([]PortBindingState, 0, len(bindings))
	for _, binding := range bindings {
		port, err := preferredPortAllocator(binding.Start, binding.End, reserved)
		if err != nil {
			return nil, nil, nil, err
		}
		reserved[port] = struct{}{}
		stateBindings = append(stateBindings, PortBindingState{
			Target: binding.Target,
			Key:    binding.Key,
			Port:   port,
		})
	}
	return targets, syncedFiles, stateBindings, nil
}

func writeFileAtomically(target string, data []byte, mode os.FileMode) (err error) {
	tempFile, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := tempFile.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}
		if err != nil {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err = tempFile.Write(data); err != nil {
		return err
	}
	if err = tempFile.Chmod(mode); err != nil {
		return err
	}
	err = tempFile.Close()
	closed = true
	if err != nil {
		return err
	}
	if err = os.Rename(tempPath, target); err != nil {
		return err
	}
	return nil
}

func resolveManagedPath(root string, relativePath string, label string, rootName string) (string, error) {
	root = filepath.Clean(root)
	resolved := filepath.Clean(filepath.Join(root, relativePath))
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s escapes %s", label, rootName)
	}
	canonicalRoot := canonicalPath(root)
	canonicalResolved, err := canonicalManagedPath(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	rel, err = filepath.Rel(canonicalRoot, canonicalResolved)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s escapes %s", label, rootName)
	}
	return resolved, nil
}

func canonicalManagedPath(path string) (string, error) {
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	missingParts := []string{}
	current := path
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missingParts = append([]string{filepath.Base(current)}, missingParts...)
		current = parent

		resolvedParent, parentErr := filepath.EvalSymlinks(current)
		if parentErr == nil {
			canonical := resolvedParent
			for _, part := range missingParts {
				canonical = filepath.Join(canonical, part)
			}
			return filepath.Clean(canonical), nil
		}
		if !errors.Is(parentErr, os.ErrNotExist) {
			return "", parentErr
		}
	}
}
