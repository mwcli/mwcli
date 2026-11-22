package root

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/discovery"
	"github.com/mwcli/mwcli/internal/sites"
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
	botLogins  int
}

func (w *fakeWiki) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := func(k string) string { return r.Form.Get(k) }
	authed := r.Header.Get("Authorization") == "Bearer "+goodToken || (w.extraToken != "" && r.Header.Get("Authorization") == "Bearer "+w.extraToken)
	if ck, err := r.Cookie("fakesession"); err == nil && ck.Value == "bot" {
		authed = true
	}
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
	case p("meta") == "siteinfo" && p("siprop") == "namespaces|namespacealiases":
		send(map[string]any{"query": map[string]any{
			"namespaces":       map[string]any{"14": map[string]any{"id": 14, "name": "분류", "canonical": "Category"}},
			"namespacealiases": []any{map[string]any{"id": 14, "alias": "Cat"}},
		}})
	case p("action") == "login":
		w.botLogins++
		if (p("lgname") != "Jmnote@mw" && p("lgname") != "Jmnote#mw") || p("lgpassword") != "botpw" || p("lgtoken") != "tok+\\" {
			send(map[string]any{"login": map[string]string{"result": "Failed", "reason": "Incorrect username or password entered."}})
			return
		}
		http.SetCookie(rw, &http.Cookie{Name: "fakesession", Value: "bot", Path: "/"})
		send(map[string]any{"login": map[string]string{"result": "Success", "lgusername": "Jmnote"}})
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
		if p("apprefix") == "Nothing" {
			send(map[string]any{"query": map[string]any{key: []any{}}})
			return
		}
		start := 0
		if c := r.Form.Get("fakecontinue") + r.Form.Get("sroffset"); c != "" {
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
			if key == "search" {
				// Like MediaWiki, search continues with a number.
				resp["continue"] = map[string]any{"sroffset": end, "continue": "-||"}
			} else {
				resp["continue"] = map[string]any{"fakecontinue": fmt.Sprint(end), "continue": "-||"}
			}
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
	t    *testing.T
	dir  string
	api  string
	host string
	base string
	wiki *fakeWiki
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
	return &env{t: t, dir: dir + "/mw", api: srv.URL + "/w/api.php", host: host, base: srv.URL, wiki: w}
}

func (e *env) run(stdin string, args ...string) (string, string, int) {
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	f := &cmdutil.Factory{IOStreams: ios, Config: func() (*config.Config, error) { return config.LoadDir(e.dir) },
		Browse: func(u string) error { e.browsed = u; return nil }, Find: discovery.Find}
	cmd := NewCmdRoot(f)
	cmd.SetArgs(args)
	_, err := cmd.ExecuteC()
	err = f.Hint(err)
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return out.String(), errOut.String(), cmdutil.ExitCode(err)
}

// store saves an account for the test wiki, added as a site named by its host.
func (e *env) store(user string, a *config.Account) *config.Config {
	cfg, err := config.LoadDir(e.dir)
	require.NoError(e.t, err)
	cfg.Sites.Wikis[e.host] = &config.Site{API: e.api}
	cfg.Login(e.host, user, a)
	require.NoError(e.t, cfg.Save())
	return cfg
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
	assert.Equal(t, e.api, cfg.Context)
	require.Contains(t, cfg.Sites.Wikis, e.host, "the first login adds the site")
	assert.Equal(t, e.api, cfg.Sites.Wikis[e.host].API)
	assert.Equal(t, "//fake.test", cfg.Sites.Wikis[e.host].Server)

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
	e.store("Jmnote", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "revoked"})
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
	e.store("Old", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "revoked"})
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

func TestSite(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "site", "add", "other", e.base, "--alias", "o")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Added site other ("+e.base+"/w/api.php)")
	out, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", "o")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Hello", out)

	_, stderr, code = e.run(goodToken, "auth", "login", "--wiki", "other", "--with-token")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	require.Contains(t, cfg.Logins, "other", "logins are stored under the site name")
	assert.Equal(t, "other", cfg.Context)
	out, _, code = e.run("", "auth", "token", "--wiki", "o")
	assert.Equal(t, 0, code)
	assert.Equal(t, goodToken+"\n", out)

	// Logging in elsewhere keeps the default; only `mw site default` changes it.
	_, stderr, code = e.run("", "site", "add", "second", e.api)
	require.Equal(t, 0, code, stderr)
	_, stderr, code = e.run(goodToken, "auth", "login", "--wiki", "second", "--with-token")
	require.Equal(t, 0, code, stderr)
	out, _, _ = e.run("", "site", "default")
	assert.Equal(t, "other\n", out)
	_, stderr, code = e.run("", "site", "default", "second")
	require.Equal(t, 0, code, stderr)
	out, _, _ = e.run("", "site", "default")
	assert.Equal(t, "second\n", out)

	out, _, _ = e.run("", "site", "list")
	assert.Contains(t, out, "  other   "+e.base+"/w/api.php  (o)\n")
	assert.Contains(t, out, "* second  "+e.base+"/w/api.php\n")

	_, stderr, code = e.run("", "site", "remove", "other")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Logged out of other")
	cfg, _ = config.LoadDir(e.dir)
	assert.NotContains(t, cfg.Sites.Wikis, "other")
	assert.NotContains(t, cfg.Logins, "other")

	_, stderr, code = e.run("", "site", "add", "bad/name", e.base)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "invalid site name")
	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", "nosuchsite")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "unknown site \"nosuchsite\"")
}

