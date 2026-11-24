package cmdutil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// Each stored account becomes the client option of its method, and mw's
// requests carry mw's User-Agent.
func TestNewClient(t *testing.T) {
	var ua, auth, cookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, auth = r.Header.Get("User-Agent"), r.Header.Get("Authorization")
		cookie = ""
		if ck, err := r.Cookie("s"); err == nil {
			cookie = ck.Value
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"userinfo":{"id":7,"name":"Alice"}}}`))
	}))
	defer srv.Close()
	for _, c := range []struct {
		account          *config.Account
		auth, cookie     string
		authPrefixOAuth1 bool
	}{
		{nil, "", "", false},
		{&config.Account{Auth: config.AuthOAuth2, OAuthToken: "tok"}, "Bearer tok", "", false},
		{&config.Account{Auth: config.AuthOAuth1, ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}, "", "", true},
		{&config.Account{Auth: config.AuthSession, Cookies: map[string]string{"s": "v"}}, "", "v", false},
	} {
		cl, err := NewClient(srv.URL+"/w/api.php", c.account)
		require.NoError(t, err)
		_, err = cl.CurrentUser(context.Background())
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(ua, "mw/"), ua)
		if c.authPrefixOAuth1 {
			assert.True(t, strings.HasPrefix(auth, "OAuth "), auth)
		} else {
			assert.Equal(t, c.auth, auth)
		}
		assert.Equal(t, c.cookie, cookie)
	}
}

func TestRefreshOAuth2PersistsRotatedTokens(t *testing.T) {
	var ua string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		assert.Equal(t, "/w/rest.php/oauth2/access_token", r.URL.Path)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse refresh form: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		assert.Equal(t, "client-id", r.Form.Get("client_id"))
		assert.Equal(t, "old-refresh", r.Form.Get("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh-access",
			"refresh_token": "fresh-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	cfg, err := config.LoadDir(t.TempDir())
	require.NoError(t, err)
	account := &config.Account{
		Auth:              config.AuthOAuth2,
		OAuthClientID:     "client-id",
		OAuthToken:        "expired-access",
		OAuthRefreshToken: "old-refresh",
		OAuthExpiry:       time.Now().Add(-time.Minute),
		InsecureStorage:   true,
	}
	cfg.Login("wiki", "Alice", account)
	target := Target{Name: "wiki", LoginKey: "wiki", API: srv.URL + "/w/api.php"}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, srv.Client())
	require.NoError(t, refreshOAuth2(ctx, cfg, target, account))
	assert.Equal(t, "fresh-access", account.OAuthToken)
	assert.Equal(t, "fresh-refresh", account.OAuthRefreshToken)
	assert.True(t, account.OAuthExpiry.After(time.Now()))
	assert.True(t, strings.HasPrefix(ua, "mw/"), ua)

	loaded, err := config.LoadDir(cfg.Dir)
	require.NoError(t, err)
	_, saved, ok := loaded.ActiveAccount("wiki")
	require.True(t, ok)
	assert.Equal(t, "fresh-access", saved.OAuthToken)
	assert.Equal(t, "fresh-refresh", saved.OAuthRefreshToken)
}
