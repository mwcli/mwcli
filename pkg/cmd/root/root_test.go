package root

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/discovery"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodToken = "good-token"

type fakeWiki struct {
	edits      []map[string]string
	extraToken string
	rvprop     string
	prop       string
	lists      []url.Values
}

func (w *fakeWiki) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := func(k string) string { return r.Form.Get(k) }
	authed := r.Header.Get("Authorization") == "Bearer "+goodToken || (w.extraToken != "" && r.Header.Get("Authorization") == "Bearer "+w.extraToken)
	send := func(v any) { rw.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(rw).Encode(v) }
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && !authed {
		send(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "Invalid access token"}})
		return
	}
	if r.URL.Path == "/gone/api.php" {
		http.NotFound(rw, r)
		return
	}
	switch {
	case p("meta") == "siteinfo":
		send(map[string]any{"query": map[string]any{"general": map[string]string{"sitename": "Fake", "generator": "MediaWiki 1.43.1", "server": "//fake.test", "script": "/w/index.php"}}})
	case p("meta") == "userinfo":
		if !authed {
			send(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 0, "name": "127.0.0.1", "anon": true, "rights": []string{"read", "edit"}}}})
			return
		}
		send(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 7, "name": "Jmnote", "rights": []string{"read", "edit", "minoredit"}}}})
	case p("meta") == "tokens":
		send(map[string]any{"query": map[string]any{"tokens": map[string]string{p("type") + "token": "tok+\\"}}})
	case p("list") != "":
		w.lists = append(w.lists, r.Form)
		key := p("list")
		all := []any{}
		for i := 1; i <= 7; i++ {
			all = append(all, map[string]any{"pageid": i, "ns": 0, "title": fmt.Sprintf("P%d", i)})
		}
		start := 0
		if c := r.Form.Get("fakecontinue"); c != "" {
			fmt.Sscan(c, &start)
		}
		n := 3
		if l := r.Form.Get(map[string]string{"allpages": "ap", "categorymembers": "cm", "search": "sr", "backlinks": "bl", "embeddedin": "ei"}[key] + "limit"); l != "" {
			fmt.Sscan(l, &n)
			n = min(n, 3)
		}
		end := min(start+n, len(all))
		q := map[string]any{key: all[start:end]}
		if key == "search" {
			q["searchinfo"] = map[string]any{"totalhits": 7}
		}
		resp := map[string]any{"query": q}
		if end < len(all) {
			resp["continue"] = map[string]string{"fakecontinue": fmt.Sprint(end), "continue": "-||"}
		}
		send(resp)
	case p("prop") == "categories" && p("pageids") == "7":
		if p("clcontinue") != "7|B" || p("continue") != "||info|revisions" {
			send(map[string]any{"error": map[string]string{"code": "badcontinue", "info": "bad continue"}})
			return
		}
		send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"pageid": 7,
			"categories": []any{map[string]any{"ns": 14, "title": "Category:C"}}}}}})
	case strings.HasPrefix(p("prop"), "info|revisions"):
		w.rvprop, w.prop = p("rvprop"), p("prop")
		if p("titles") == "Missing" {
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": "Missing", "missing": true}}}})
			return
		}
		rev := map[string]any{"revid": 42, "parentid": 41, "timestamp": "2026-11-22T00:00:00Z", "user": "Jmnote", "comment": "fix", "size": 5,
			"slots": map[string]any{"main": map[string]string{"content": "Hello"}}}
		send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{
			"title": p("titles"), "pageid": 7, "ns": 0, "fullurl": "https://fake.test/wiki/" + p("titles"), "contentmodel": "wikitext",
			"length": 5, "touched": "2026-11-22T00:00:01Z",
			"protection": []any{map[string]string{"type": "edit", "level": "sysop", "expiry": "infinity"}},
			"categories": []any{map[string]any{"ns": 14, "title": "Category:A"}, map[string]any{"ns": 14, "title": "Category:B"}},
			"revisions":  []any{rev},
		}}}, "continue": map[string]string{"clcontinue": "7|B", "continue": "||info|revisions"}})
	case p("prop") == "revisions":
		if p("titles") == "Missing" {
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"missing": true}}}})
			return
		}
		send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"revisions": []any{map[string]any{"slots": map[string]any{"main": map[string]string{"content": "Hello"}}}}}}}})
	case p("action") == "edit":
		if p("assert") != "user" || !authed {
			send(map[string]any{"error": map[string]string{"code": "assertuserfailed", "info": "not logged in"}})
			return
		}
		w.edits = append(w.edits, map[string]string{"title": p("title"), "text": p("text"), "summary": p("summary"), "token": p("token")})
		send(map[string]any{"edit": map[string]string{"result": "Success", "title": p("title")}})
	default:
		send(map[string]any{"error": map[string]string{"code": "badvalue", "info": "unknown"}})
	}
}

