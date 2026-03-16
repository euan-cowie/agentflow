package agentflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type TmuxOps struct {
	exec Executor
}

func NewTmuxOps(exec Executor) TmuxOps {
	return TmuxOps{exec: exec}
}

func (t TmuxOps) HasSession(ctx context.Context, session string) bool {
	_, err := t.exec.Run(ctx, "", nil, "tmux", "has-session", "-t", session)
	return err == nil
}

func (t TmuxOps) NewSession(ctx context.Context, session, cwd string, window TmuxWindowConfig, command string) error {
	args := []string{"new-session", "-d", "-s", session, "-c", cwd, "-n", window.Name}
	if command != "" {
		args = append(args, "sh", "-lc", command)
	}
	_, err := t.exec.Run(ctx, "", nil, "tmux", args...)
	return err
}

func (t TmuxOps) AddWindow(ctx context.Context, session, cwd string, window TmuxWindowConfig, command string) error {
	args := []string{"new-window", "-d", "-t", session, "-c", cwd, "-n", window.Name}
	if command != "" {
		args = append(args, "sh", "-lc", command)
	}
	_, err := t.exec.Run(ctx, "", nil, "tmux", args...)
	return err
}

func (t TmuxOps) WindowExists(ctx context.Context, session, name string) bool {
	target := fmt.Sprintf("%s:%s", session, name)
	_, err := t.exec.Run(ctx, "", nil, "tmux", "list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return false
	}
	result, err := t.exec.Run(ctx, "", nil, "tmux", "display-message", "-p", "-t", target, "#{window_name}")
	return err == nil && strings.TrimSpace(result.Stdout) == name
}

func (t TmuxOps) RespawnWindow(ctx context.Context, session, cwd string, window TmuxWindowConfig, command string) error {
	target := fmt.Sprintf("%s:%s", session, window.Name)
	args := []string{"respawn-window", "-k", "-t", target, "-c", cwd}
	if command != "" {
		args = append(args, "sh", "-lc", command)
	}
	_, err := t.exec.Run(ctx, "", nil, "tmux", args...)
	return err
}

func (t TmuxOps) SendKeys(ctx context.Context, session, name, command string) error {
	target := fmt.Sprintf("%s:%s", session, name)
	if _, err := t.exec.Run(ctx, "", nil, "tmux", "send-keys", "-t", target, "-l", command); err != nil {
		return err
	}
	_, err := t.exec.Run(ctx, "", nil, "tmux", "send-keys", "-t", target, "C-m")
	return err
}

func (t TmuxOps) Attach(ctx context.Context, session string) error {
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", session)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (t TmuxOps) SelectWindow(ctx context.Context, session, name string) error {
	target := fmt.Sprintf("%s:%s", session, name)
	_, err := t.exec.Run(ctx, "", nil, "tmux", "select-window", "-t", target)
	return err
}

func (t TmuxOps) KillSession(ctx context.Context, session string) error {
	_, err := t.exec.Run(ctx, "", nil, "tmux", "kill-session", "-t", session)
	return err
}

func (t TmuxOps) CaptureWindow(ctx context.Context, session, name string) (string, error) {
	target := fmt.Sprintf("%s:%s", session, name)
	result, err := t.exec.Run(ctx, "", nil, "tmux", "capture-pane", "-p", "-t", target)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}
