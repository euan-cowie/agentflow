package agentflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLinearOpsIssueUsesAuthorizationHeader(t *testing.T) {
	t.Parallel()

	ops := newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		return linearHTTPResponse(t, map[string]any{
			"data": map[string]any{
				"issue": map[string]any{
					"id":         "issue-1",
					"identifier": "AF-123",
					"title":      "Fix auth flow",
					"url":        "https://linear.app/example/issue/AF-123",
					"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
					"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
				},
			},
		}), nil
	})

	issue, err := ops.Issue(context.Background(), "test-token", "AF-123")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if issue == nil {
		t.Fatal("expected issue")
	}
	if issue.Identifier != "AF-123" || issue.State.Name != "Todo" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
}

func TestLinearOpsPickerIssuesAssignedScopeSortsByIdentifier(t *testing.T) {
	t.Parallel()

	ops := newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		return linearHTTPResponse(t, map[string]any{
			"data": map[string]any{
				"viewer": map[string]any{
					"assignedIssues": map[string]any{
						"nodes": []map[string]any{
							{
								"id":         "issue-2",
								"identifier": "AF-200",
								"title":      "Second",
								"url":        "https://linear.app/example/issue/AF-200",
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
							},
							{
								"id":         "issue-1",
								"identifier": "AF-100",
								"title":      "First",
								"url":        "https://linear.app/example/issue/AF-100",
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-2", "name": "In Progress", "type": "started"},
							},
						},
					},
				},
			},
		}), nil
	})

	issues, err := ops.PickerIssues(context.Background(), "test-token", LinearConfig{APIKeyEnv: "LINEAR_API_KEY"})
	if err != nil {
		t.Fatalf("PickerIssues returned error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
	if issues[0].Identifier != "AF-100" || issues[1].Identifier != "AF-200" {
		t.Fatalf("expected issues to be sorted by identifier, got %+v", issues)
	}
}

func TestLinearOpsTransitionIssueAndAttachment(t *testing.T) {
	t.Parallel()

	ops := newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		variables := payload["variables"].(map[string]any)

		switch {
		case strings.Contains(query, "query WorkflowStates"):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"workflowStates": map[string]any{
						"nodes": []map[string]any{
							{"id": "state-1", "name": "Todo", "type": "unstarted"},
							{"id": "state-2", "name": "Done", "type": "completed"},
						},
					},
				},
			}), nil
		case strings.Contains(query, "mutation IssueUpdate"):
			if variables["stateId"] != "state-2" {
				t.Fatalf("expected issue update to target state-2, got %v", variables["stateId"])
			}
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issueUpdate": map[string]any{
						"success": true,
						"issue": map[string]any{
							"id":         "issue-1",
							"identifier": "AF-123",
							"title":      "Fix auth flow",
							"url":        "https://linear.app/example/issue/AF-123",
							"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
							"state":      map[string]any{"id": "state-2", "name": "Done", "type": "completed"},
						},
					},
				},
			}), nil
		case strings.Contains(query, "mutation AttachmentCreate"):
			if variables["issueId"] != "issue-1" || variables["url"] != "https://github.com/example/pull/123" {
				t.Fatalf("unexpected attachment variables: %+v", variables)
			}
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"attachmentCreate": map[string]any{
						"success": true,
					},
				},
			}), nil
		default:
			t.Fatalf("unexpected query: %s", query)
			return nil, nil
		}
	})

	issue, err := ops.TransitionIssue(context.Background(), "test-token", LinearIssue{
		ID:         "issue-1",
		Identifier: "AF-123",
		Title:      "Fix auth flow",
		URL:        "https://linear.app/example/issue/AF-123",
		Team:       LinearTeam{ID: "team-1", Key: "AF"},
		State:      LinearWorkflowState{ID: "state-1", Name: "Todo", Type: "unstarted"},
	}, "Done", "completed")
	if err != nil {
		t.Fatalf("TransitionIssue returned error: %v", err)
	}
	if issue.State.Name != "Done" {
		t.Fatalf("expected updated issue state, got %+v", issue)
	}

	if err := ops.EnsureAttachment(context.Background(), "test-token", "issue-1", "PR #123", "Open pull request", "https://github.com/example/pull/123"); err != nil {
		t.Fatalf("EnsureAttachment returned error: %v", err)
	}
}

func readLinearPayload(t *testing.T, body io.ReadCloser) map[string]any {
	t.Helper()
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return payload
}

func writeLinearResponse(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func linearHTTPResponse(t *testing.T, payload map[string]any) *http.Response {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}

func newLinearTestOps(t *testing.T, fn func(*http.Request) (*http.Response, error)) LinearOps {
	t.Helper()
	client := &http.Client{
		Transport: roundTripFunc(fn),
	}
	ops := NewLinearOps(client)
	ops.endpoint = "https://example.invalid/graphql"
	return ops
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
