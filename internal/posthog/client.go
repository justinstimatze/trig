// Package posthog is a minimal REST client for the PostHog flag endpoints
// trig needs — not a general-purpose SDK. Request/response/error shape
// mirrors plancheck's internal/simulate/agentapi.go, this codebase's
// existing pattern for a small personal-API-key HTTP client.
package posthog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/justinstimatze/trig/internal/config"
)

type Client struct {
	apiKey    string
	host      string
	projectID string
	http      *http.Client
}

// AuthError means the API key is missing a required scope or otherwise
// unauthorized (HTTP 401/403) — distinguished from other failures so
// callers can tell the user to check their key's scopes instead of
// dumping a raw API error.
type AuthError struct {
	StatusCode int
	Body       string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("PostHog API error %d: %s", e.StatusCode, e.Body)
}

// NotFoundError means the requested flag doesn't exist (by the caller's
// lookup key) — distinguished so callers can exit with a "not found" code
// rather than a generic failure.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

func NewClient(cfg *config.Config) *Client {
	return &Client{
		apiKey:    cfg.PostHogAPIKey,
		host:      cfg.PostHogHost,
		projectID: cfg.PostHogProjectID,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// FlagURL is the flag's page in the PostHog UI — used as the Linear
// attachment's link-back target.
func (c *Client) FlagURL(id int) string {
	return fmt.Sprintf("https://%s/project/%s/feature_flags/%d", c.host, c.projectID, id)
}

// do executes a single HTTP request against reqURL and decodes the JSON
// response body into out. body, if non-nil, is marshaled as the request
// body (used for PATCH/POST; nil for GET).
func (c *Client) do(method, reqURL string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, reqURL, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		truncated := string(respBody[:min(len(respBody), 200)])
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return &AuthError{StatusCode: resp.StatusCode, Body: truncated}
		}
		return fmt.Errorf("PostHog API error %d: %s", resp.StatusCode, truncated)
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
	}
	return nil
}

// GetFlagByKey looks up a feature flag by its exact string key. PostHog's
// list endpoint only supports substring `search`, not an exact-key filter
// (posthog.com/docs/api/feature-flags), so this filters the search results
// client-side for an exact match.
func (c *Client) GetFlagByKey(key string) (*FeatureFlag, error) {
	reqURL := fmt.Sprintf("https://%s/api/projects/%s/feature_flags/?search=%s",
		c.host, c.projectID, url.QueryEscape(key))

	var list flagListResponse
	if err := c.do("GET", reqURL, nil, &list); err != nil {
		return nil, err
	}
	for _, flag := range list.Results {
		if flag.Key == key {
			return &flag, nil
		}
	}
	return nil, &NotFoundError{Message: fmt.Sprintf("no flag with key %q (search matched %d flag(s), none exact)", key, len(list.Results))}
}

// ListFlags returns every feature flag in the project, following
// pagination (PostHog's list endpoint returns {count, next, results} —
// `next` is a full URL or empty when done). Used for trig's own
// `linear:TICKET-ID` tag-prefix discovery — DESIGN.md: "list flags, filter
// by tag prefix, extract the ticket id."
func (c *Client) ListFlags() ([]FeatureFlag, error) {
	var all []FeatureFlag
	next := fmt.Sprintf("https://%s/api/projects/%s/feature_flags/", c.host, c.projectID)
	for next != "" {
		var list flagListResponse
		if err := c.do("GET", next, nil, &list); err != nil {
			return nil, err
		}
		all = append(all, list.Results...)
		next = list.Next
	}
	return all, nil
}

// SetTags replaces a flag's full tags array. PostHog's update endpoint has
// no add-one-tag operation — PATCH takes a whole `tags` array
// (posthog.com/docs/api/feature-flags) — so callers needing to add or remove
// a tag must read the flag's current tags first and pass the full merged
// list. A nil tags slice marshals to JSON `null`, which PostHog's API
// rejects with 400 "This field may not be null", so nil is normalized to an
// empty array here rather than trusting every caller to remember.
func (c *Client) SetTags(id int, tags []string) (*FeatureFlag, error) {
	if tags == nil {
		tags = []string{}
	}
	reqURL := fmt.Sprintf("https://%s/api/projects/%s/feature_flags/%d/", c.host, c.projectID, id)
	var flag FeatureFlag
	if err := c.do("PATCH", reqURL, map[string]interface{}{"tags": tags}, &flag); err != nil {
		return nil, err
	}
	return &flag, nil
}
