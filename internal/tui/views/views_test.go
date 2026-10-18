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
		io.WriteString(w, `{"data":{"team":{"members":{"nodes":[{"id":"u1","active":true}]}}}}`)
	}))
	defer srv.Close()
	ctx := &common.Ctx{Cfg: config.Default(), Theme: theme.New(true), Keys: common.DefaultKeys(),
		Svc: linear.NewService(linear.NewClient(srv.URL, http.DefaultTransport))}
	me := linear.User{Id: "u1", Active: true}
	team := linear.Team{Id: "t1"}
	team.States.Nodes = []linear.State{{Id: "s1"}, {Id: "s2"}}
	iss := linear.Issue{Id: "i1", Title: "Old", Priority: 2, Estimate: 3, State: linear.State{Id: "s1"}, Team: linear.TeamRef{Id: "t1"}, Assignee: &me}
	iss.Labels.Nodes = []linear.Label{{Id: "l1"}}

	f := EditIssueForm(ctx, iss, "desc")
	f.teams = []linear.Team{team}
	f.labels = []linear.Label{{Id: "l1"}, {Id: "l2"}}
	f.cycles["t1"] = nil

	in, err := f.input()
	if err != nil || len(in) != 0 {
		t.Fatalf("unchanged form = %v, %v", in, err)
	}

	f.v.Title = "New"
	f.v.AssigneeID = ""
	f.v.StateID = "s2"
	f.v.Estimate = -1
	f.v.Labels = []string{"l2", "l1"}
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
	if _, ok := in["labelIds"]; !ok {
		t.Error("labelIds should be sent when labels change")
	}
	if len(in) != len(want)+1 {
		t.Errorf("unexpected fields: %v", in)
	}
}

func TestHumanizeType(t *testing.T) {
	if got := humanizeType("issueStatusChanged"); got != "status changed" {
		t.Fatalf("got %q", got)
	}
}
