package auth

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuth2Endpoints(t *testing.T) {
	endpoint, err := cmdutil.OAuth2Endpoint("https://wiki.example.org/w/api.php")
	require.NoError(t, err)
	assert.Equal(t, "https://wiki.example.org/w/rest.php/oauth2/authorize", endpoint.AuthURL)
	assert.Equal(t, "https://wiki.example.org/w/rest.php/oauth2/access_token", endpoint.TokenURL)

	_, err = cmdutil.OAuth2Endpoint("http://wiki.example.org/w/api.php")
	assert.Error(t, err)
}

func TestOAuth1URLAddsAndEncodesParameters(t *testing.T) {
	raw, err := oauth1AuthorizeURL("https://wiki.example.org/index.php?title=Special%3AOAuth%2Fauthorize", "consumer-key", "a+b/c")
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "Special:OAuth/authorize", u.Query().Get("title"))
	assert.Equal(t, "a+b/c", u.Query().Get("oauth_token"))
	assert.Equal(t, "consumer-key", u.Query().Get("oauth_consumer_key"))
}

func TestOAuth1JSONTokenResponse(t *testing.T) {
	for _, raw := range []string{
		`{"key":"request-token","secret":"request-secret"}`,
		`{"oauth_token":"request-token","oauth_token_secret":"request-secret"}`,
	} {
		var got oauth1Reply
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
		assert.Equal(t, oauth1Reply{Token: "request-token", Secret: "request-secret"}, got)
	}

	var rejected oauth1Reply
	assert.Error(t, json.Unmarshal([]byte(`{"error":{"code":"bad","info":"no"}}`), &rejected))
}
