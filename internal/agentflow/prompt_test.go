package agentflow

import (
	"strings"
	"testing"
	"time"
)

func TestBuildAgentContextPromptIncludesLinearIssueContext(t *testing.T) {
	state := TaskState{
		TaskID:       "task-123",
		TaskRef:      TaskRef{Source: taskSourceLinear, Key: "TGG-139", Title: "TGG-139 Implement Copy Bullet Points", URL: "https://linear.app/example/issue/TGG-139"},
		WorktreePath: "/tmp/worktree",
		IssueState:   "In Review",
		IssueContext: &LinearIssueContext{
			TeamName:           "Grassroots gateway",
			TeamKey:            "TGG",
			Description:        "Copy setup action bullet splitting into coaching points.",
			Labels:             []string{"Frontend", "Drill Studio"},
			HasMoreComments:    true,
			HasMoreAttachments: true,
			Comments: []LinearIssueComment{
				{
					ID:        "comment-1",
					Author:    "Alice",
					Body:      "Please match the setup action behavior exactly.",
					URL:       "https://linear.app/example/comment/comment-1",
					CreatedAt: time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC),
				},
			},
			Attachments: []LinearIssueAttachment{
				{
					ID:         "attachment-1",
					Title:      "Spec",
					Subtitle:   "Figma",
					URL:        "https://example.com/spec",
					SourceType: "link",
				},
			},
		},
	}

	prompt := buildAgentContextPrompt("Read AGENTS.md first.", state)
	for _, want := range []string{
		"Read AGENTS.md first.",
		"Linear Issue:",
		"Key: TGG-139",
		"URL: https://linear.app/example/issue/TGG-139",
		"State: In Review",
		"Team: TGG Grassroots gateway",
		"Labels: Drill Studio, Frontend",
		"Description:",
		"Comments:",
		"Alice",
		"... additional comments are available in Linear",
		"Attachments:",
		"https://example.com/spec",
		"... additional attachments are available in Linear",
		"Task: TGG-139 Implement Copy Bullet Points",
		"Task ID: task-123",
		"Worktree: /tmp/worktree",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}
