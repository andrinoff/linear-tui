package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const (
	AuthURL  = "https://linear.app/oauth/authorize"
	TokenURL = "https://api.linear.app/oauth/token"
	// Linear expects comma-separated scopes in a single parameter.
	Scopes = "read,write,issues:create,comments:create"
)

// OAuth runs the authorization code flow with PKCE against a loopback
// redirect, so no client secret is needed.
type OAuth struct {
	ClientID string
	Port     int
	AuthURL  string
	TokenURL string
	Timeout  time.Duration
}

func (o *OAuth) Config(redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    o.ClientID,
		RedirectURL: redirect,
		Scopes:      []string{Scopes},
		Endpoint: oauth2.Endpoint{
			AuthURL:   or(o.AuthURL, AuthURL),
			TokenURL:  or(o.TokenURL, TokenURL),
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Login starts a loopback server, calls open with the authorization URL and
// waits for the redirect carrying the code.
func (o *OAuth) Login(ctx context.Context, open func(url string)) (*oauth2.Token, error) {
	if o.ClientID == "" {
		return nil, errors.New("no OAuth client id configured (set oauth.client_id or LINEAR_OAUTH_CLIENT_ID)")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", o.Port))
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://%s/callback", ln.Addr())
	cfg := o.Config(redirect)

	verifier := oauth2.GenerateVerifier()
	state := randomState()
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "consent"))

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var res result
		switch {
		case q.Get("state") != state:
			res.err = errors.New("oauth state mismatch")
		case q.Get("error") != "":
			res.err = fmt.Errorf("authorization denied: %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("no authorization code in callback")
		default:
			res.code = q.Get("code")
		}
		msg := "Logged in to Linear. You can close this tab and return to the terminal."
		if res.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			msg = "Login failed: " + html.EscapeString(res.err.Error())
		}
		fmt.Fprintf(w, "<!doctype html><title>linear-tui</title><p>%s</p>", msg)
		select {
		case done <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	open(authURL)

	timeout := o.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		return cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	}
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
