package agentflow

import (
	"fmt"
	"strings"
)

func resolveManualTask(repoRoot, input string) (TaskRef, string, error) {
	key := canonicalTaskKey(input)
	if key == "" {
		return TaskRef{}, "", fmt.Errorf("task must not be empty")
	}
	ref := TaskRef{
		Source: taskSourceManual,
		Key:    key,
		Title:  strings.TrimSpace(input),
		Slug:   slugify(key),
	}
	return ref, taskID(repoRoot, ref.Source, ref.Key), nil
}

func branchName(cfg EffectiveConfig, ref TaskRef, taskID string) string {
	prefix := cfg.Repo.BranchPrefix
	if prefix != "" && prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	return prefix + ref.Slug + "-" + taskID[:6]
}

func renderSessionName(cfg EffectiveConfig, ref TaskRef, taskID string) string {
	value := cfg.Tmux.SessionName
	replacer := strings.NewReplacer(
		"{{repo}}", slugify(cfg.Repo.Name),
		"{{task}}", ref.Slug,
		"{{id}}", taskID[:6],
	)
	value = replacer.Replace(value)
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return slugify(cfg.Repo.Name) + "-" + ref.Slug + "-" + taskID[:6]
	}
	return value
}
