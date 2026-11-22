package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A wiki that redirects with 307 or 308 must not receive the bot password at
// the redirect target.
func TestPostIsNotRedirected(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusFound} {
		var posts int
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts++
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"tokens": map[string]string{"logintoken": "t+\\"}}})
		}))
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/w/api.php?"+r.URL.RawQuery, status)
		}))
		api := source.URL + "/w/api.php"
		c, err := New(api, WithBotPassword("Alice@bot", "secret"))
		require.NoError(t, err)
		_, err = c.CurrentUser()
		assert.ErrorContains(t, err, "refusing to follow a redirect of a POST request", status)
		assert.Zero(t, posts, status)
		source.Close()
		target.Close()
	}
}

func TestGetIsRedirected(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 0, "name": "1.2.3.4", "anon": true}}})
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/w/api.php?"+r.URL.RawQuery, http.StatusMovedPermanently)
	}))
	defer source.Close()
	c, err := New(source.URL + "/w/api.php")
	require.NoError(t, err)
	u, err := c.CurrentUser()
	require.NoError(t, err)
	assert.True(t, u.Anon)
	assert.Equal(t, target.URL+"/w/api.php", c.Resolved)
}

// A client names itself: the library's User-Agent by default, the caller's
// with WithUserAgent.
func TestUserAgent(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"userinfo":{"id":0,"name":"1.2.3.4","anon":true}}}`))
	}))
	defer srv.Close()
	for _, opts := range [][]Option{nil, {WithUserAgent("mytool/1.0 (me@example.org)")}} {
		c, err := New(srv.URL+"/w/api.php", opts...)
		require.NoError(t, err)
		_, err = c.CurrentUser()
		require.NoError(t, err)
	}
	assert.Equal(t, []string{DefaultUserAgent, "mytool/1.0 (me@example.org)"}, got)
}

// WithCookies sends the session cookies of a password login.
func TestWithCookies(t *testing.T) {
	var cookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ck, err := r.Cookie("session"); err == nil {
			cookie = ck.Value
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":{"userinfo":{"id":7,"name":"Alice"}}}`))
	}))
	defer srv.Close()
	c, err := New(srv.URL+"/w/api.php", WithCookies(map[string]string{"session": "abc"}))
	require.NoError(t, err)
	_, err = c.User()
	require.NoError(t, err)
	assert.Equal(t, "abc", cookie)
}
