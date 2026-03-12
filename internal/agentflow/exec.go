package agentflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor struct{}

func (e Executor) Run(ctx context.Context, cwd string, env []string, name string, args ...string) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	cmd.Env = mergedEnv(env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return result, fmt.Errorf("%s %v: %w: %s", name, args, err, result.Stderr)
	}
	return result, nil
}

func (e Executor) RunLogged(ctx context.Context, cwd string, env []string, logPath string, stream io.Writer, command string) error {
	if err := ensureDir(filepath.Dir(logPath)); err != nil {
		return err
	}
	file, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer file.Close()

	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	cmd.Dir = cwd
	cmd.Env = mergedEnv(env)

	writer := io.Writer(file)
	if stream != nil {
		writer = io.MultiWriter(file, stream)
	}
	cmd.Stdout = writer
	cmd.Stderr = writer
	return cmd.Run()
}

func mergedEnv(extra []string) []string {
	out := append([]string(nil), os.Environ()...)
	out = append(out, extra...)
	return out
}
