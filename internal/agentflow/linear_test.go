package agentflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
					"id":          "issue-1",
					"identifier":  "AF-123",
					"title":       "Fix auth flow",
					"url":         "https://linear.app/example/issue/AF-123",
					"updatedAt":   "2026-03-15T10:00:00Z",
					"description": "Detailed description",
					"team":        map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
					"state":       map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
					"labels": map[string]any{
						"nodes": []map[string]any{
							{"name": "Frontend"},
							{"name": "Bug"},
						},
					},
					"comments": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false},
						"nodes": []map[string]any{
							{
								"id":        "comment-1",
								"body":      "Investigate the auth middleware",
								"createdAt": "2026-03-14T09:00:00Z",
								"url":       "https://linear.app/example/comment/comment-1",
								"user":      map[string]any{"name": "Alice"},
								"parent":    nil,
							},
							{
								"id":        "comment-2",
								"body":      "Nested reply should be omitted",
								"createdAt": "2026-03-14T10:00:00Z",
								"url":       "https://linear.app/example/comment/comment-2",
								"user":      map[string]any{"name": "Bob"},
								"parent":    map[string]any{"id": "comment-1"},
							},
						},
					},
					"attachments": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": true},
						"nodes": []map[string]any{
							{
								"id":         "attachment-1",
								"title":      "Spec",
								"subtitle":   "Google Doc",
								"url":        "https://example.com/spec",
								"sourceType": "link",
								"createdAt":  "2026-03-14T08:00:00Z",
							},
						},
					},
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
	if issue.Context.Description != "Detailed description" {
		t.Fatalf("expected description to be loaded, got %+v", issue.Context)
	}
	if len(issue.Context.Labels) != 2 || issue.Context.Labels[0] != "Bug" || issue.Context.Labels[1] != "Frontend" {
		t.Fatalf("expected labels to be sorted, got %+v", issue.Context.Labels)
	}
	if len(issue.Context.Comments) != 1 || issue.Context.Comments[0].Author != "Alice" {
		t.Fatalf("expected only top-level comments to be loaded, got %+v", issue.Context.Comments)
	}
	if issue.Context.HasMoreComments {
		t.Fatalf("expected issue comments to be fully loaded, got %+v", issue.Context)
	}
	if len(issue.Context.Attachments) != 1 || issue.Context.Attachments[0].Title != "Spec" {
		t.Fatalf("expected attachments to be loaded, got %+v", issue.Context.Attachments)
	}
	if !issue.Context.HasMoreAttachments {
		t.Fatalf("expected issue to record more attachments availability, got %+v", issue.Context)
	}
}

