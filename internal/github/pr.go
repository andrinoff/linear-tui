package github

import "time"

// Actor is a GitHub user or bot.
type Actor struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
}

func (a *Actor) Name() string {
	if a == nil || a.Login == "" {
		return "ghost"
	}
	return a.Login
}

// PR is the pull request data shown in the PR view.
type PR struct {
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	URL          string    `json:"url"`
	State        string    `json:"state"` // OPEN, CLOSED, MERGED
	IsDraft      bool      `json:"isDraft"`
	CreatedAt    time.Time `json:"createdAt"`
	Additions    int       `json:"additions"`
	Deletions    int       `json:"deletions"`
	ChangedFiles int       `json:"changedFiles"`
	HeadRefName  string    `json:"headRefName"`
	BaseRefName  string    `json:"baseRefName"`
	Author       *Actor    `json:"author"`

	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer struct {
				Actor
				Name string `json:"name"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	LatestReviews struct {
		Nodes []struct {
			State  string `json:"state"`
			Author *Actor `json:"author"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	Commits struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State    string `json:"state"`
					Contexts struct {
						TotalCount int `json:"totalCount"`
						Nodes      []struct {
							Typename   string `json:"__typename"`
							Name       string `json:"name"`
							Conclusion string `json:"conclusion"`
							Status     string `json:"status"`
							Context    string `json:"context"`
							State      string `json:"state"`
						} `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Files struct {
		TotalCount int    `json:"totalCount"`
		Nodes      []File `json:"nodes"`
	} `json:"files"`
	TimelineItems struct {
		Nodes []Event `json:"nodes"`
	} `json:"timelineItems"`
}

type File struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Event is one timeline item; Typename says which fields are set.
type Event struct {
	Typename  string    `json:"__typename"`
	CreatedAt time.Time `json:"createdAt"`
	Actor     *Actor    `json:"actor"`
	Author    *Actor    `json:"author"`
	Body      string    `json:"body"`
	State     string    `json:"state"`

	Commit *struct {
		AbbreviatedOid  string    `json:"abbreviatedOid"`
		MessageHeadline string    `json:"messageHeadline"`
		AuthoredDate    time.Time `json:"authoredDate"`
		Author          struct {
			Name string `json:"name"`
			User *Actor `json:"user"`
		} `json:"author"`
	} `json:"commit"`
	BeforeCommit *struct {
		AbbreviatedOid string `json:"abbreviatedOid"`
	} `json:"beforeCommit"`
	AfterCommit *struct {
		AbbreviatedOid string `json:"abbreviatedOid"`
	} `json:"afterCommit"`
	RequestedReviewer *struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"requestedReviewer"`
	Comments struct {
		Nodes []struct {
			Body      string    `json:"body"`
			Path      string    `json:"path"`
			CreatedAt time.Time `json:"createdAt"`
			Author    *Actor    `json:"author"`
		} `json:"nodes"`
	} `json:"comments"`
}

// Status is the PR state as Linear names it: open, draft, merged, closed.
func (p *PR) Status() string {
	switch {
	case p.State == "MERGED":
		return "merged"
	case p.State == "CLOSED":
		return "closed"
	case p.IsDraft:
		return "draft"
	}
	return "open"
}

// Checks summarizes the head commit's checks: state is SUCCESS, FAILURE,
// PENDING, ERROR, or "" when there are none.
func (p *PR) Checks() (state string, total, failed int) {
	if len(p.Commits.Nodes) == 0 || p.Commits.Nodes[0].Commit.StatusCheckRollup == nil {
		return "", 0, 0
	}
	r := p.Commits.Nodes[0].Commit.StatusCheckRollup
	for _, c := range r.Contexts.Nodes {
		switch c.Conclusion + c.State {
		case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED":
			failed++
		}
	}
	return r.State, r.Contexts.TotalCount, failed
}

const prQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      number title body url state isDraft createdAt additions deletions changedFiles headRefName baseRefName
      author { login avatarUrl }
      reviewRequests(first: 20) { nodes { requestedReviewer { ... on User { login avatarUrl } ... on Team { name } } } }
      latestReviews(first: 20) { nodes { state author { login avatarUrl } } }
      commits(last: 1) { totalCount nodes { commit { statusCheckRollup { state contexts(first: 100) { totalCount nodes {
        __typename ... on CheckRun { name conclusion status } ... on StatusContext { context state } } } } } } }
      files(first: 100) { totalCount nodes { path additions deletions } }
      timelineItems(first: 100, itemTypes: [PULL_REQUEST_COMMIT, HEAD_REF_FORCE_PUSHED_EVENT, REVIEW_REQUESTED_EVENT,
          PULL_REQUEST_REVIEW, ISSUE_COMMENT, MERGED_EVENT, CLOSED_EVENT, REOPENED_EVENT, READY_FOR_REVIEW_EVENT]) {
        nodes {
          __typename
          ... on PullRequestCommit { commit { abbreviatedOid messageHeadline authoredDate author { name user { login avatarUrl } } } }
          ... on HeadRefForcePushedEvent { createdAt actor { login avatarUrl } beforeCommit { abbreviatedOid } afterCommit { abbreviatedOid } }
          ... on ReviewRequestedEvent { createdAt actor { login avatarUrl } requestedReviewer { ... on User { login } ... on Team { name } } }
          ... on PullRequestReview { createdAt state body author { login avatarUrl }
            comments(first: 20) { nodes { body path createdAt author { login avatarUrl } } } }
          ... on IssueComment { createdAt body author { login avatarUrl } }
          ... on MergedEvent { createdAt actor { login avatarUrl } }
          ... on ClosedEvent { createdAt actor { login avatarUrl } }
          ... on ReopenedEvent { createdAt actor { login avatarUrl } }
          ... on ReadyForReviewEvent { createdAt actor { login avatarUrl } }
        }
      }
    }
  }
}`