type env struct {
	t     *testing.T
	dir   string
	api   string
	host  string
	base  string
	cache *discovery.Cache
	wiki  *fakeWiki
	// browsed is the last URL --web opened.
	browsed string
}

func newEnv(t *testing.T) *env {
	w := &fakeWiki{}
	srv := httptest.NewServer(w)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("MW_CONFIG_DIR", dir+"/mw")
	t.Setenv("MW_TOKEN", "")
	t.Setenv("MW_WIKI", "")
	host, _ := config.HostOf(srv.URL)
	cache := &discovery.Cache{Dir: dir + "/cache", Base: func(string) string { return srv.URL }, Now: time.Now}
	return &env{t: t, dir: dir + "/mw", api: srv.URL + "/w/api.php", host: host, base: srv.URL, cache: cache, wiki: w}
}

func (e *env) run(stdin string, args ...string) (string, string, int) {
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	f := &cmdutil.Factory{IOStreams: ios, Config: func() (*config.Config, error) { return config.LoadDir(e.dir) }, Discovery: e.cache,
		Browse: func(u string) error { e.browsed = u; return nil }}
	cmd := NewCmdRoot(f)
	cmd.SetArgs(args)
	_, err := cmd.ExecuteC()
	f.Forget(err)
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return out.String(), errOut.String(), cmdutil.ExitCode(err)
}

func (e *env) login() {
	_, stderr, code := e.run(goodToken+"\n", "auth", "login", "--api", e.api, "--with-token")
	require.Equal(e.t, 0, code, stderr)
}

func TestLoginStatusTokenLogout(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "auth", "status")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "not logged into any wikis")

	_, stderr, code = e.run("bad\n", "auth", "login", "--api", e.api, "--with-token")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "check token")

	e.login()
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, e.host, cfg.DefaultHost)
	assert.Equal(t, e.api, cfg.API(e.host))

	out, stderr, code := e.run("", "auth", "status")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "✓ Logged in to "+e.host+" account Jmnote")
	assert.Contains(t, out, "- Active account: true")
	assert.Contains(t, out, "- Token: ********************************oken\n")
	assert.NotContains(t, out, goodToken)
	assert.Contains(t, out, "- Can: edit · Cannot: create, move, delete, protect")
	assert.Contains(t, out, "- Rights: edit, minoredit, read (3)")

	out, _, _ = e.run("", "auth", "status", "--show-token")
	assert.Contains(t, out, "- Token: "+goodToken)

	out, _, code = e.run("", "auth", "token")
	assert.Equal(t, 0, code)
	assert.Equal(t, goodToken+"\n", out)

	_, stderr, code = e.run("", "auth", "logout")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Logged out of "+e.host+" account Jmnote")
	_, _, code = e.run("", "auth", "status")
	assert.Equal(t, 1, code)
}

func TestStatusInvalidToken(t *testing.T) {
	e := newEnv(t)
	cfg, _ := config.LoadDir(e.dir)
	cfg.Login(e.host, e.api, "Jmnote", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "revoked"})
	require.NoError(t, cfg.Save())
	out, _, code := e.run("", "auth", "status")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "X Failed to log in to "+e.host+" account Jmnote")
	assert.Contains(t, out, "The token is invalid.")
}

func TestEnvToken(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_TOKEN", goodToken)
	_, stderr, code := e.run(goodToken, "auth", "login", "--api", e.api, "--with-token")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "MW_TOKEN")

	t.Setenv("MW_WIKI", e.host)
	cfg, _ := config.LoadDir(e.dir)
	cfg.Login(e.host, e.api, "Old", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "revoked"})
	require.NoError(t, cfg.Save())
	out, _, _ := e.run("", "auth", "status", "--active")
	assert.Contains(t, out, "account Jmnote (MW_TOKEN)")
	assert.NotContains(t, out, "Old")
}

func TestPageViewEdit(t *testing.T) {
	e := newEnv(t)
	out, _, code := e.run("", "page", "view", "Sandbox", "--api", e.api)
	assert.Equal(t, 0, code)
	assert.Equal(t, "Hello", out)

	_, stderr, code := e.run("", "page", "view", "Missing", "--api", e.api)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist")

	_, _, code = e.run("", "page", "edit", "Sandbox", "--api", e.api, "--text", "x")
	assert.Equal(t, 4, code)

	e.login()
	_, _, code = e.run("", "page", "edit", "Sandbox", "--text", "x", "-F", "-")
	assert.Equal(t, 2, code)

	_, stderr, code = e.run("New text", "page", "edit", "Sandbox", "-F", "-", "-s", "sum")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Updated Sandbox on "+e.host)
	require.Len(t, e.wiki.edits, 1)
	assert.Equal(t, map[string]string{"title": "Sandbox", "text": "New text", "summary": "sum", "token": "tok+\\"}, e.wiki.edits[0])
}

