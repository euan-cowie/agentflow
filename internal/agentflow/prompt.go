package agentflow

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

func readConfirmationPrompt(input io.Reader, output io.Writer, prompt string) (bool, error) {
	if _, err := io.WriteString(output, prompt); err != nil {
		return false, err
	}
	reader := bufio.NewReader(input)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "yes" || answer == "y", nil
}

func (a *App) confirmForceDown(state TaskState) error {
	prompt := fmt.Sprintf("Force remove dirty worktree for task %q? This will discard uncommitted changes. Type 'yes' to continue: ", state.TaskRef.Title)
	ok, err := readConfirmationPrompt(a.stdin, a.stdout, prompt)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("forced teardown declined")
	}
	return nil
}
