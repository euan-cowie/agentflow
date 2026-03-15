package cli

import (
	"testing"

	"github.com/euan-cowie/agentflow/internal/agentflow"
)

func TestTaskListTitleStripsLinearIdentifier(t *testing.T) {
	state := agentflow.TaskState{
		TaskRef: agentflow.TaskRef{
			Source: "linear",
			Key:    "TGG-132",
			Title:  "TGG-132 Add reviews and testimonials to the website",
		},
	}

	title := taskListTitle(state)
	if title != "Add reviews and testimonials to the website" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestSummarizeFailureReasonExtractsGraphQLErrorMessage(t *testing.T) {
	reason := `linear api returned 400 Bad Request: {"errors":[{"message":"Variable \"$teamId\" of type \"String!\" used in position expecting type \"ID\"."}]}`

	summary := summarizeFailureReason(reason)
	if summary != `Variable "$teamId" of type "String!" used in position expecting type "ID".` {
		t.Fatalf("unexpected summary: %q", summary)
	}
}