func TestApi(t *testing.T) {
	e := newEnv(t)
	e.login()
	out, _, code := e.run("", "api", "action=query", "-f", "meta=userinfo")
	require.Equal(t, 0, code)
	var v map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	assert.Equal(t, "Jmnote", v["query"].(map[string]any)["userinfo"].(map[string]any)["name"])

	out, _, code = e.run("", "api", "action=nonsense")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "badvalue")

	_, stderr, code := e.run("", "api", "-X", "POST", "action=edit", "assert=user", "title=A", "-F", "text=@-")
	assert.Equal(t, 0, code, stderr)
	require.Len(t, e.wiki.edits, 1)
	assert.Equal(t, "tok+\\", e.wiki.edits[0]["token"])

	_, _, code = e.run("", "api", "meta=userinfo")
	assert.Equal(t, 2, code)

	_, _, code = e.run("", "page", "view")
	assert.Equal(t, 2, code)
}

func TestVersion(t *testing.T) {
	e := newEnv(t)
	out, _, code := e.run("", "version")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasPrefix(out, "mw version "))
	out2, _, _ := e.run("", "--version")
	assert.Equal(t, out, out2)
	assert.True(t, bytes.Contains([]byte(out), []byte("https://github.com/mwcli/mwcli/releases/")))
}

func TestDiscovery(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", "other.test")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Hello", out)
	s, ok := e.cache.Lookup("other.test")
	require.True(t, ok)
	assert.Equal(t, e.base+"/w/api.php", s.API)

	_, stderr, code = e.run(goodToken, "auth", "login", "--wiki", "other.test", "--with-token")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	require.Contains(t, cfg.Hosts, e.host, "stored under the host of the discovered API")
	assert.NotContains(t, cfg.Hosts, "other.test")
	assert.Empty(t, cfg.Hosts[e.host].API, "discovered API stays in the cache")
	out, _, code = e.run("", "auth", "token", "--wiki", "other.test")
	assert.Equal(t, 0, code)
	assert.Equal(t, goodToken+"\n", out)
	out, _, _ = e.run("", "auth", "status")
	assert.Contains(t, out, "- API: "+e.base+"/w/api.php")

	e.cache.Invalidate("gone.test")
	require.NoError(t, os.MkdirAll(e.cache.Dir+"/discovery/gone.test", 0o755))
	require.NoError(t, os.WriteFile(e.cache.Dir+"/discovery/gone.test/siteinfo.json",
		[]byte(`{"api":"`+e.base+`/gone/api.php","checked":"`+time.Now().UTC().Format(time.RFC3339)+`"}`), 0o644))
	_, ok = e.cache.Lookup("gone.test")
	require.True(t, ok)
	_, _, code = e.run("", "page", "view", "Sandbox", "--wiki", "gone.test")
	assert.Equal(t, 1, code)
	_, ok = e.cache.Lookup("gone.test")
	assert.False(t, ok, "a 404 drops the cached API")
	out, _, code = e.run("", "page", "view", "Sandbox", "--wiki", "gone.test")
	assert.Equal(t, 0, code)
	assert.Equal(t, "Hello", out)
}

func TestInterwikiPrefix(t *testing.T) {
	e := newEnv(t)
	cfg, _ := config.LoadDir(e.dir)
	cfg.Login("www.mediawiki.org", "", "Alice", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "mw-token"})
	require.NoError(t, cfg.Save())
	for _, h := range []string{"mw", "mediawikiwiki", "www.mediawiki.org"} {
		out, stderr, code := e.run("", "auth", "token", "--wiki", h)
		require.Equal(t, 0, code, h+": "+stderr)
		assert.Equal(t, "mw-token\n", out, h)
	}
	_, stderr, code := e.run("", "auth", "logout", "--wiki", "mw")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Logged out of www.mediawiki.org account Alice")

	out, _, _ := e.run("", "help", "interwiki")
	assert.Contains(t, out, "commons        https://commons.wikimedia.org/w/api.php")
}

