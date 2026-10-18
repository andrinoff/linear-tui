package linear

import (
	"regexp"
	"strings"
)

// Filter is the subset of Linear's IssueFilter the TUI exposes.
type Filter struct {
	TeamID       string
	CycleID      string
	ProjectID    string
	AssignedToMe bool
	Unassigned   bool
	AssigneeIDs  []string
	StateTypes   []string
	StateIDs     []string
	LabelIDs     []string
	Priorities   []int
	HasDueDate   bool
	Search       string
}

func (f Filter) IsZero() bool {
	return f.TeamID == "" && f.CycleID == "" && f.ProjectID == "" && !f.AssignedToMe &&
		!f.Unassigned && len(f.AssigneeIDs) == 0 && len(f.StateTypes) == 0 &&
		len(f.StateIDs) == 0 && len(f.LabelIDs) == 0 && len(f.Priorities) == 0 &&
		!f.HasDueDate && f.Search == ""
}

func in[T any](vals []T) Input { return Input{"in": vals} }

func eqID(id string) Input { return Input{"id": Input{"eq": id}} }

// Input converts the filter to a GraphQL IssueFilter. A nil result means
// "no filter".
func (f Filter) Input() Input {
	var and []Input
	if f.TeamID != "" {
		and = append(and, Input{"team": eqID(f.TeamID)})
	}
	if f.CycleID != "" {
		and = append(and, Input{"cycle": eqID(f.CycleID)})
	}
	if f.ProjectID != "" {
		and = append(and, Input{"project": eqID(f.ProjectID)})
	}
	var assignee []Input
	if f.AssignedToMe {
		assignee = append(assignee, Input{"isMe": Input{"eq": true}})
	}
	if f.Unassigned {
		assignee = append(assignee, Input{"null": true})
	}
	if len(f.AssigneeIDs) > 0 {
		assignee = append(assignee, Input{"id": in(f.AssigneeIDs)})
	}
	switch len(assignee) {
	case 0:
	case 1:
		and = append(and, Input{"assignee": assignee[0]})
	default:
		and = append(and, Input{"assignee": Input{"or": assignee}})
	}
	if len(f.StateTypes) > 0 {
		and = append(and, Input{"state": Input{"type": in(f.StateTypes)}})
	}
	if len(f.StateIDs) > 0 {
		and = append(and, Input{"state": Input{"id": in(f.StateIDs)}})
	}
	if len(f.LabelIDs) > 0 {
		and = append(and, Input{"labels": Input{"some": Input{"id": in(f.LabelIDs)}}})
	}
	if len(f.Priorities) > 0 {
		p := make([]float64, len(f.Priorities))
		for i, v := range f.Priorities {
			p[i] = float64(v)
		}
		and = append(and, Input{"priority": in(p)})
	}
	if f.HasDueDate {
		and = append(and, Input{"dueDate": Input{"null": false}})
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		and = append(and, Input{"or": []Input{
			{"title": Input{"containsIgnoreCase": s}},
			{"searchableContent": Input{"contains": s}},
		}})
	}
	switch len(and) {
	case 0:
		return nil
	case 1:
		return and[0]
	}
	return Input{"and": and}
}

var identifierRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{0,6})-(\d+)\b`)

// ParseIdentifier finds an issue identifier like "ENG-123" in s (for example
// a git branch name such as "alice/eng-123-fix-login").
func ParseIdentifier(s string) (string, bool) {
	m := identifierRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return strings.ToUpper(m[1]) + "-" + m[2], true
}
