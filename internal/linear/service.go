package linear

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Khan/genqlient/graphql"
)

type (
	Issue         = IssueFields
	State         = StateFields
	User          = UserFields
	Label         = LabelFields
	Team          = TeamFull
	Cycle         = CycleFields
	Project       = ProjectFields
	Comment       = CommentFields
	Notification  = NotificationFields
	IssueDetail   = GetIssueIssue
	ProjectDetail = GetProjectProject
	CustomView    = CustomViewsCustomViewsCustomViewConnectionNodesCustomView
	Viewer        = GetViewerResponse
)

const pageSize = 50

// Page is one page of a cursor-paginated connection.
type Page[T any] struct {
	Items   []T
	Cursor  string
	HasMore bool
}

// Service is the domain API the TUI talks to. It hides pagination details
// and caches slow-changing metadata (teams, labels, viewer).
type Service struct {
	gql graphql.Client
	ttl time.Duration
	// HTTP is the authenticated client, used to download images hosted by
	// Linear. Other hosts are fetched without credentials.
	HTTP *http.Client

	mu      sync.Mutex
	viewer  cached[*Viewer]
	teams   cached[[]Team]
	labels  cached[[]Label]
	members map[string]*cached[[]User]
}

type cached[T any] struct {
	val T
	at  time.Time
}

func (c *cached[T]) fresh(ttl time.Duration) bool { return !c.at.IsZero() && time.Since(c.at) < ttl }

func NewService(gql graphql.Client) *Service {
	return &Service{gql: gql, ttl: 5 * time.Minute}
}