func TestStatusJWTToken(t *testing.T) {
	e := newEnv(t)
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"bedd22b2b0af19db","iat":1795047176.6,"exp":33351955976,"scopes":["basic","editpage"]}`))
	jwt := "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9." + payload + ".c2lnbmF0dXJlWXZ6"
	cfg, _ := config.LoadDir(e.dir)
	cfg.Login(e.host, e.api, "Jmnote", &config.Account{Auth: config.AuthOAuth2, OAuthToken: jwt})
	require.NoError(t, cfg.Save())
	e.wiki.extraToken = jwt

	out, stderr, code := e.run("", "auth", "status")
	require.Equal(t, 0, code, stderr+out)
	assert.Contains(t, out, "- Token: ********************************WXZ6 (client bedd22b2, issued 2026-11-19)\n")
	assert.Contains(t, out, "- Grants: basic, editpage\n")
	assert.NotContains(t, out, "eyJ0")

	out, _, _ = e.run("", "auth", "status", "-t")
	assert.Contains(t, out, "- Token: "+jwt+" (client bedd22b2")
}

func TestPageViewJSON(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "page", "view", "Sandbox", "--api", e.api, "--json", "pageid,url,revision,protection")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `{"pageid":7,"url":"https://fake.test/wiki/Sandbox",
		"revision":{"revid":42,"parentid":41,"timestamp":"2026-11-22T00:00:00Z","user":"Jmnote","comment":"fix","size":5},
		"protection":[{"type":"edit","level":"sysop","expiry":"infinity"}]}`, out)
	assert.NotContains(t, e.wiki.rvprop, "content")
	assert.Equal(t, "info|revisions", e.wiki.prop)

	out, _, code = e.run("", "page", "view", "Sandbox", "--api", e.api, "--json", "categories,content", "--jq", ".categories[]")
	assert.Equal(t, 0, code)
	assert.Equal(t, "Category:A\nCategory:B\nCategory:C\n", out)
	assert.Contains(t, e.wiki.rvprop, "content")

	out, _, _ = e.run("", "page", "view", "Sandbox", "--api", e.api, "--json", "content", "--jq", ".content")
	assert.Equal(t, "Hello\n", out)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--api", e.api, "--json")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "Specify one or more comma-separated fields")
	assert.Contains(t, stderr, "categories")

	for _, args := range [][]string{{"--json="}, {"--json", ""}, {"--web", "--json="}} {
		out, stderr, code = e.run("", append([]string{"page", "view", "Sandbox", "--api", e.api}, args...)...)
		assert.Equal(t, 2, code, args)
		assert.Empty(t, out, args)
		assert.Contains(t, stderr, "Specify one or more comma-separated fields", args)
	}
	assert.Empty(t, e.browsed)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--api", e.api, "--json", "nope")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `Unknown JSON field: "nope"`)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--api", e.api, "--jq", ".")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "without specifying `--json`")

	_, stderr, code = e.run("", "page", "view", "Missing", "--api", e.api, "--json", "title")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist")
}

func TestPageViewWeb(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "page", "view", "Main Page", "--api", e.api, "--web")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, e.base+"/w/index.php?title=Main+Page", e.browsed)

	_, _, code = e.run("", "page", "view", "Main Page", "--api", e.api, "--web", "--json", "title")
	assert.Equal(t, 2, code)
}

func TestPageList(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "page", "list", "--api", e.api, "-L", "5")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "P1\nP2\nP3\nP4\nP5\n", out)
	require.Len(t, e.wiki.lists, 2)
	assert.Equal(t, "allpages", e.wiki.lists[0].Get("list"))
	assert.Equal(t, "5", e.wiki.lists[0].Get("aplimit"))
	assert.Equal(t, "3", e.wiki.lists[1].Get("fakecontinue"))
	assert.Equal(t, "2", e.wiki.lists[1].Get("aplimit"))

	e.wiki.lists = nil
	out, _, code = e.run("", "page", "list", "--api", e.api, "--category", "Living people", "-n", "0", "--json", "title,pageid", "-L", "2")
	require.Equal(t, 0, code)
	assert.JSONEq(t, `[{"title":"P1","pageid":1},{"title":"P2","pageid":2}]`, out)
	assert.Equal(t, "Category:Living people", e.wiki.lists[0].Get("cmtitle"))
	assert.Equal(t, "0", e.wiki.lists[0].Get("cmnamespace"))

	e.wiki.lists = nil
	_, _, code = e.run("", "page", "list", "--api", e.api, "--category", "분류:사람")
	require.Equal(t, 0, code)
	assert.Equal(t, "분류:사람", e.wiki.lists[0].Get("cmtitle"))

	out, _, code = e.run("", "page", "list", "--api", e.api, "-S", "jq", "-L", "100", "--json", "title", "--jq", "length")
	require.Equal(t, 0, code)
	assert.Equal(t, "7\n", out)

	for flag, list := range map[string]string{"--links-to": "backlinks", "--embeds": "embeddedin", "--prefix": "allpages"} {
		e.wiki.lists = nil
		_, stderr, code = e.run("", "page", "list", "--api", e.api, flag, "X", "-L", "1")
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, list, e.wiki.lists[0].Get("list"), flag)
	}

	_, stderr, code = e.run("", "page", "list", "--api", e.api, "--prefix", "A", "--search", "b")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "specify only one of --prefix, --search")
	_, stderr, code = e.run("", "page", "list", "--api", e.api, "--category", "")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--category needs a value")
	_, _, code = e.run("", "page", "list", "--api", e.api, "-L", "0")
	assert.Equal(t, 2, code)
}
