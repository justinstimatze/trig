// Package linear is a minimal GraphQL client for the Linear mutations and
// queries trig needs — not a general-purpose SDK. Request/response/error
// shape mirrors internal/posthog (itself mirroring plancheck's
// internal/simulate/agentapi.go), adapted for GraphQL's single endpoint and
// its own error-envelope convention.
package linear

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/justinstimatze/trig/internal/config"
)

// endpoint is a var, not a const, so tests can point it at a local server.
var endpoint = "https://api.linear.app/graphql"

type Client struct {
	apiKey string
	http   *http.Client
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		apiKey: cfg.LinearAPIKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// AuthError means the API key is unauthorized or lacks a required
// permission — HTTP 401/403, or a GraphQL error whose message reads as an
// auth/permission failure (Linear can report an under-scoped key either
// way). The message-text check is a best-effort heuristic, unverified
// against a real under-scoped key.
type AuthError struct {
	Messages []string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("Linear API error: %s", strings.Join(e.Messages, "; "))
}

// NotFoundError means the requested issue or label doesn't exist. Linear
// reports a missing entity as a GraphQL error (e.g. "Entity not found:
// Issue" for issue(id: "ZZZ-999")) — not a null `data` field, which is the
// other shape this type also covers (see GetIssueByIdentifier,
// GetLabelByName).
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

func looksLikeAuthError(msgs []string) bool {
	for _, m := range msgs {
		lower := strings.ToLower(m)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden") {
			return true
		}
	}
	return false
}

func looksLikeNotFoundError(msgs []string) bool {
	for _, m := range msgs {
		if strings.Contains(strings.ToLower(m), "not found") {
			return true
		}
	}
	return false
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

// do executes a single query or mutation and decodes its `data` field into
// out. GraphQL surfaces failures as an `errors` array on an HTTP 200, not
// via status code, so that's checked before the status-code check ever
// matters for anything but transport-level failure.
func (c *Client) do(query string, variables map[string]interface{}, out interface{}) error {
	reqBytes, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey) // no "Bearer" prefix — Linear's own convention

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return &AuthError{Messages: []string{fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateUTF8(string(body), 200))}}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear API error %d: %s", resp.StatusCode, truncateUTF8(string(body), 200))
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
		}
		if looksLikeAuthError(msgs) {
			return &AuthError{Messages: msgs}
		}
		if looksLikeNotFoundError(msgs) {
			return &NotFoundError{Message: strings.Join(msgs, "; ")}
		}
		return fmt.Errorf("linear GraphQL error: %s", strings.Join(msgs, "; "))
	}
	if out != nil && envelope.Data != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("parse data: %w", err)
		}
	}
	return nil
}

const issueQuery = `
query($id: String!) {
  issue(id: $id) {
    id
    identifier
    title
    labels { nodes { id name } }
    attachments { nodes { id title subtitle url metadata } }
  }
}`

// GetIssueByIdentifier looks up an issue by its human-readable identifier
// (e.g. "CUR-515") — Linear's `issue(id:)` query resolves a human-readable
// identifier directly, no separate search step needed.
func (c *Client) GetIssueByIdentifier(identifier string) (*Issue, error) {
	var resp issueResponse
	if err := c.do(issueQuery, map[string]interface{}{"id": identifier}, &resp); err != nil {
		return nil, err
	}
	if resp.Issue == nil {
		return nil, &NotFoundError{Message: fmt.Sprintf("no issue with identifier %q", identifier)}
	}
	return &Issue{
		ID:          resp.Issue.ID,
		Identifier:  resp.Issue.Identifier,
		Title:       resp.Issue.Title,
		Labels:      resp.Issue.Labels.Nodes,
		Attachments: resp.Issue.Attachments.Nodes,
	}, nil
}

const labelByNameQuery = `
query($name: String!) {
  issueLabels(filter: {name: {eq: $name}}) {
    nodes { id name }
  }
}`

// GetLabelByName finds a workspace label (e.g. "posthog-flag") by exact name.
func (c *Client) GetLabelByName(name string) (*IssueLabel, error) {
	var resp issueLabelsResponse
	if err := c.do(labelByNameQuery, map[string]interface{}{"name": name}, &resp); err != nil {
		return nil, err
	}
	if len(resp.IssueLabels.Nodes) == 0 {
		return nil, &NotFoundError{Message: fmt.Sprintf("no label named %q", name)}
	}
	return &resp.IssueLabels.Nodes[0], nil
}

const labelCreateMutation = `
mutation($name: String!) {
  issueLabelCreate(input: {name: $name}) {
    success
    issueLabel { id name }
  }
}`