// Fetch downloads an image (an avatar or logo). Linear-hosted files are
// requested with credentials first and anonymously if that is refused;
// other hosts never receive credentials.
func (s *Service) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	clients := []*http.Client{http.DefaultClient}
	if s.HTTP != nil && (host == "linear.app" || strings.HasSuffix(host, ".linear.app")) {
		clients = []*http.Client{s.HTTP, http.DefaultClient}
	}
	var lastErr error
	for _, c := range clients {
		data, err := fetch(ctx, c, rawURL)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func fetch(ctx context.Context, c *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// Ask for formats we can decode so CDNs don't negotiate AVIF.
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp;q=0.9")
	req.Header.Set("User-Agent", "linear-tui")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// Invalidate drops cached metadata so the next call refetches it.
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewer, s.teams, s.labels = cached[*Viewer]{}, cached[[]Team]{}, cached[[]Label]{}
	s.members = nil
}

func load[T any](s *Service, c *cached[T], fetch func() (T, error)) (T, error) {
	s.mu.Lock()
	if c.fresh(s.ttl) {
		v := c.val
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	v, err := fetch()
	if err != nil {
		return v, Classify(err)
	}
	s.mu.Lock()
	*c = cached[T]{val: v, at: time.Now()}
	s.mu.Unlock()
	return v, nil
}

func (s *Service) Viewer(ctx context.Context) (*Viewer, error) {
	return load(s, &s.viewer, func() (*Viewer, error) { return GetViewer(ctx, s.gql) })
}

func (s *Service) Teams(ctx context.Context) ([]Team, error) {
	return load(s, &s.teams, func() ([]Team, error) {
		r, err := Teams(ctx, s.gql)
		if err != nil {
			return nil, err
		}
		teams := r.Teams.Nodes
		for i := range teams {
			slices.SortFunc(teams[i].States.Nodes, func(a, b State) int {
				if c := stateTypeRank(a.Type) - stateTypeRank(b.Type); c != 0 {
					return c
				}
				return int(a.Position - b.Position)
			})
		}
		return teams, nil
	})
}

func (s *Service) Team(ctx context.Context, id string) (*Team, error) {
	teams, err := s.Teams(ctx)
	if err != nil {
		return nil, err
	}
	for i := range teams {
		if teams[i].Id == id {
			return &teams[i], nil
		}
	}
	return nil, errors.New("team not found")
}

func (s *Service) Labels(ctx context.Context) ([]Label, error) {
	return load(s, &s.labels, func() ([]Label, error) {
		r, err := Labels(ctx, s.gql)
		if err != nil {
			return nil, err
		}
		return r.IssueLabels.Nodes, nil
	})
}

// Members returns a team's members. They are fetched per team because
// nesting them in the teams query exceeds Linear's complexity limit.
func (s *Service) Members(ctx context.Context, teamID string) ([]User, error) {
	s.mu.Lock()
	if s.members == nil {
		s.members = map[string]*cached[[]User]{}
	}
	c := s.members[teamID]
	if c == nil {
		c = &cached[[]User]{}
		s.members[teamID] = c
	}
	s.mu.Unlock()
	return load(s, c, func() ([]User, error) {
		r, err := TeamMembers(ctx, s.gql, teamID)
		if err != nil {
			return nil, err
		}
		return r.Team.Members.Nodes, nil
	})
}

// TeamLabels returns workspace labels plus those belonging to teamID.
func (s *Service) TeamLabels(ctx context.Context, teamID string) ([]Label, error) {
	all, err := s.Labels(ctx)
	if err != nil {
		return nil, err
	}
	var out []Label
	for _, l := range all {
		if l.Team == nil || l.Team.Id == teamID {
			out = append(out, l)
		}
	}
	return out, nil
}

// StateTypeOrder is Linear's workflow order of state types.
var StateTypeOrder = []string{"triage", "backlog", "unstarted", "started", "completed", "canceled"}

func stateTypeRank(t string) int {
	if i := slices.Index(StateTypeOrder, t); i >= 0 {
		return i
	}
	return len(StateTypeOrder)
}

func (s *Service) Issues(ctx context.Context, f Filter, order PaginationOrderBy, cursor string) (Page[Issue], error) {
	r, err := Issues(ctx, s.gql, f.Input(), pageSize, cursor, order)
	if err != nil {
		return Page[Issue]{}, Classify(err)
	}
	c := r.Issues
	return Page[Issue]{Items: c.Nodes, Cursor: c.PageInfo.EndCursor, HasMore: c.PageInfo.HasNextPage}, nil
}

// Search finds issues by identifier or text.
func (s *Service) Search(ctx context.Context, term, cursor string) (Page[Issue], error) {
	if id, ok := ParseIdentifier(term); ok && cursor == "" {
		if d, err := s.Issue(ctx, id); err == nil {
			return Page[Issue]{Items: []Issue{d.IssueFields}}, nil
		}
	}
	return s.Issues(ctx, Filter{Search: term}, PaginationOrderByUpdatedat, cursor)
}

func (s *Service) Issue(ctx context.Context, id string) (*IssueDetail, error) {
	r, err := GetIssue(ctx, s.gql, id)
	if err != nil {
		return nil, Classify(err)
	}
	d := r.Issue
	slices.SortFunc(d.Comments.Nodes, func(a, b Comment) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return &d, nil
}

func (s *Service) IssueByBranch(ctx context.Context, branch string) (*Issue, error) {
	r, err := IssueByBranch(ctx, s.gql, branch)
	if err != nil {
		return nil, Classify(err)
	}
	return r.IssueVcsBranchSearch, nil
}

func (s *Service) Cycles(ctx context.Context, teamID string) ([]Cycle, error) {
	r, err := Cycles(ctx, s.gql, teamID)
	if err != nil {
		return nil, Classify(err)
	}
	cycles := r.Team.Cycles.Nodes
	slices.SortFunc(cycles, func(a, b Cycle) int { return b.StartsAt.Compare(a.StartsAt) })
	return cycles, nil
}

func (s *Service) Projects(ctx context.Context) ([]Project, error) {
	r, err := Projects(ctx, s.gql)
	if err != nil {
		return nil, Classify(err)
	}
	return r.Projects.Nodes, nil
}

func (s *Service) Project(ctx context.Context, id string) (*ProjectDetail, error) {
	r, err := GetProject(ctx, s.gql, id)
	if err != nil {
		return nil, Classify(err)
	}
	p := r.Project
	slices.SortFunc(p.ProjectMilestones.Nodes, func(a, b GetProjectProjectProjectMilestonesProjectMilestoneConnectionNodesProjectMilestone) int {
		return int(a.SortOrder - b.SortOrder)
	})
	return &p, nil
}

func (s *Service) Notifications(ctx context.Context, cursor string) (Page[Notification], error) {
	r, err := Notifications(ctx, s.gql, pageSize, cursor)
	if err != nil {
		return Page[Notification]{}, Classify(err)
	}
	c := r.Notifications
	return Page[Notification]{Items: c.Nodes, Cursor: c.PageInfo.EndCursor, HasMore: c.PageInfo.HasNextPage}, nil
}

func (s *Service) UnreadCount(ctx context.Context) (int, error) {
	r, err := UnreadCount(ctx, s.gql)
	if err != nil {
		return 0, Classify(err)
	}
	return r.NotificationsUnreadCount, nil
}

func (s *Service) CustomViews(ctx context.Context) ([]CustomView, error) {
	r, err := CustomViews(ctx, s.gql)
	if err != nil {
		return nil, Classify(err)
	}
	var out []CustomView
	for _, v := range r.CustomViews.Nodes {
		if v.ModelName == "" || v.ModelName == "Issue" {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Service) CustomViewIssues(ctx context.Context, id, cursor string) (Page[Issue], error) {
	r, err := CustomViewIssues(ctx, s.gql, id, pageSize, cursor)
	if err != nil {
		return Page[Issue]{}, Classify(err)
	}
	c := r.CustomView.Issues
	return Page[Issue]{Items: c.Nodes, Cursor: c.PageInfo.EndCursor, HasMore: c.PageInfo.HasNextPage}, nil
}

var errNotSuccessful = errors.New("linear: request was not successful")

func ok(success bool, err error) error {
	if err != nil {
		return Classify(err)
	}
	if !success {
		return errNotSuccessful
	}
	return nil
}

func (s *Service) CreateIssue(ctx context.Context, in Input) (*Issue, error) {
	r, err := IssueCreate(ctx, s.gql, in)
	if err := ok(r != nil && r.IssueCreate.Success, err); err != nil {
		return nil, err
	}
	return r.IssueCreate.Issue, nil
}

func (s *Service) UpdateIssue(ctx context.Context, id string, in Input) (*Issue, error) {
	r, err := IssueUpdate(ctx, s.gql, id, in)
	if err := ok(r != nil && r.IssueUpdate.Success, err); err != nil {
		return nil, err
	}
	return r.IssueUpdate.Issue, nil
}

func (s *Service) ArchiveIssue(ctx context.Context, id string) error {
	r, err := IssueArchive(ctx, s.gql, id)
	return ok(r != nil && r.IssueArchive.Success, err)
}

func (s *Service) DeleteIssue(ctx context.Context, id string) error {
	r, err := IssueDelete(ctx, s.gql, id)
	return ok(r != nil && r.IssueDelete.Success, err)
}

func (s *Service) CreateRelation(ctx context.Context, issueID, relatedID, kind string) error {
	r, err := IssueRelationCreate(ctx, s.gql, Input{"issueId": issueID, "relatedIssueId": relatedID, "type": kind})
	return ok(r != nil && r.IssueRelationCreate.Success, err)
}

func (s *Service) LinkURL(ctx context.Context, issueID, url, title string) error {
	r, err := AttachmentLinkURL(ctx, s.gql, issueID, url, title)
	return ok(r != nil && r.AttachmentLinkURL.Success, err)
}

func (s *Service) CreateComment(ctx context.Context, issueID, body, parentID string) (*Comment, error) {
	in := Input{"issueId": issueID, "body": body}
	if parentID != "" {
		in["parentId"] = parentID
	}
	r, err := CommentCreate(ctx, s.gql, in)
	if err := ok(r != nil && r.CommentCreate.Success, err); err != nil {
		return nil, err
	}
	return &r.CommentCreate.Comment, nil
}

func (s *Service) UpdateComment(ctx context.Context, id, body string) (*Comment, error) {
	r, err := CommentUpdate(ctx, s.gql, id, Input{"body": body})
	if err := ok(r != nil && r.CommentUpdate.Success, err); err != nil {
		return nil, err
	}
	return &r.CommentUpdate.Comment, nil
}

func (s *Service) DeleteComment(ctx context.Context, id string) error {
	r, err := CommentDelete(ctx, s.gql, id)
	return ok(r != nil && r.CommentDelete.Success, err)
}

func (s *Service) updateNotification(ctx context.Context, id string, in Input) error {
	r, err := NotificationUpdate(ctx, s.gql, id, in)
	return ok(r != nil && r.NotificationUpdate.Success, err)
}

func (s *Service) MarkNotification(ctx context.Context, id string, read bool) error {
	in := Input{"readAt": nil}
	if read {
		in["readAt"] = time.Now().UTC()
	}
	return s.updateNotification(ctx, id, in)
}

func (s *Service) SnoozeNotification(ctx context.Context, id string, until time.Time) error {
	return s.updateNotification(ctx, id, Input{"snoozedUntilAt": until.UTC()})
}

func (s *Service) ArchiveNotification(ctx context.Context, id string) error {
	r, err := NotificationArchive(ctx, s.gql, id)
	return ok(r != nil && r.NotificationArchive.Success, err)
}

// MarkAllRead marks the given notifications read, stopping at the first error.
func (s *Service) MarkAllRead(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.MarkNotification(ctx, id, true); err != nil {
			return err
		}
	}
	return nil
}
