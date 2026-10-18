package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/oauth2"
)

type memKeyring struct {
	data map[string]string
	fail bool
}

func (m *memKeyring) Get(s, u string) (string, error) {
	if v, ok := m.data[s+u]; ok && !m.fail {
		return v, nil
	}
	return "", errors.New("not found")
}

func (m *memKeyring) Set(s, u, p string) error {
	if m.fail {
		return errors.New("no keyring")
	}
	m.data[s+u] = p
	return nil
}

func (m *memKeyring) Delete(s, u string) error { delete(m.data, s+u); return nil }

func TestStoreKeyringAndFileFallback(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	for _, fail := range []bool{false, true} {
		warned := false
		s := &Store{Dir: t.TempDir(), Keyring: &memKeyring{data: map[string]string{}, fail: fail}, Warn: func(string) { warned = true }}
		if _, err := s.Load(); !errors.Is(err, ErrNoCredentials) {
			t.Fatalf("empty load err = %v", err)
		}
		if err := s.Save(&Credentials{Kind: KindAPIKey, APIKey: "k"}); err != nil {
			t.Fatal(err)
		}
		if warned != fail {
			t.Fatalf("fail=%v warned=%v", fail, warned)
		}
		c, err := s.Load()
		if err != nil || c.APIKey != "k" {
			t.Fatalf("load = %+v, %v", c, err)
		}
		if err := s.Delete(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(); !errors.Is(err, ErrNoCredentials) {
			t.Fatalf("after delete err = %v", err)
		}
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv(EnvAPIKey, "env-key")
	s := &Store{Dir: t.TempDir(), Keyring: &memKeyring{data: map[string]string{}}}
	c, err := s.Load()
	if err != nil || c.APIKey != "env-key" || !c.FromEnv {
		t.Fatalf("load = %+v, %v", c, err)
	}
}

func TestOAuthPKCE(t *testing.T) {
	var challenge string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, "bad verifier", http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != "the-code" || r.Form.Get("client_id") != "cid" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	o := &OAuth{ClientID: "cid", AuthURL: "https://example.invalid/authorize", TokenURL: tokenSrv.URL}
	tok, err := o.Login(context.Background(), func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		challenge = q.Get("code_challenge")
		if q.Get("code_challenge_method") != "S256" || q.Get("scope") != Scopes {
			t.Errorf("auth url = %s", authURL)
		}
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + q.Get("state"))
			if err == nil {
				resp.Body.Close()
			}
		}()
	})
	if err != nil || tok.AccessToken != "at" {
		t.Fatalf("token = %+v, %v", tok, err)
	}
}

func TestOAuthStateMismatch(t *testing.T) {
	o := &OAuth{ClientID: "cid", AuthURL: "https://example.invalid/a", TokenURL: "https://example.invalid/t"}
	_, err := o.Login(context.Background(), func(authURL string) {
		u, _ := url.Parse(authURL)
		go http.Get(u.Query().Get("redirect_uri") + "?code=x&state=wrong")
	})
	if err == nil {
		t.Fatal("expected state mismatch error")
	}
}

func TestPersistingSource(t *testing.T) {
	var saved []string
	p := &persistingSource{src: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "new"}), last: "old", save: func(t *oauth2.Token) { saved = append(saved, t.AccessToken) }}
	p.Token()
	p.Token()
	if len(saved) != 1 || saved[0] != "new" {
		t.Fatalf("saved = %v", saved)
	}
}
