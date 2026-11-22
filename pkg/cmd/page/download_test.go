package page

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileName(t *testing.T) {
	for _, c := range []struct{ title, model, want string }{
		{"Main Page", "wikitext", "Main Page.wiki"},
		{"Help:Edit/ko", "wikitext", "Help%3AEdit%2Fko.wiki"},
		{`A\B*C?D"E<F>G|H`, "wikitext", "A%5CB%2AC%3FD%22E%3CF%3EG%7CH.wiki"},
		{"100%", "wikitext", "100%25.wiki"},
		{"대문", "wikitext", "대문.wiki"},
		{"Module:Arguments", "Scribunto", "Module%3AArguments.lua"},
		{"MediaWiki:Common.css", "css", "MediaWiki%3ACommon.css"},
		{"User:A/common.JS", "javascript", "User%3AA%2Fcommon.JS"},
		{"Data", "json", "Data.json"},
		{"Flow board", "flow-board", "Flow board.txt"},
		{"Etc.", "wikitext", "Etc%2E.wiki"},
		{".htaccess", "wikitext", "%2Ehtaccess.wiki"},
		{"CON", "wikitext", "%43ON.wiki"},
		{"Nul.txt", "text", "%4Eul.txt"},
		{"Console", "wikitext", "Console.wiki"},
	} {
		assert.Equal(t, c.want, fileName(c.title, c.model), c.title)
	}
}
