package posthog

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		apiKey:    "test-key",
		host:      strings.TrimPrefix(srv.URL, "https://"),
		projectID: "123",
		http:      srv.Client(),
	}
}

func TestDo_AuthError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"detail":"missing scope"}`))
	})

	err := c.do("GET", "https://"+c.host+"/x", nil, nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("do() error = %v, want *AuthError", err)
	}
	if authErr.StatusCode != http.StatusForbidden {
		t.Errorf("AuthError.StatusCode = %d, want %d", authErr.StatusCode, http.StatusForbidden)
	}
}

func TestDo_GenericError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`boom`))
	})

	err := c.do("GET", "https://"+c.host+"/x", nil, nil)
	if err == nil {
		t.Fatal("do() error = nil, want an error")
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		t.Errorf("do() classified a 500 as AuthError: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("do() error = %q, want it to mention the status code", err.Error())
	}
}

func TestDo_Success(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"key": "agent-mode"})
	})

	var out struct {
		Key string `json:"key"`
	}
	if err := c.do("GET", "https://"+c.host+"/x", nil, &out); err != nil {
		t.Fatalf("do() error = %v, want nil", err)
	}
	if out.Key != "agent-mode" {
		t.Errorf("out.Key = %q, want %q", out.Key, "agent-mode")
	}
}

func TestGetFlagByKey(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(flagListResponse{
			Results: []FeatureFlag{
				{ID: 1, Key: "agent-mode-v2"}, // substring match, not exact
				{ID: 2, Key: "agent-mode"},
			},
		})
	})

	flag, err := c.GetFlagByKey("agent-mode")
	if err != nil {
		t.Fatalf("GetFlagByKey() error = %v", err)
	}
	if flag.ID != 2 {
		t.Errorf("GetFlagByKey() returned id %d, want the exact-match id 2", flag.ID)
	}
}

func TestGetFlagByKey_NotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(flagListResponse{Results: nil})
	})

	_, err := c.GetFlagByKey("nope")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetFlagByKey() error = %v, want *NotFoundError", err)
	}
}

func TestListFlags_Pagination(t *testing.T) {
	var baseURL string
	pageOnePath := "/api/projects/123/feature_flags/"
	pageTwoPath := "/api/projects/123/feature_flags/page2"

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pageOnePath:
			json.NewEncoder(w).Encode(flagListResponse{
				Results: []FeatureFlag{{ID: 1, Key: "flag-a"}},
				Next:    baseURL + pageTwoPath,
			})
		case pageTwoPath:
			json.NewEncoder(w).Encode(flagListResponse{
				Results: []FeatureFlag{{ID: 2, Key: "flag-b"}},
				Next:    "",
			})
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	baseURL = "https://" + c.host

	flags, err := c.ListFlags()
	if err != nil {
		t.Fatalf("ListFlags() error = %v", err)
	}
	if len(flags) != 2 || flags[0].Key != "flag-a" || flags[1].Key != "flag-b" {
		t.Errorf("ListFlags() = %+v, want both pages concatenated in order", flags)
	}
}

func TestSetTags_NilNormalizedToEmptyArray(t *testing.T) {
	var sawBody []byte
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sawBody, _ = io.ReadAll(r.Body)
		json.NewEncoder(w).Encode(FeatureFlag{ID: 1, Key: "agent-mode", Tags: []string{}})
	})

	if _, err := c.SetTags(1, nil); err != nil {
		t.Fatalf("SetTags() error = %v", err)
	}

	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(sawBody, &body); err != nil {
		t.Fatalf("request body wasn't valid JSON: %v (body: %s)", err, sawBody)
	}
	if body.Tags == nil {
		t.Errorf("SetTags(id, nil) sent tags:null in the request body — PostHog's API rejects this")
	}
}