// An API that was not added gets a hint when it does not answer like MediaWiki.
func TestUnregisteredHint(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "page", "view", "Sandbox", "--api", e.base+"/gone/api.php")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "add it with `mw site add")
}

func TestBuiltinSites(t *testing.T) {
	e := newEnv(t)
	for alias, name := range map[string]string{
		"mw": "www.mediawiki.org", "mediawiki.org": "www.mediawiki.org", "commons": "commons.wikimedia.org",
		"wikipedia": "en.wikipedia.org",
	} {
		_, stderr, code := e.run("", "site", "default", alias)
		require.Equal(t, 0, code, alias+": "+stderr)
		assert.Contains(t, stderr, "Default site is now "+name, alias)
	}
	cfg, _ := config.LoadDir(e.dir)
	cfg.Login("wikimedia", "Alice", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "mw-token"})
	require.NoError(t, cfg.Save())
	for _, h := range []string{"mw", "mediawikiwiki", "www.mediawiki.org", "mediawiki.org"} {
		out, stderr, code := e.run("", "auth", "token", "--wiki", h)
		require.Equal(t, 0, code, h+": "+stderr)
		assert.Equal(t, "mw-token\n", out, h)
	}
	out, _, _ := e.run("", "site", "list", "--builtin")
	assert.Contains(t, out, "commons.wikimedia.org     https://commons.wikimedia.org/w/api.php  (commons)")
	assert.Equal(t, len(sites.Builtin()), strings.Count(out, "\n"))
}

