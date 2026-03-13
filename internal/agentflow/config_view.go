package agentflow

import (
	"context"
	"fmt"
	"os"
)

func (a *App) ConfigOverview(ctx context.Context, repoArg string) (ConfigOverview, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return ConfigOverview{}, err
	}
	if err := rejectLegacyManifest(repoRoot); err != nil {
		return ConfigOverview{}, err
	}
	path := ResolvedConfigPath(repoRoot)
	return ConfigOverview{
		Repo: ConfigFileInfo{
			Path:   path,
			Exists: fileExists(path),
		},
	}, nil
}

func (a *App) ShowConfig(ctx context.Context, repoArg string) (string, string, bool, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return "", "", false, err
	}
	if err := rejectLegacyManifest(repoRoot); err != nil {
		return "", "", false, err
	}
	path := ResolvedConfigPath(repoRoot)
	content, exists, err := ReadConfigFile(path)
	return path, content, exists, err
}

func (a *App) WriteConfig(ctx context.Context, repoArg string, force bool) (string, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return "", err
	}
	if err := rejectLegacyManifest(repoRoot); err != nil {
		return "", err
	}
	return WriteConfig(repoRoot, force)
}

func (a *App) ShowEffectiveConfig(ctx context.Context, repoArg string, format string) (string, error) {
	runtime, err := a.loadRuntime(ctx, repoArg)
	if err != nil {
		return "", err
	}
	return RenderEffectiveConfig(runtime.EffectiveConfig, format)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func rejectLegacyManifest(repoRoot string) error {
	legacyManifestPath := ResolvedLegacyManifestPath(repoRoot)
	if _, err := os.Stat(legacyManifestPath); err == nil {
		return fmt.Errorf("legacy repo manifest found at %s; merge it into %s and remove the manifest file", legacyManifestPath, ResolvedConfigPath(repoRoot))
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
