package agentflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const linearGraphQLEndpoint = "https://api.linear.app/graphql"

type LinearOps struct {
	client   *http.Client
	endpoint string
}

type LinearIssue struct {
	ID         string
	Identifier string
	Title      string
	URL        string
	Team       LinearTeam
	State      LinearWorkflowState
}

type LinearTeam struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type LinearWorkflowState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type linearGraphQLResponse[T any] struct {
	Data   T                    `json:"data"`
	Errors []linearGraphQLError `json:"errors"`
}

type linearGraphQLError struct {
	Message string `json:"message"`
}

func NewLinearOps(client *http.Client) LinearOps {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return LinearOps{
		client:   client,
		endpoint: linearGraphQLEndpoint,
	}
}

func (l LinearOps) Viewer(ctx context.Context, apiKey string) error {
	type responseData struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	resp, err := linearQuery[responseData](ctx, l, apiKey, `query Viewer { viewer { id } }`, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resp.Viewer.ID) == "" {
		return errorsNew("linear viewer query returned no user")
	}
	return nil
}

func (l LinearOps) issue(ctx context.Context, apiKey, lookup string) (*LinearIssue, error) {
	type responseData struct {
		Issue *linearIssueNode `json:"issue"`
	}
	resp, err := linearQuery[responseData](ctx, l, apiKey, linearIssueQuery, map[string]any{
		"id": lookup,
	})
	if err != nil {
		return nil, err
	}
	if resp.Issue == nil {
		return nil, nil
	}
	issue := resp.Issue.toIssue()
	return &issue, nil
}

func (l LinearOps) Issue(ctx context.Context, apiKey, identifier string) (*LinearIssue, error) {
	return l.issue(ctx, apiKey, canonicalLinearIssueKey(identifier))
}

func (l LinearOps) IssueByID(ctx context.Context, apiKey, issueID string) (*LinearIssue, error) {
	return l.issue(ctx, apiKey, strings.TrimSpace(issueID))
}

func (l LinearOps) PickerIssues(ctx context.Context, apiKey string, cfg LinearConfig) ([]LinearIssue, error) {
	scope := effectiveLinearPickerScope(cfg)
	switch scope {
	case "assigned":
		type responseData struct {
			Viewer struct {
				AssignedIssues struct {
					Nodes []linearIssueNode `json:"nodes"`
				} `json:"assignedIssues"`
			} `json:"viewer"`
		}
		resp, err := linearQuery[responseData](ctx, l, apiKey, `
query AssignedIssues($first: Int!) {
  viewer {
    assignedIssues(
      first: $first
      filter: {
        state: { type: { nin: ["completed", "canceled"] } }
      }
    ) {
      nodes {
        id
        identifier
        title
        url
        team { id key name }
        state { id name type }
      }
    }
  }
}
`, map[string]any{"first": 200})
		if err != nil {
			return nil, err
		}
		return linearIssuesFromNodes(resp.Viewer.AssignedIssues.Nodes), nil
	case "team":
		type responseData struct {
			Issues struct {
				Nodes []linearIssueNode `json:"nodes"`
			} `json:"issues"`
		}
		resp, err := linearQuery[responseData](ctx, l, apiKey, `
query TeamIssues($first: Int!, $teamKeys: [String!]!) {
  issues(
    first: $first
    filter: {
      team: { key: { in: $teamKeys } }
      state: { type: { nin: ["completed", "canceled"] } }
    }
  ) {
    nodes {
      id
      identifier
      title
      url
      team { id key name }
      state { id name type }
    }
  }
}
`, map[string]any{
			"first":    200,
			"teamKeys": uniqueUpperStrings(cfg.TeamKeys),
		})
		if err != nil {
			return nil, err
		}
		return linearIssuesFromNodes(resp.Issues.Nodes), nil
	default:
		return nil, fmt.Errorf("unsupported Linear picker scope %q", scope)
	}
}

func (l LinearOps) TransitionIssue(ctx context.Context, apiKey string, issue LinearIssue, stateName, stateType string) (LinearIssue, error) {
	workflowState, err := l.targetWorkflowState(ctx, apiKey, issue.Team.ID, stateName, stateType)
	if err != nil {
		return LinearIssue{}, err
	}
	if issue.State.ID == workflowState.ID {
		return issue, nil
	}

	type responseData struct {
		IssueUpdate struct {
			Success bool             `json:"success"`
			Issue   *linearIssueNode `json:"issue"`
		} `json:"issueUpdate"`
	}
	resp, err := linearQuery[responseData](ctx, l, apiKey, `
mutation IssueUpdate($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) {
    success
    issue {
      id
      identifier
      title
      url
      team { id key name }
      state { id name type }
    }
  }
}
`, map[string]any{
		"id":      issue.ID,
		"stateId": workflowState.ID,
	})
	if err != nil {
		return LinearIssue{}, err
	}
	if !resp.IssueUpdate.Success || resp.IssueUpdate.Issue == nil {
		return LinearIssue{}, errorsNew("linear issue update did not succeed")
	}
	return resp.IssueUpdate.Issue.toIssue(), nil
}