func TestLinearOpsIssuePagesTopLevelCommentsPastReplies(t *testing.T) {
	t.Parallel()

	ops := newLinearTestOps(t, func(r *http.Request) (*http.Response, error) {
		payload := readLinearPayload(t, r.Body)
		query := payload["query"].(string)
		switch {
		case strings.Contains(query, "query Issue("):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":          "issue-1",
						"identifier":  "AF-123",
						"title":       "Fix auth flow",
						"url":         "https://linear.app/example/issue/AF-123",
						"updatedAt":   "2026-03-15T10:00:00Z",
						"description": "Detailed description",
						"team":        map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":       map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
						"comments": map[string]any{
							"pageInfo": map[string]any{
								"hasNextPage": true,
								"endCursor":   "cursor-1",
							},
							"nodes": []map[string]any{
								{
									"id":        "comment-1",
									"body":      "Top-level first page",
									"createdAt": "2026-03-14T09:00:00Z",
									"url":       "https://linear.app/example/comment/comment-1",
									"user":      map[string]any{"name": "Alice"},
									"parent":    nil,
								},
								{
									"id":        "comment-2",
									"body":      "Reply should not count against the saved top-level budget",
									"createdAt": "2026-03-14T10:00:00Z",
									"url":       "https://linear.app/example/comment/comment-2",
									"user":      map[string]any{"name": "Bob"},
									"parent":    map[string]any{"id": "comment-1"},
								},
							},
						},
					},
				},
			}), nil
		case strings.Contains(query, "query IssueCommentsPage"):
			variables := payload["variables"].(map[string]any)
			if variables["after"] != "cursor-1" {
				t.Fatalf("expected next-page cursor cursor-1, got %v", variables["after"])
			}
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"comments": map[string]any{
							"pageInfo": map[string]any{
								"hasNextPage": false,
								"endCursor":   "",
							},
							"nodes": []map[string]any{
								{
									"id":        "comment-3",
									"body":      "Second top-level page",
									"createdAt": "2026-03-14T11:00:00Z",
									"url":       "https://linear.app/example/comment/comment-3",
									"user":      map[string]any{"name": "Cara"},
									"parent":    nil,
								},
							},
						},
					},
				},
			}), nil
		default:
			t.Fatalf("unexpected query: %s", query)
			return nil, nil
		}
	})

	issue, err := ops.Issue(context.Background(), "test-token", "AF-123")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if issue == nil {
		t.Fatal("expected issue")
	}
	if len(issue.Context.Comments) != 2 {
		t.Fatalf("expected two top-level comments, got %+v", issue.Context.Comments)
	}
	if issue.Context.Comments[0].ID != "comment-1" || issue.Context.Comments[1].ID != "comment-3" {
		t.Fatalf("expected top-level comments from both pages, got %+v", issue.Context.Comments)
	}
	if issue.Context.HasMoreComments {
		t.Fatalf("expected saved comments to be complete after paging, got %+v", issue.Context)
	}
}

func TestLinearOpsPickerIssuesAssignedScopeSortsByStateThenUpdated(t *testing.T) {
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
								"updatedAt":  "2026-03-10T10:00:00Z",
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-2", "name": "In Progress", "type": "started"},
							},
							{
								"id":         "issue-1",
								"identifier": "AF-100",
								"title":      "First",
								"url":        "https://linear.app/example/issue/AF-100",
								"updatedAt":  "2026-03-15T10:00:00Z",
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
							},
							{
								"id":         "issue-3",
								"identifier": "AF-150",
								"title":      "Third",
								"url":        "https://linear.app/example/issue/AF-150",
								"updatedAt":  "2026-03-12T10:00:00Z",
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-3", "name": "Review", "type": "started"},
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
	if len(issues) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(issues))
	}
	if issues[0].Identifier != "AF-150" || issues[1].Identifier != "AF-200" || issues[2].Identifier != "AF-100" {
		t.Fatalf("expected issues to be sorted by state then recent update, got %+v", issues)
	}
}

func TestLinearOpsPickerIssuesAssignedScopeHonorsIdentifierSort(t *testing.T) {
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
								"updatedAt":  time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-1", "name": "Todo", "type": "unstarted"},
							},
							{
								"id":         "issue-1",
								"identifier": "AF-100",
								"title":      "First",
								"url":        "https://linear.app/example/issue/AF-100",
								"updatedAt":  time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
								"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
								"state":      map[string]any{"id": "state-2", "name": "In Progress", "type": "started"},
							},
						},
					},
				},
			},
		}), nil
	})

	issues, err := ops.PickerIssues(context.Background(), "test-token", LinearConfig{
		APIKeyEnv: "LINEAR_API_KEY",
		IssueSort: "identifier",
	})
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
			if !strings.Contains(query, "query WorkflowStates($teamId: ID!)") {
				t.Fatalf("expected workflow states query to use ID!, got %s", query)
			}
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
							"id": "issue-1",
						},
					},
				},
			}), nil
		case strings.Contains(query, "query Issue("):
			return linearHTTPResponse(t, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id":         "issue-1",
						"identifier": "AF-123",
						"title":      "Fix auth flow",
						"url":        "https://linear.app/example/issue/AF-123",
						"updatedAt":  "2026-03-15T10:00:00Z",
						"team":       map[string]any{"id": "team-1", "key": "AF", "name": "Agentflow"},
						"state":      map[string]any{"id": "state-2", "name": "Done", "type": "completed"},
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
