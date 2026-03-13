package agentflow

import (
	"context"
	"os"
)

func (a *App) ConfigOverview(ctx context.Context, repoArg string) (ConfigOverview, error) {
	globalPath, err := ResolvedGlobalConfigPath(a.configPath)
	if err != nil {
		return ConfigOverview{}, err
	}
	globalExists := fileExists(globalPath)
	overview := ConfigOverview{
		Global: ConfigFileInfo{
			Path:   globalPath,
			Exists: globalExists,
		},
	}

	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		if repoArg != "" {
			return ConfigOverview{}, err
		}
		return overview, nil
	}
	repoPath := ResolvedRepoConfigPath(repoRoot)
	manifestPath := ResolvedManifestPath(repoRoot)
	overview.Repo = &ConfigFileInfo{
		Path:   repoPath,
		Exists: fileExists(repoPath),
	}
	overview.Manifest = &ConfigFileInfo{
		Path:   manifestPath,
		Exists: fileExists(manifestPath),
	}
	return overview, nil
}

func (a *App) ShowGlobalConfig() (string, string, bool, error) {
	path, err := ResolvedGlobalConfigPath(a.configPath)
	if err != nil {
		return "", "", false, err
	}
	content, exists, err := ReadConfigFile(path)
	return path, content, exists, err
}

func (a *App) WriteGlobalConfig(force bool) (string, error) {
	return WriteGlobalConfig(a.configPath, force)
}

func (a *App) ShowRepoConfig(ctx context.Context, repoArg string) (string, string, bool, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return "", "", false, err
	}
	path := ResolvedRepoConfigPath(repoRoot)
	content, exists, err := ReadConfigFile(path)
	return path, content, exists, err
}

func (a *App) ShowManifest(ctx context.Context, repoArg string) (string, string, bool, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return "", "", false, err
	}
	path := ResolvedManifestPath(repoRoot)
	content, exists, err := ReadConfigFile(path)
	return path, content, exists, err
}

func (a *App) WriteRepoConfig(ctx context.Context, repoArg string, force bool) (string, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return "", err
	}
	return WriteRepoConfig(repoRoot, force)
}

func (a *App) WriteManifest(ctx context.Context, repoArg string, force bool) (string, error) {
	repoRoot, err := a.resolveRepoRoot(ctx, repoArg)
	if err != nil {
		return "", err
	}
	return WriteManifest(repoRoot, force)
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
