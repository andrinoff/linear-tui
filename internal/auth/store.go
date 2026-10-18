package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"

	"github.com/andrinoff/linear-tui/internal/linear"
)

const (
	keyringService = "linear-tui"
	keyringAccount = "credentials"
	EnvAPIKey      = "LINEAR_API_KEY"
)

var ErrNoCredentials = errors.New("not logged in")

type Kind string

const (
	KindAPIKey Kind = "api_key"
	KindOAuth  Kind = "oauth"
)

type Credentials struct {
	Kind   Kind          `json:"kind"`
	APIKey string        `json:"api_key,omitempty"`
	Token  *oauth2.Token `json:"token,omitempty"`
	// FromEnv marks credentials read from LINEAR_API_KEY; they are never saved.
	FromEnv bool `json:"-"`
}

// Store persists credentials in the OS keyring, falling back to a 0600 file
// when no keyring is available (for example on headless Linux).
type Store struct {
	// Dir holds the fallback credentials file.
	Dir string
	// Keyring can be replaced in tests.
	Keyring Keyring
	// Warn is called once when falling back to the file store.
	Warn func(string)
}

type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osKeyring struct{}

func (osKeyring) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (osKeyring) Set(s, u, p string) error        { return keyring.Set(s, u, p) }
func (osKeyring) Delete(s, u string) error        { return keyring.Delete(s, u) }

func NewStore(dir string) *Store { return &Store{Dir: dir, Keyring: osKeyring{}} }

func (s *Store) file() string { return filepath.Join(s.Dir, "credentials.json") }

func (s *Store) Load() (*Credentials, error) {
	if key := os.Getenv(EnvAPIKey); key != "" {
		return &Credentials{Kind: KindAPIKey, APIKey: key, FromEnv: true}, nil
	}
	raw, err := s.Keyring.Get(keyringService, keyringAccount)
	if err != nil {
		b, ferr := os.ReadFile(s.file())
		if errors.Is(ferr, fs.ErrNotExist) {
			return nil, ErrNoCredentials
		}
		if ferr != nil {
			return nil, ferr
		}
		raw = string(b)
	}
	var c Credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) Save(c *Credentials) error {
	if c.FromEnv {
		return nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := s.Keyring.Set(keyringService, keyringAccount, string(b)); err == nil {
		_ = os.Remove(s.file())
		return nil
	}
	if s.Warn != nil {
		s.Warn("system keyring unavailable; storing credentials in " + s.file())
		s.Warn = nil
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.file(), b, 0o600)
}

func (s *Store) Delete() error {
	kerr := s.Keyring.Delete(keyringService, keyringAccount)
	ferr := os.Remove(s.file())
	if errors.Is(ferr, fs.ErrNotExist) {
		ferr = nil
	}
	if kerr != nil && !errors.Is(kerr, keyring.ErrNotFound) {
		return errors.Join(kerr, ferr)
	}
	return ferr
}

// Transport returns an authenticated RoundTripper for c. Refreshed OAuth
// tokens are written back to the store.
func (s *Store) Transport(ctx context.Context, c *Credentials, oauth *OAuth) (http.RoundTripper, error) {
	switch c.Kind {
	case KindAPIKey:
		return &linear.APIKeyTransport{Key: c.APIKey}, nil
	case KindOAuth:
		if oauth == nil || c.Token == nil {
			return nil, errors.New("oauth credentials need a client id")
		}
		src := oauth.Config("").TokenSource(ctx, c.Token)
		return &oauth2.Transport{Source: &persistingSource{src: src, last: c.Token.AccessToken, save: func(t *oauth2.Token) {
			_ = s.Save(&Credentials{Kind: KindOAuth, Token: t})
		}}}, nil
	}
	return nil, errors.New("unknown credential kind " + string(c.Kind))
}

type persistingSource struct {
	src  oauth2.TokenSource
	mu   sync.Mutex
	last string
	save func(*oauth2.Token)
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	t, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if t.AccessToken != p.last {
		p.last = t.AccessToken
		p.save(t)
	}
	return t, nil
}
