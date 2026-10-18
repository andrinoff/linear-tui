// Package github reads pull requests from GitHub for the PR view, using
// the token from the GitHub CLI (gh) or GH_TOKEN/GITHUB_TOKEN.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNoToken = errors.New("no GitHub token: install the GitHub CLI and run `gh auth login`, or set GH_TOKEN")

// Ref identifies a pull request.
type Ref struct {
	Host, Owner, Repo string
	Number            int
}

func (r Ref) String() string { return fmt.Sprintf("%s#%d", r.Repo, r.Number) }

// ParseURL parses https://<host>/<owner>/<repo>/pull/<n>.
func ParseURL(raw string) (Ref, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Ref{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return Ref{}, false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil {
		return Ref{}, false
	}
	return Ref{Host: u.Host, Owner: parts[0], Repo: parts[1], Number: n}, true
}

// Client talks to one GitHub host.
type Client struct {
	HTTP  *http.Client
	Token string
	// API is the REST base URL and GraphQL the GraphQL endpoint.
	API, GraphQL string
}

var (
	mu      sync.Mutex
	clients = map[string]*Client{}
	// tokenFor is replaced in tests.
	tokenFor = ghToken
)

// Register sets the client for host (tests and custom setups).
func Register(host string, c *Client) {
	mu.Lock()
	defer mu.Unlock()
	clients[host] = c
}

// For returns a client for host, resolving the token once.
func For(host string) (*Client, error) {
	mu.Lock()
	defer mu.Unlock()
	if c, ok := clients[host]; ok {
		return c, nil
	}
	token, err := tokenFor(host)
	if err != nil {
		return nil, err
	}
	c := &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, Token: token,
		API: "https://api.github.com", GraphQL: "https://api.github.com/graphql"}
	if host != "github.com" {
		c.API, c.GraphQL = "https://"+host+"/api/v3", "https://"+host+"/api/graphql"
	}
	clients[host] = c
	return c, nil
}

func ghToken(host string) (string, error) {
	for _, env := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := os.Getenv(env); t != "" && host == "github.com" {
			return t, nil
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", ErrNoToken
	}
	out, err := exec.Command("gh", "auth", "token", "--hostname", host).Output()
	if t := strings.TrimSpace(string(out)); err == nil && t != "" {
		return t, nil
	}
	return "", ErrNoToken
}

func (c *Client) do(ctx context.Context, method, endpoint, accept string, body any) ([]byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "linear-tui")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		var e struct{ Message string }
		_ = json.Unmarshal(data, &e)
		if e.Message == "" {
			e.Message = resp.Status
		}
		return nil, fmt.Errorf("github: %s", e.Message)
	}
	return data, nil
}

// PullRequest loads the overview data for the PR view.
func (c *Client) PullRequest(ctx context.Context, ref Ref) (*PR, error) {
	data, err := c.do(ctx, http.MethodPost, c.GraphQL, "application/json", map[string]any{
		"query":     prQuery,
		"variables": map[string]any{"owner": ref.Owner, "name": ref.Repo, "number": ref.Number},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Repository struct {
				PullRequest *PR `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("github: %s", resp.Errors[0].Message)
	}
	if resp.Data.Repository.PullRequest == nil {
		return nil, errors.New("github: pull request not found")
	}
	return resp.Data.Repository.PullRequest, nil
}

// Diff returns the PR's unified diff.
func (c *Client) Diff(ctx context.Context, ref Ref) (string, error) {
	data, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.API, ref.Owner, ref.Repo, ref.Number), "application/vnd.github.diff", nil)
	return string(data), err
}

// Comment posts a conversation comment on the PR.
func (c *Client) Comment(ctx context.Context, ref Ref, body string) error {
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.API, ref.Owner, ref.Repo, ref.Number),
		"application/vnd.github+json", map[string]string{"body": body})
	return err
}
