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
	UpdatedAt  time.Time
	Team       LinearTeam
	State      LinearWorkflowState
	Context    LinearIssueContext
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

type linearCommentAuthor struct {
	Name string `json:"name"`
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
        updatedAt
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
		return linearIssuesFromNodes(resp.Viewer.AssignedIssues.Nodes, effectiveLinearIssueSort(cfg)), nil
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
      updatedAt
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
		return linearIssuesFromNodes(resp.Issues.Nodes, effectiveLinearIssueSort(cfg)), nil
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
      updatedAt
      description
      team { id key name }
      state { id name type }
      labels(first: 20) {
        nodes {
          name
        }
      }
      comments(first: 50) {
        pageInfo {
          hasNextPage
        }
        nodes {
          id
          body
          createdAt
          url
          user { name }
          parent { id }
        }
      }
      attachments(first: 20) {
        pageInfo {
          hasNextPage
        }
        nodes {
          id
          title
          subtitle
          url
          sourceType
          createdAt
        }
      }
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
query WorkflowStates($teamId: ID!) {
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

func linearIssuesFromNodes(nodes []linearIssueNode, sortMode string) []LinearIssue {
	out := make([]LinearIssue, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.toIssue())
	}
	switch sortMode {
	case "linear":
		return out
	case "identifier":
		slices.SortFunc(out, func(a, b LinearIssue) int {
			if cmp := strings.Compare(a.Identifier, b.Identifier); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Title, b.Title)
		})
		return out
	case "updated":
		slices.SortFunc(out, func(a, b LinearIssue) int {
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				if a.UpdatedAt.After(b.UpdatedAt) {
					return -1
				}
				return 1
			}
			if cmp := strings.Compare(a.Identifier, b.Identifier); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Title, b.Title)
		})
		return out
	}
	slices.SortFunc(out, func(a, b LinearIssue) int {
		if cmp := linearIssueStateRank(a.State.Type) - linearIssueStateRank(b.State.Type); cmp != 0 {
			return cmp
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			if a.UpdatedAt.After(b.UpdatedAt) {
				return -1
			}
			return 1
		}
		if cmp := strings.Compare(a.Identifier, b.Identifier); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Title, b.Title)
	})
	return out
}

type linearIssueNode struct {
	ID          string              `json:"id"`
	Identifier  string              `json:"identifier"`
	Title       string              `json:"title"`
	URL         string              `json:"url"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Team        LinearTeam          `json:"team"`
	State       LinearWorkflowState `json:"state"`
	Description string              `json:"description"`
	Labels      struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Nodes []struct {
			ID        string               `json:"id"`
			Body      string               `json:"body"`
			URL       string               `json:"url"`
			CreatedAt time.Time            `json:"createdAt"`
			User      *linearCommentAuthor `json:"user"`
			Parent    *struct {
				ID string `json:"id"`
			} `json:"parent"`
		} `json:"nodes"`
	} `json:"comments"`
	Attachments struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Nodes []struct {
			ID         string    `json:"id"`
			Title      string    `json:"title"`
			Subtitle   string    `json:"subtitle"`
			URL        string    `json:"url"`
			SourceType string    `json:"sourceType"`
			CreatedAt  time.Time `json:"createdAt"`
		} `json:"nodes"`
	} `json:"attachments"`
}

func (n linearIssueNode) toIssue() LinearIssue {
	labels := make([]string, 0, len(n.Labels.Nodes))
	for _, label := range n.Labels.Nodes {
		name := strings.TrimSpace(label.Name)
		if name != "" {
			labels = append(labels, name)
		}
	}
	slices.Sort(labels)

	comments := make([]LinearIssueComment, 0, len(n.Comments.Nodes))
	for _, comment := range n.Comments.Nodes {
		if comment.Parent != nil {
			continue
		}
		body := strings.TrimSpace(comment.Body)
		if body == "" {
			continue
		}
		author := ""
		if comment.User != nil {
			author = strings.TrimSpace(comment.User.Name)
		}
		comments = append(comments, LinearIssueComment{
			ID:        strings.TrimSpace(comment.ID),
			Author:    author,
			Body:      body,
			URL:       strings.TrimSpace(comment.URL),
			CreatedAt: comment.CreatedAt,
		})
	}
	slices.SortFunc(comments, func(a, b LinearIssueComment) int {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if a.CreatedAt.Before(b.CreatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})

	attachments := make([]LinearIssueAttachment, 0, len(n.Attachments.Nodes))
	for _, attachment := range n.Attachments.Nodes {
		attachments = append(attachments, LinearIssueAttachment{
			ID:         strings.TrimSpace(attachment.ID),
			Title:      strings.TrimSpace(attachment.Title),
			Subtitle:   strings.TrimSpace(attachment.Subtitle),
			URL:        strings.TrimSpace(attachment.URL),
			SourceType: strings.TrimSpace(attachment.SourceType),
			CreatedAt:  attachment.CreatedAt,
		})
	}
	slices.SortFunc(attachments, func(a, b LinearIssueAttachment) int {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if a.CreatedAt.Before(b.CreatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})

	return LinearIssue{
		ID:         strings.TrimSpace(n.ID),
		Identifier: strings.TrimSpace(n.Identifier),
		Title:      strings.TrimSpace(n.Title),
		URL:        strings.TrimSpace(n.URL),
		UpdatedAt:  n.UpdatedAt,
		Team:       n.Team,
		State:      n.State,
		Context: LinearIssueContext{
			TeamName:           strings.TrimSpace(n.Team.Name),
			TeamKey:            strings.TrimSpace(n.Team.Key),
			Description:        strings.TrimSpace(n.Description),
			Labels:             labels,
			Comments:           comments,
			HasMoreComments:    n.Comments.PageInfo.HasNextPage,
			Attachments:        attachments,
			HasMoreAttachments: n.Attachments.PageInfo.HasNextPage,
		},
	}
}

func linearIssueStateRank(stateType string) int {
	switch strings.ToLower(strings.TrimSpace(stateType)) {
	case "started":
		return 0
	case "unstarted":
		return 1
	case "completed":
		return 2
	case "canceled":
		return 3
	default:
		return 4
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
    updatedAt
    description
    team { id key name }
    state { id name type }
    labels(first: 20) {
      nodes {
        name
      }
    }
    comments(first: 50) {
      pageInfo {
        hasNextPage
      }
      nodes {
        id
        body
        createdAt
        url
        user { name }
        parent { id }
      }
    }
    attachments(first: 20) {
      pageInfo {
        hasNextPage
      }
      nodes {
        id
        title
        subtitle
        url
        sourceType
        createdAt
      }
    }
  }
}
`

func errorsNew(message string) error {
	return fmt.Errorf("%s", message)
}