func TestStatusJWTToken(t *testing.T) {
	e := newEnv(t)
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"bedd22b2b0af19db","iat":1795047176.6,"exp":33351955976,"scopes":["basic","editpage"]}`))
	jwt := "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9." + payload + ".c2lnbmF0dXJlWXZ6"
	e.store("Jmnote", &config.Account{Auth: config.AuthOAuth2, OAuthToken: jwt})
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

	for in, want := range map[string]string{
		"분류:사람":                  "분류:사람",
		"category:A":             "category:A",
		"Cat:A":                  "Cat:A",
		"CS1 errors: periodical": "Category:CS1 errors: periodical",
	} {
		e.wiki.lists = nil
		_, stderr, code = e.run("", "page", "list", "--api", e.api, "--category", in)
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, want, e.wiki.lists[0].Get("cmtitle"), in)
	}

	out, _, code = e.run("", "page", "list", "--api", e.api, "--prefix", "Nothing", "--json", "title")
	require.Equal(t, 0, code)
	assert.Equal(t, "[]\n", out)

	e.wiki.lists = nil
	out, stderr, code = e.run("", "page", "list", "--api", e.api, "-S", "jq", "-L", "100", "--json", "title", "--jq", "length")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "7\n", out)
	require.Len(t, e.wiki.lists, 3)
	assert.Equal(t, "6", e.wiki.lists[2].Get("sroffset"))

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

func TestBotPassword(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_PASSWORD", "wrong")
	_, stderr, code := e.run("", "auth", "login", "--api", e.api, "--username", "Jmnote@mw")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "bot password login failed for Jmnote@mw: Incorrect username or password")

	t.Setenv("MW_PASSWORD", "botpw")
	_, stderr, code = e.run("", "auth", "login", "--api", e.api, "--username", "Jmnote@mw")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Logged in to "+e.host+" as Jmnote")
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	a := cfg.Logins[e.host].Users["Jmnote"]
	require.NotNil(t, a)
	assert.Equal(t, config.AuthBotPassword, a.Auth)
	assert.Equal(t, "Jmnote@mw", a.BotName)
	assert.Equal(t, "botpw", a.BotPassword)
	assert.Empty(t, a.Cookies)

	// Each run logs in again: the stored session would expire.
	t.Setenv("MW_PASSWORD", "")
	before := e.wiki.botLogins
	out, stderr, code := e.run("", "auth", "status")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "- Auth: bot password Jmnote@mw")
	assert.Contains(t, out, "- Password: ********")
	assert.NotContains(t, out, "botpw")
	assert.Equal(t, before+1, e.wiki.botLogins)

	_, stderr, code = e.run("New", "page", "edit", "Sandbox", "-F", "-")
	require.Equal(t, 0, code, stderr)
	require.Len(t, e.wiki.edits, 1)

	_, _, code = e.run("", "auth", "token")
	assert.Equal(t, 1, code)

	// Another wiki on the same host never gets the bot password.
	other := e.base + "/other/api.php"
	before = e.wiki.botLogins
	out, stderr, code = e.run("", "page", "view", "Sandbox", "--api", other)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Hello", out)
	_, _, code = e.run("x", "page", "edit", "Sandbox", "--api", other, "-F", "-")
	assert.Equal(t, 4, code)
	assert.Equal(t, before, e.wiki.botLogins)
	require.Len(t, e.wiki.edits, 1)

	cfg.Logins[e.host].Users["Jmnote"].BotPassword = "deleted"
	require.NoError(t, cfg.Save())
	out, _, code = e.run("", "auth", "status")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "The bot password was not accepted")
}

func TestBotPasswordFlag(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_PASSWORD", "botpw")
	_, stderr, code := e.run("", "auth", "login", "--api", e.api, "--username", "Jmnote#mw", "--bot-password")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, config.AuthBotPassword, cfg.Logins[e.host].Users["Jmnote"].Auth)
	assert.Equal(t, "Jmnote#mw", cfg.Logins[e.host].Users["Jmnote"].BotName)

	_, stderr, code = e.run("", "auth", "login", "--api", e.api, "--bot-password")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--bot-password needs --username")
}

func TestStatusEnvTokenForFarmSite(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_TOKEN", goodToken)
	cfg := &config.Config{Context: "handbook", Sites: config.Sites{Wikis: map[string]*config.Site{}},
		Farms:  map[string][]config.FarmSite{"docs": {{Host: e.host, Aliases: []string{"handbook"}, API: e.api}}},
		Logins: map[string]*config.Login{}, Dir: e.dir}
	require.NoError(t, cfg.Save())
	out, stderr, code := e.run("", "auth", "status", "--wiki", "handbook")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "account ")
	assert.Contains(t, out, "MW_TOKEN")
}

func TestSiteAddRejectsFarmName(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Farms: map[string][]config.FarmSite{"wikimedia": nil}, Dir: e.dir}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run("", "site", "add", "wikimedia", "https://new.example.org/w/api.php")
	require.Equal(t, 2, code)
	assert.Contains(t, stderr, "conflicts with a farm name")
}

func TestStatusUsesFarmAPIWhenSiteAliasConflicts(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Dir: e.dir, Sites: config.Sites{Wikis: map[string]*config.Site{"other": {API: "https://other.example.org/api.php", Aliases: []string{"wikimedia"}}}},
		Farms: map[string][]config.FarmSite{"wikimedia": {{Host: e.host, API: e.api}}}, Logins: map[string]*config.Login{}}
	cfg.Login("wikimedia", "Jmnote", &config.Account{Auth: config.AuthOAuth2, OAuthToken: goodToken})
	require.NoError(t, cfg.Save())
	out, stderr, code := e.run("", "auth", "status")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "API: "+e.api)
	assert.NotContains(t, out, "API: https://other.example.org/api.php")
}

func TestFirstLoginRejectsFarmNameCollision(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Dir: e.dir, Farms: map[string][]config.FarmSite{"unrelated.example.org": {{Host: "wiki.example.org"}}}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run(goodToken, "auth", "login", "--wiki", "unrelated.example.org", "--with-token")
	require.Equal(t, 2, code)
	assert.Contains(t, stderr, "conflicts with a farm name")
	cfg, _ = config.LoadDir(e.dir)
	assert.NotContains(t, cfg.Sites.Wikis, "unrelated.example.org")
}

func TestFarmAliasAndAPISelectSameWikiAndPreserveDefault(t *testing.T) {
	e := newEnv(t)
	secondAPI := e.base + "/two/api.php"
	cfg := &config.Config{Dir: e.dir, Sites: config.Sites{Wikis: map[string]*config.Site{}}, Farms: map[string][]config.FarmSite{
		"multi": {
			{Host: e.host, Aliases: []string{"first"}, API: e.api},
			{Host: e.host, Aliases: []string{"second"}, API: secondAPI},
		},
	}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run("", "page", "view", "Main Page", "--wiki", "second", "--api", secondAPI)
	require.Equal(t, 0, code, stderr)

	_, stderr, code = e.run(goodToken, "auth", "login", "--api", secondAPI, "--with-token")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, secondAPI, cfg.Context)
	r, err := sites.Lookup(cfg, cfg.Context)
	require.NoError(t, err)
	assert.Equal(t, secondAPI, r.Site.API)
}

func TestAPILoginWithoutAliasPersistsResolvableDefault(t *testing.T) {
	e := newEnv(t)
	secondAPI := e.base + "/two/api.php"
	cfg := &config.Config{Dir: e.dir, Sites: config.Sites{Wikis: map[string]*config.Site{}}, Farms: map[string][]config.FarmSite{
		"multi": {
			{Host: e.host, API: e.api},
			{Host: e.host, API: secondAPI},
		},
	}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run(goodToken, "auth", "login", "--api", secondAPI, "--with-token")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, secondAPI, cfg.Context)
	_, stderr, code = e.run("", "page", "view", "Main Page")
	require.Equal(t, 0, code, stderr)
}

func TestSiteRemoveClearsAPIDefault(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Dir: e.dir, Context: e.api, Sites: config.Sites{Wikis: map[string]*config.Site{"docs": {API: e.api}}}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run("", "site", "remove", "docs")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Empty(t, cfg.Context)
}
