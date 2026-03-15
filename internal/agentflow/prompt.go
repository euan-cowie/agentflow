package agentflow

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const (
	linearPromptDescriptionLimit = 6000
	linearPromptCommentLimit     = 50
	linearPromptCommentBodyLimit = 1500
	linearPromptAttachmentLimit  = 20
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

func buildAgentContextPrompt(basePrompt string, state TaskState) string {
	parts := make([]string, 0, 3)
	if prompt := strings.TrimSpace(basePrompt); prompt != "" {
		parts = append(parts, prompt)
	}
	if issueContext := formatLinearIssuePrompt(state); issueContext != "" {
		parts = append(parts, issueContext)
	}
	parts = append(parts, fmt.Sprintf("Task: %s\nTask ID: %s\nWorktree: %s", state.TaskRef.Title, state.TaskID, state.WorktreePath))
	return strings.Join(parts, "\n\n")
}

func formatLinearIssuePrompt(state TaskState) string {
	if !isLinearTask(state) {
		return ""
	}

	lines := []string{"Linear Issue:"}
	if key := strings.TrimSpace(state.TaskRef.Key); key != "" {
		lines = append(lines, "Key: "+key)
	}
	if url := strings.TrimSpace(state.TaskRef.URL); url != "" {
		lines = append(lines, "URL: "+url)
	}
	if issueState := strings.TrimSpace(state.IssueState); issueState != "" {
		lines = append(lines, "State: "+issueState)
	}

	if state.IssueContext == nil {
		return strings.Join(lines, "\n")
	}

	context := state.IssueContext
	if team := strings.TrimSpace(strings.Trim(strings.Join([]string{context.TeamKey, context.TeamName}, " "), " ")); team != "" {
		lines = append(lines, "Team: "+team)
	}
	if len(context.Labels) > 0 {
		labels := append([]string(nil), context.Labels...)
		slices.Sort(labels)
		lines = append(lines, "Labels: "+strings.Join(labels, ", "))
	}
	if description := trimPromptText(context.Description, linearPromptDescriptionLimit); description != "" {
		lines = append(lines, "", "Description:", description)
	}
	if len(context.Comments) > 0 {
		lines = append(lines, "", "Comments:")
		shownComments := min(len(context.Comments), linearPromptCommentLimit)
		for idx, comment := range context.Comments {
			if idx >= linearPromptCommentLimit {
				break
			}
			header := comment.CreatedAt.UTC().Format("2006-01-02 15:04")
			if author := strings.TrimSpace(comment.Author); author != "" {
				header += " " + author
			}
			lines = append(lines, fmt.Sprintf("[%d] %s", idx+1, header))
			lines = append(lines, trimPromptText(comment.Body, linearPromptCommentBodyLimit))
			if url := strings.TrimSpace(comment.URL); url != "" {
				lines = append(lines, "Comment URL: "+url)
			}
			lines = append(lines, "")
		}
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if shownComments < len(context.Comments) {
			lines = append(lines, fmt.Sprintf("... %d more comments omitted", len(context.Comments)-shownComments))
		}
		if context.HasMoreComments {
			lines = append(lines, "... additional comments are available in Linear")
		}
	}
	if len(context.Attachments) > 0 {
		lines = append(lines, "", "Attachments:")
		shownAttachments := min(len(context.Attachments), linearPromptAttachmentLimit)
		for idx, attachment := range context.Attachments {
			if idx >= linearPromptAttachmentLimit {
				break
			}
			parts := []string{strings.TrimSpace(attachment.Title)}
			if subtitle := strings.TrimSpace(attachment.Subtitle); subtitle != "" {
				parts = append(parts, subtitle)
			}
			if sourceType := strings.TrimSpace(attachment.SourceType); sourceType != "" {
				parts = append(parts, sourceType)
			}
			parts = append(parts, strings.TrimSpace(attachment.URL))
			lines = append(lines, "- "+strings.Join(compactStrings(parts), " | "))
		}
		if shownAttachments < len(context.Attachments) {
			lines = append(lines, fmt.Sprintf("... %d more attachments omitted", len(context.Attachments)-shownAttachments))
		}
		if context.HasMoreAttachments {
			lines = append(lines, "... additional attachments are available in Linear")
		}
	}

	return strings.Join(lines, "\n")
}

func trimPromptText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return value
	}
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