func (l LinearOps) EnsureAttachment(ctx context.Context, apiKey, issueID, title, subtitle, url string) error {
	type responseData struct {
		AttachmentCreate struct {
			Success bool `json:"success"`
		} `json:"attachmentCreate"`
	}
	resp, err := linearQuery[responseData](ctx, l, apiKey, `
mutation AttachmentCreate($issueId: String!, $title: String!, $subtitle: String!, $url: String!) {
  attachmentCreate(
    input: {
      issueId: $issueId
      title: $title
      subtitle: $subtitle
      url: $url
    }
  ) {
    success
  }
}
`, map[string]any{
		"issueId":  issueID,
		"title":    title,
		"subtitle": subtitle,
		"url":      url,
	})
	if err != nil {
		return err
	}
	if !resp.AttachmentCreate.Success {
		return errorsNew("linear attachment creation did not succeed")
	}
	return nil
}

func (l LinearOps) targetWorkflowState(ctx context.Context, apiKey, teamID, stateName, stateType string) (LinearWorkflowState, error) {
	type responseData struct {
		WorkflowStates struct {
			Nodes []LinearWorkflowState `json:"nodes"`
		} `json:"workflowStates"`
	}
	resp, err := linearQuery[responseData](ctx, l, apiKey, `
query WorkflowStates($teamId: String!) {
  workflowStates(filter: { team: { id: { eq: $teamId } } }) {
    nodes {
      id
      name
      type
    }
  }
}
`, map[string]any{"teamId": teamID})
	if err != nil {
		return LinearWorkflowState{}, err
	}
	stateName = strings.TrimSpace(stateName)
	if stateName != "" {
		for _, workflowState := range resp.WorkflowStates.Nodes {
			if strings.EqualFold(strings.TrimSpace(workflowState.Name), stateName) {
				return workflowState, nil
			}
		}
		return LinearWorkflowState{}, fmt.Errorf("linear workflow state %q not found for team %s", stateName, teamID)
	}
	for _, workflowState := range resp.WorkflowStates.Nodes {
		if strings.EqualFold(strings.TrimSpace(workflowState.Type), strings.TrimSpace(stateType)) {
			return workflowState, nil
		}
	}
	return LinearWorkflowState{}, fmt.Errorf("linear workflow state type %q not found for team %s", stateType, teamID)
}

func linearQuery[T any](ctx context.Context, l LinearOps, apiKey, query string, variables map[string]any) (T, error) {
	var zero T
	payload, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(payload))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", strings.TrimSpace(apiKey))

	resp, err := l.client.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return zero, fmt.Errorf("linear api returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var decoded linearGraphQLResponse[T]
	if err := json.Unmarshal(body, &decoded); err != nil {
		return zero, err
	}
	if len(decoded.Errors) > 0 {
		messages := make([]string, 0, len(decoded.Errors))
		for _, item := range decoded.Errors {
			if strings.TrimSpace(item.Message) != "" {
				messages = append(messages, strings.TrimSpace(item.Message))
			}
		}
		if len(messages) == 0 {
			return zero, errorsNew("linear api returned GraphQL errors")
		}
		return zero, errorsNew(strings.Join(messages, "; "))
	}
	return decoded.Data, nil
}

func linearIssuesFromNodes(nodes []linearIssueNode) []LinearIssue {
	out := make([]LinearIssue, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.toIssue())
	}
	slices.SortFunc(out, func(a, b LinearIssue) int {
		return strings.Compare(a.Identifier, b.Identifier)
	})
	return out
}

type linearIssueNode struct {
	ID         string              `json:"id"`
	Identifier string              `json:"identifier"`
	Title      string              `json:"title"`
	URL        string              `json:"url"`
	Team       LinearTeam          `json:"team"`
	State      LinearWorkflowState `json:"state"`
}

func (n linearIssueNode) toIssue() LinearIssue {
	return LinearIssue{
		ID:         strings.TrimSpace(n.ID),
		Identifier: strings.TrimSpace(n.Identifier),
		Title:      strings.TrimSpace(n.Title),
		URL:        strings.TrimSpace(n.URL),
		Team:       n.Team,
		State:      n.State,
	}
}

func uniqueUpperStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToUpper(strings.TrimSpace(value))
		if trimmed == "" || slices.Contains(out, trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

const linearIssueQuery = `
query Issue($id: String!) {
  issue(id: $id) {
    id
    identifier
    title
    url
    team { id key name }
    state { id name type }
  }
}
`

func errorsNew(message string) error {
	return fmt.Errorf("%s", message)
}
