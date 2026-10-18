package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseURL(t *testing.T) {
	ref, ok := ParseURL("https://github.com/charmbracelet/crush/pull/3869/files")
	if !ok || ref != (Ref{Host: "github.com", Owner: "charmbracelet", Repo: "crush", Number: 3869}) || ref.String() != "crush#3869" {
		t.Fatalf("ref = %+v, %v", ref, ok)
	}
	for _, bad := range []string{"https://github.com/a/b/issues/1", "https://github.com/a/b", "not a url"} {
		if _, ok := ParseURL(bad); ok {
			t.Errorf("ParseURL(%q) should fail", bad)
		}
	}
}

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), Token: "tok", API: srv.URL, GraphQL: srv.URL + "/graphql"}
}

// testdata/pr.json is a real response for charmbracelet/bubbletea#1200.
func TestPullRequestDecodesRealResponse(t *testing.T) {
	fixture, err := os.ReadFile("testdata/pr.json")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/graphql" || r.Header.Get("Authorization") != "Bearer tok" || body.Variables["number"] != float64(1200) {
			t.Errorf("bad request %s %v", r.URL.Path, body.Variables)
		}
		w.Write(fixture)
	})
	pr, err := c.PullRequest(context.Background(), Ref{Owner: "charmbracelet", Repo: "bubbletea", Number: 1200})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status() != "merged" || pr.Author.Name() == "ghost" || pr.Files.TotalCount == 0 || len(pr.TimelineItems.Nodes) == 0 {
		t.Fatalf("decoded = status %s author %v files %d events %d", pr.Status(), pr.Author, pr.Files.TotalCount, len(pr.TimelineItems.Nodes))
	}
	var commits, merged int
	for _, e := range pr.TimelineItems.Nodes {
		switch e.Typename {
		case "PullRequestCommit":
			if e.Commit == nil || e.Commit.AbbreviatedOid == "" {
				t.Fatal("commit not decoded")
			}
			commits++
		case "MergedEvent":
			merged++
		}
	}
	if commits == 0 || merged != 1 {
		t.Fatalf("commits %d merged %d", commits, merged)
	}
}

func TestDiffAndComment(t *testing.T) {
	var commented string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
			if r.Header.Get("Accept") != "application/vnd.github.diff" {
				t.Errorf("accept = %q", r.Header.Get("Accept"))
			}
			io.WriteString(w, "diff --git a/x b/x\n+hi\n")
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/7/comments":
			b, _ := io.ReadAll(r.Body)
			commented = string(b)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		}
	})
	ref := Ref{Owner: "o", Repo: "r", Number: 7}
	d, err := c.Diff(context.Background(), ref)
	if err != nil || !strings.Contains(d, "+hi") {
		t.Fatalf("diff = %q, %v", d, err)
	}
	if err := c.Comment(context.Background(), ref, "lgtm"); err != nil || !strings.Contains(commented, `"lgtm"`) {
		t.Fatalf("comment = %q, %v", commented, err)
	}
	if _, err := c.Diff(context.Background(), Ref{Owner: "x", Repo: "y", Number: 1}); err == nil || !strings.Contains(err.Error(), "Not Found") {
		t.Fatalf("expected Not Found, got %v", err)
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("GH_TOKEN", "envtok")
	if tok, err := ghToken("github.com"); err != nil || tok != "envtok" {
		t.Fatalf("token = %q, %v", tok, err)
	}
}
