package agentflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AgentRunner struct{}

func (r AgentRunner) commandString(agent AgentConfig, worktree string, prompt string, resume bool) (string, error) {
	command := strings.TrimSpace(agent.Command)
	if command == "" {
		command = defaultWorkflowConfig().Agents["default"].Command
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("agent command must not be empty")
	}
	if agent.Runner != "" && agent.Runner != "codex" {
		return "", fmt.Errorf("unsupported runner %q", agent.Runner)
	}
	args := make([]string, 0, len(parts)+4)
	args = append(args, parts[0])
	if resume {
		args = append(args, "resume", "--last")
	}
	args = append(args, parts[1:]...)
	args = append(args, "-C", worktree)
	if prompt != "" {
		args = append(args, prompt)
	}
	return shellJoin(args), nil
}

func taskEnv(state TaskState) []string {
	values := make([]string, 0, 6)
	values = append(values, "AGENTFLOW_TASK_ID="+state.TaskID)
	values = append(values, "AGENTFLOW_TASK_SLUG="+state.TaskRef.Slug)
	values = append(values, "AGENTFLOW_WORKTREE="+state.WorktreePath)
	managedFiles := state.EffectiveManagedEnvFiles()
	absoluteManagedFiles := make([]string, 0, len(managedFiles))
	for _, target := range managedFiles {
		absoluteManagedFiles = append(absoluteManagedFiles, filepath.Join(state.WorktreePath, target))
	}
	if len(absoluteManagedFiles) == 1 {
		values = append(values, "AGENTFLOW_ENV_FILE="+absoluteManagedFiles[0])
	}
	if len(absoluteManagedFiles) > 0 {
		values = append(values, "AGENTFLOW_ENV_FILES="+strings.Join(absoluteManagedFiles, string(os.PathListSeparator)))
	}

	bindings := state.EffectivePortBindings()
	if len(bindings) > 0 {
		values = append(values, fmt.Sprintf("AGENTFLOW_PORT=%d", bindings[0].Port))
	}
	seen := map[string]int{}
	ports := map[string]int{}
	for _, binding := range bindings {
		if binding.Key == "" || binding.Port == 0 {
			continue
		}
		seen[binding.Key]++
		ports[binding.Key] = binding.Port
	}
	for key, count := range seen {
		if count == 1 {
			values = append(values, fmt.Sprintf("%s=%d", key, ports[key]))
		}
	}
	return values
}

func shellCommandWithEnv(command string, env []string) string {
	if len(env) == 0 {
		return command
	}
	parts := make([]string, 0, len(env)+1)
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		parts = append(parts, key+"="+shellQuote(value))
	}
	parts = append(parts, command)
	return strings.Join(parts, " ")
}
