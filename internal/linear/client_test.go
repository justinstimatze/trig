package linear

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	original := endpoint
	endpoint = srv.URL
	t.Cleanup(func() { endpoint = original })

	return &Client{apiKey: "test-key", http: &http.Client{Timeout: 5 * time.Second}}
}

func TestDo_HTTPAuthError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	err := c.do("query {}", nil, nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("do() error = %v, want *AuthError", err)
	}
}

func TestDo_GraphQLAuthError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{{"message": "You do not have permission to access this resource"}},
		})
	})

	err := c.do("query {}", nil, nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("do() error = %v, want *AuthError (GraphQL permission error)", err)
	}
}

func TestDo_GraphQLNotFoundError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{{"message": "Entity not found: Issue"}},
		})
	})

	err := c.do("query {}", nil, nil)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("do() error = %v, want *NotFoundError", err)
	}
}

func TestDo_GraphQLGenericError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{{"message": "Something else broke"}},
		})
	})

	err := c.do("query {}", nil, nil)
	if err == nil {
		t.Fatal("do() error = nil, want an error")
	}
	var authErr *AuthError
	var notFound *NotFoundError
	if errors.As(err, &authErr) || errors.As(err, &notFound) {
		t.Errorf("do() misclassified a generic GraphQL error as %v", err)
	}
}

func TestDo_Success(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"identifier": "CUR-515"},
		})
	})

	var out struct {
		Identifier string `json:"identifier"`
	}
	if err := c.do("query {}", nil, &out); err != nil {
		t.Fatalf("do() error = %v, want nil", err)
	}
	if out.Identifier != "CUR-515" {
		t.Errorf("out.Identifier = %q, want %q", out.Identifier, "CUR-515")
	}
}

func TestGetIssueByIdentifier_NullDataIsNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"issue": nil},
		})
	})

	_, err := c.GetIssueByIdentifier("ZZZ-999")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetIssueByIdentifier() error = %v, want *NotFoundError", err)
	}
}

func TestListIssuesByState_Paginates(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req graphQLRequest
		json.NewDecoder(r.Body).Decode(&req)
		after, _ := req.Variables["after"].(string)

		if after == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"issues": map[string]interface{}{
						"nodes": []map[string]interface{}{
							{"id": "1", "identifier": "CUR-1", "team": map[string]string{"id": "team-a"}, "releases": map[string]interface{}{"nodes": []map[string]interface{}{}}},
						},
						"pageInfo": map[string]interface{}{"hasNextPage": true, "endCursor": "cursor-1"},
					},
				},
			})
			return
		}
		if after != "cursor-1" {
			t.Errorf("second page request carried after=%q, want %q", after, "cursor-1")
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"issues": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"id": "2", "identifier": "CUR-2", "team": map[string]string{"id": "team-b"},
							"releases": map[string]interface{}{"nodes": []map[string]interface{}{
								{"stage": map[string]string{"type": "started"}},
								{"stage": map[string]string{"type": "completed"}},
							}},
						},
					},
					"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
				},
			},
		})
	})

	got, err := c.ListIssuesByState("Merged")
	if err != nil {
		t.Fatalf("ListIssuesByState() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("made %d requests, want 2 (one per page)", calls)
	}
	want := []IssueByState{
		{ID: "1", Identifier: "CUR-1", TeamID: "team-a", ReleaseCompleted: false},
		{ID: "2", Identifier: "CUR-2", TeamID: "team-b", ReleaseCompleted: true},
	}
	if len(got) != len(want) {
		t.Fatalf("ListIssuesByState() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListIssuesByState()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGetWorkflowStateByName_Found(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"workflowStates": map[string]interface{}{
					"nodes": []map[string]string{{"id": "state-1", "name": "Dark"}},
				},
			},
		})
	})

	got, err := c.GetWorkflowStateByName("team-a", "Dark")
	if err != nil {
		t.Fatalf("GetWorkflowStateByName() error = %v", err)
	}
	if got.ID != "state-1" || got.Name != "Dark" {
		t.Errorf("GetWorkflowStateByName() = %+v, want {ID: state-1, Name: Dark}", got)
	}
}

func TestGetWorkflowStateByName_NotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"workflowStates": map[string]interface{}{"nodes": []map[string]string{}},
			},
		})
	})

	_, err := c.GetWorkflowStateByName("team-a", "Nonexistent")
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("GetWorkflowStateByName() error = %v, want *NotFoundError", err)
	}
}

func TestSetState_Success(t *testing.T) {
	var gotVars map[string]interface{}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req graphQLRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotVars = req.Variables
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"issueUpdate": map[string]bool{"success": true}},
		})
	})

	if err := c.SetState("issue-1", "state-1"); err != nil {
		t.Fatalf("SetState() error = %v", err)
	}
	if gotVars["issueID"] != "issue-1" || gotVars["stateID"] != "state-1" {
		t.Errorf("SetState() sent variables %+v, want issueID=issue-1 stateID=state-1", gotVars)
	}
}

func TestSetState_ReportedFailure(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"issueUpdate": map[string]bool{"success": false}},
		})
	})

	if err := c.SetState("issue-1", "state-1"); err == nil {
		t.Fatal("SetState() error = nil, want an error when issueUpdate reports success=false")
	}
}

func TestTruncateUTF8_DoesNotSplitAMultiByteRune(t *testing.T) {
	// "世" is 3 bytes (E4 B8 96); 199 ASCII bytes + this rune straddles the
	// 200-byte cutoff, so a plain byte slice would cut it in half.
	s := strings.Repeat("a", 199) + "世" + strings.Repeat("b", 50)

	got := truncateUTF8(s, 200)

	if !utf8.ValidString(got) {
		t.Fatalf("truncateUTF8() produced invalid UTF-8: %q", got)
	}
	if len(got) > 200 {
		t.Errorf("truncateUTF8() = %d bytes, want <= 200", len(got))
	}
	if got != strings.Repeat("a", 199) {
		t.Errorf("truncateUTF8() = %q, want the split rune dropped entirely", got)
	}
}
