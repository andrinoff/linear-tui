package linear

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

const Endpoint = "https://api.linear.app/graphql"

var (
	ErrAuth      = errors.New("linear: authentication failed")
	ErrForbidden = errors.New("linear: forbidden")
	ErrRateLimit = errors.New("linear: rate limited")
)

// APIKeyTransport authenticates requests with a personal API key. Linear
// expects the raw key in the Authorization header, without a Bearer prefix.
type APIKeyTransport struct {
	Key  string
	Base http.RoundTripper
}

func (t *APIKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", t.Key)
	return base(t.Base).RoundTrip(req)
}

// RetryTransport retries rate-limited requests with jittered backoff.
type RetryTransport struct {
	Base       http.RoundTripper
	MaxRetries int
	Sleep      func(context.Context, time.Duration) error
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "linear-tui")
	sleep := t.Sleep
	if sleep == nil {
		sleep = ctxSleep
	}
	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := base(t.Base).RoundTrip(req)
		if err != nil || attempt >= t.MaxRetries || !rateLimited(resp) {
			return resp, err
		}
		wait := backoff(attempt, resp.Header)
		resp.Body.Close()
		if err := sleep(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// rateLimited reports whether resp is a rate-limit error, restoring the body
// so the caller can still read it.
func rateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusBadRequest {
		return false
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return bytes.Contains(body, []byte("RATELIMITED"))
}

func backoff(attempt int, h http.Header) time.Duration {
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Requests-Reset"), 10, 64); err == nil {
		if d := time.Until(time.UnixMilli(reset)); d > 0 && d < 30*time.Second {
			return d
		}
	}
	d := time.Duration(1<<attempt) * 500 * time.Millisecond
	return d + rand.N(d/2+1)
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func base(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return http.DefaultTransport
	}
	return rt
}

// NewClient returns a GraphQL client on top of an authenticated transport.
func NewClient(endpoint string, auth http.RoundTripper) graphql.Client {
	if endpoint == "" {
		endpoint = Endpoint
	}
	hc := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &RetryTransport{Base: auth, MaxRetries: 3},
	}
	return graphql.NewClient(endpoint, hc)
}

// Classify maps transport and GraphQL errors to sentinel errors so the UI can
// react (for example, sending the user back to login on ErrAuth).
func Classify(err error) error {
	if err == nil {
		return nil
	}
	var list gqlerror.List
	var httpErr *graphql.HTTPError
	switch {
	case errors.As(err, &httpErr):
		switch httpErr.StatusCode {
		case http.StatusUnauthorized:
			return errors.Join(ErrAuth, err)
		case http.StatusForbidden:
			return errors.Join(ErrForbidden, err)
		case http.StatusTooManyRequests:
			return errors.Join(ErrRateLimit, err)
		}
		list = httpErr.Response.Errors
	case errors.As(err, &list):
	}
	for _, e := range list {
		kind := strings.ToLower(extString(e, "type") + " " + extString(e, "code"))
		switch {
		case strings.Contains(kind, "authentication"):
			return errors.Join(ErrAuth, err)
		case strings.Contains(kind, "forbidden"):
			return errors.Join(ErrForbidden, err)
		case strings.Contains(kind, "ratelimited"):
			return errors.Join(ErrRateLimit, err)
		}
	}
	return err
}

func extString(e *gqlerror.Error, key string) string {
	s, _ := e.Extensions[key].(string)
	return s
}

// Message returns a short human-readable message for err.
func Message(err error) string {
	var list gqlerror.List
	var httpErr *graphql.HTTPError
	switch {
	case errors.Is(err, ErrAuth):
		return "authentication failed, please log in again"
	case errors.Is(err, ErrForbidden):
		return "you don't have permission to do that"
	case errors.Is(err, ErrRateLimit):
		return "rate limited by Linear, try again shortly"
	case errors.As(err, &httpErr) && len(httpErr.Response.Errors) > 0:
		list = httpErr.Response.Errors
	case errors.As(err, &list):
	}
	if len(list) > 0 {
		if msg := extString(list[0], "userPresentableMessage"); msg != "" {
			return msg
		}
		return list[0].Message
	}
	return err.Error()
}
