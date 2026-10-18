package views

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andrinoff/linear-tui/internal/config"
	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui/common"
	"github.com/andrinoff/linear-tui/internal/tui/theme"
)

func TestEditFormSendsOnlyChanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"teams":{"nodes":[{"id":"t1","key":"ENG","name":"Eng","states":{"nodes":[{"id":"s1","type":"unstarted"},{"id":"s2","type":"started"}]}}]}}}`)
	}))
	defer srv.Close()
	ctx := &common.Ctx{Cfg: config.Default(), Theme: theme.New(true), Keys: common.DefaultKeys(),
		Svc: linear.NewService(linear.NewClient(srv.URL, http.DefaultTransport))}
	me := linear.User{Id: "u1", Active: true}
	iss := linear.Issue{Id: "i1", Title: "Old", Priority: 2, Estimate: 3, State: linear.State{Id: "s1"}, Team: linear.TeamRef{Id: "t1"}, Assignee: &me}
	iss.Labels.Nodes = []linear.Label{{Id: "l1"}}

	f := EditIssueForm(ctx, iss, "desc")
	in, err := f.input()
	if err != nil || len(in) != 0 {
		t.Fatalf("unchanged form = %v, %v", in, err)
	}

	f.title.SetValue("New")
	f.assignee = nil
	f.state = &f.team.States.Nodes[1]
	f.estimate = -1
	f.labels = append(f.labels, linear.Label{Id: "l2"})
	in, err = f.input()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"title": "New", "assigneeId": nil, "stateId": "s2", "estimate": nil}
	for k, v := range want {
		got, ok := in[k]
		if !ok || got != v {
			t.Errorf("%s = %v (present %v), want %v", k, got, ok, v)
		}
	}
	if ids, _ := in["labelIds"].([]string); len(ids) != 2 {
		t.Errorf("labelIds = %v", in["labelIds"])
	}
	if len(in) != len(want)+1 {
		t.Errorf("unexpected fields: %v", in)
	}

	f.title.SetValue("  ")
	if _, err := f.input(); err == nil {
		t.Error("empty title should be rejected")
	}
}

func TestHumanizeType(t *testing.T) {
	if got := humanizeType("issueStatusChanged"); got != "status changed" {
		t.Fatalf("got %q", got)
	}
}
