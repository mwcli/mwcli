package cmdutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		_, err = cl.CurrentUser()
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