// CreateLabel creates a new workspace-level label (no team scoping — trig's
// `flagged` label is meant to work across every team's tickets, matching
// DESIGN.md's "zero config needed" intent).
func (c *Client) CreateLabel(name string) (*IssueLabel, error) {
	var resp struct {
		IssueLabelCreate struct {
			Success    bool       `json:"success"`
			IssueLabel IssueLabel `json:"issueLabel"`
		} `json:"issueLabelCreate"`
	}
	if err := c.do(labelCreateMutation, map[string]interface{}{"name": name}, &resp); err != nil {
		return nil, err
	}
	if !resp.IssueLabelCreate.Success {
		return nil, fmt.Errorf("issueLabelCreate reported failure for %q", name)
	}
	return &resp.IssueLabelCreate.IssueLabel, nil
}

const addLabelMutation = `
mutation($issueID: String!, $labelID: String!) {
  issueUpdate(id: $issueID, input: {addedLabelIds: [$labelID]}) {
    success
  }
}`

// AddLabel applies labelID to issueID additively — it does not touch the
// issue's other labels (IssueUpdateInput.addedLabelIds, not labelIds).
func (c *Client) AddLabel(issueID, labelID string) error {
	var resp issueUpdateResponse
	if err := c.do(addLabelMutation, map[string]interface{}{"issueID": issueID, "labelID": labelID}, &resp); err != nil {
		return err
	}
	if !resp.IssueUpdate.Success {
		return fmt.Errorf("issueUpdate reported failure for issue %s", issueID)
	}
	return nil
}

const removeLabelMutation = `
mutation($issueID: String!, $labelID: String!) {
  issueUpdate(id: $issueID, input: {removedLabelIds: [$labelID]}) {
    success
  }
}`

// RemoveLabel removes labelID from issueID without touching other labels
// (IssueUpdateInput.removedLabelIds, the counterpart to AddLabel's
// addedLabelIds).
func (c *Client) RemoveLabel(issueID, labelID string) error {
	var resp issueUpdateResponse
	if err := c.do(removeLabelMutation, map[string]interface{}{"issueID": issueID, "labelID": labelID}, &resp); err != nil {
		return err
	}
	if !resp.IssueUpdate.Success {
		return fmt.Errorf("issueUpdate reported failure for issue %s", issueID)
	}
	return nil
}

const attachmentCreateMutation = `
mutation($issueID: String!, $title: String!, $subtitle: String, $url: String!, $metadata: JSONObject) {
  attachmentCreate(input: {issueId: $issueID, title: $title, subtitle: $subtitle, url: $url, metadata: $metadata}) {
    success
    attachment { id title subtitle url metadata }
  }
}`

// CreateAttachment adds a new attachment to issueID's Links/Attachments row.
// Linear's own attachmentCreate is an upsert keyed on (issueId, url) — its
// schema description says so verbatim: "Creates a new attachment, or
// updates existing if the same url and issueId is used." So calling this
// twice with the same issueID+url edits the same attachment in place (same
// id) rather than creating a second one. There is no way to have two
// independent attachments to the same URL on one issue.
func (c *Client) CreateAttachment(issueID, title, subtitle, url string, metadata map[string]interface{}) (*Attachment, error) {
	var resp attachmentCreateResponse
	vars := map[string]interface{}{"issueID": issueID, "title": title, "subtitle": subtitle, "url": url, "metadata": metadata}
	if err := c.do(attachmentCreateMutation, vars, &resp); err != nil {
		return nil, err
	}
	if !resp.AttachmentCreate.Success {
		return nil, fmt.Errorf("attachmentCreate reported failure for issue %s", issueID)
	}
	return &resp.AttachmentCreate.Attachment, nil
}

const attachmentUpdateMutation = `
mutation($id: String!, $title: String!, $subtitle: String, $metadata: JSONObject) {
  attachmentUpdate(id: $id, input: {title: $title, subtitle: $subtitle, metadata: $metadata}) {
    success
    attachment { id title subtitle url metadata }
  }
}`

// UpdateAttachment edits an existing attachment in place — the create-or-
// update path DESIGN.md calls for so re-runs don't accumulate duplicates.
func (c *Client) UpdateAttachment(attachmentID, title, subtitle string, metadata map[string]interface{}) (*Attachment, error) {
	var resp attachmentUpdateResponse
	vars := map[string]interface{}{"id": attachmentID, "title": title, "subtitle": subtitle, "metadata": metadata}
	if err := c.do(attachmentUpdateMutation, vars, &resp); err != nil {
		return nil, err
	}
	if !resp.AttachmentUpdate.Success {
		return nil, fmt.Errorf("attachmentUpdate reported failure for attachment %s", attachmentID)
	}
	return &resp.AttachmentUpdate.Attachment, nil
}

// truncateUTF8 cuts s to at most n bytes, backing off further if the cut
// lands mid-rune — a plain byte slice can split a multi-byte UTF-8
// character in half, leaving an invalid tail wherever the string is shown.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
