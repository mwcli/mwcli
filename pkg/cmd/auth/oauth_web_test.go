package auth

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuth2Endpoints(t *testing.T) {
	endpoint, err := oauth2Endpoints("https://wiki.example.org/w/api.php")
	require.NoError(t, err)
	assert.Equal(t, "https://wiki.example.org/w/rest.php/oauth2/authorize", endpoint.AuthURL)
	assert.Equal(t, "https://wiki.example.org/w/rest.php/oauth2/access_token", endpoint.TokenURL)

	_, err = oauth2Endpoints("http://wiki.example.org/w/api.php")
	assert.Error(t, err)
}

func TestOAuth1URLAddsAndEncodesParameters(t *testing.T) {
	raw, err := oauth1URL("https://wiki.example.org/index.php?title=Special%3AOAuth%2Fauthorize", url.Values{"oauth_token": {"a+b/c"}})
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "Special:OAuth/authorize", u.Query().Get("title"))
	assert.Equal(t, "a+b/c", u.Query().Get("oauth_token"))
}
