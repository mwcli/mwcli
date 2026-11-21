package interwiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookup(t *testing.T) {
	host, api, ok := Lookup("commons")
	assert.True(t, ok)
	assert.Equal(t, "commons.wikimedia.org", host)
	assert.Equal(t, "https://commons.wikimedia.org/w/api.php", api)

	host, _, ok = Lookup("MW")
	assert.True(t, ok)
	assert.Equal(t, "www.mediawiki.org", host)

	for _, name := range []string{"google", "hrwiki", "fandom", "wikinews", "commons.wikimedia.org", ""} {
		_, _, ok := Lookup(name)
		assert.False(t, ok, name)
	}
	assert.Len(t, All(), 21)

	api, ok = APIForHost("tmbw.net")
	assert.True(t, ok)
	assert.Equal(t, "https://tmbw.net/wiki/api.php", api)
}
