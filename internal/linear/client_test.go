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
