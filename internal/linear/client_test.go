package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
)

func newTestService(t *testing.T, h http.HandlerFunc) *Service {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &RetryTransport{
		Base:       &APIKeyTransport{Key: "lin_api_test"},
		MaxRetries: 3,
		Sleep:      func(context.Context, time.Duration) error { return nil },
	}}
	return NewService(graphql.NewClient(srv.URL, hc))
}

func TestAPIKeyHeaderAndRetry(t *testing.T) {
	var calls atomic.Int32
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "lin_api_test" {
			t.Errorf("Authorization = %q, want raw key without Bearer", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "UnreadCount") {
			t.Errorf("retried body lost: %q", body)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"data":{"notificationsUnreadCount":7}}`)
	})
	n, err := svc.UnreadCount(context.Background())
	if err != nil || n != 7 {
		t.Fatalf("UnreadCount = %d, %v", n, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestClassifyAuthError(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"errors":[{"message":"bad key","extensions":{"type":"authentication error","userPresentableMessage":"Invalid API key"}}]}`)
	})
	_, err := svc.UnreadCount(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if got := Message(err); !strings.Contains(got, "authentication") {
		t.Fatalf("Message = %q", got)
	}
}

func TestGraphQLErrorMessage(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errors":[{"message":"x","extensions":{"userPresentableMessage":"Title is required"}}]}`)
	})
	_, err := svc.CreateIssue(context.Background(), Input{"teamId": "t"})
	if got := Message(err); got != "Title is required" {
		t.Fatalf("Message = %q", got)
	}
}

func TestIssuesPagination(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if _, ok := req.Variables["after"]; ok {
			io.WriteString(w, `{"data":{"issues":{"nodes":[{"id":"2","identifier":"ENG-2","state":{"id":"s"},"team":{"id":"t"},"labels":{"nodes":[]}}],"pageInfo":{"hasNextPage":false,"endCursor":"c2"}}}}`)
			return
		}
		io.WriteString(w, `{"data":{"issues":{"nodes":[{"id":"1","identifier":"ENG-1","assignee":null,"state":{"id":"s"},"team":{"id":"t"},"labels":{"nodes":[{"id":"l","name":"bug"}]}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`)
	})
	ctx := context.Background()
	p1, err := svc.Issues(ctx, Filter{AssignedToMe: true}, "", "")
	if err != nil || len(p1.Items) != 1 || !p1.HasMore || p1.Cursor != "c1" {
		t.Fatalf("page1 = %+v, %v", p1, err)
	}
	if p1.Items[0].Assignee != nil || p1.Items[0].Labels.Nodes[0].Name != "bug" {
		t.Fatalf("decoded issue = %+v", p1.Items[0])
	}
	p2, err := svc.Issues(ctx, Filter{}, "", p1.Cursor)
	if err != nil || p2.HasMore || p2.Items[0].Identifier != "ENG-2" {
		t.Fatalf("page2 = %+v, %v", p2, err)
	}
}

func TestFilterInput(t *testing.T) {
	if (Filter{}).Input() != nil {
		t.Fatal("empty filter should be nil")
	}
	got, _ := json.Marshal(Filter{TeamID: "t1"}.Input())
	if string(got) != `{"team":{"id":{"eq":"t1"}}}` {
		t.Fatalf("single = %s", got)
	}
	got, _ = json.Marshal(Filter{AssignedToMe: true, Unassigned: true, Priorities: []int{1}}.Input())
	want := `{"and":[{"assignee":{"or":[{"isMe":{"eq":true}},{"null":true}]}},{"priority":{"in":[1]}}]}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestParseIdentifier(t *testing.T) {
	for in, want := range map[string]string{
		"alice/eng-123-fix-login": "ENG-123",
		"ENG-9":                   "ENG-9",
		"feature/no-ticket":       "",
	} {
		got, _ := ParseIdentifier(in)
		if got != want {
			t.Errorf("ParseIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}
