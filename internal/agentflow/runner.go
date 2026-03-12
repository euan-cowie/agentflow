package agentflow

import (
	"fmt"
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
	values := make([]string, 0, 4)
	values = append(values, "AGENTFLOW_TASK_ID="+state.TaskID)
	values = append(values, "AGENTFLOW_TASK_SLUG="+state.TaskRef.Slug)
	values = append(values, "AGENTFLOW_WORKTREE="+state.WorktreePath)
	if state.AllocatedPort != 0 {
		values = append(values, fmt.Sprintf("AGENTFLOW_PORT=%d", state.AllocatedPort))
		if state.PortKey != "" && state.PortKey != "AGENTFLOW_PORT" {
			values = append(values, fmt.Sprintf("%s=%d", state.PortKey, state.AllocatedPort))
		}
	}
	values = append(values, "AGENTFLOW_ENV_FILE="+filepath.Join(state.WorktreePath, state.ManagedEnvFile))
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
