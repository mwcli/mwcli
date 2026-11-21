package root

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zetaoss/mwcli/internal/config"
	"github.com/zetaoss/mwcli/pkg/cmdutil"
	"github.com/zetaoss/mwcli/pkg/iostreams"
)

const goodToken = "good-token"

type fakeWiki struct {
	edits []map[string]string
}

func (w *fakeWiki) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p := func(k string) string { return r.Form.Get(k) }
	authed := r.Header.Get("Authorization") == "Bearer "+goodToken
	send := func(v any) { rw.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(rw).Encode(v) }
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && !authed {
		send(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "Invalid access token"}})
		return
	}
	switch {
	case p("meta") == "userinfo":
		if !authed {
			send(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 0, "name": "127.0.0.1", "anon": true, "rights": []string{"read", "edit"}}}})
			return
		}
		send(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 7, "name": "Jmnote", "rights": []string{"read", "edit", "minoredit"}}}})
	case p("meta") == "tokens":
		send(map[string]any{"query": map[string]any{"tokens": map[string]string{p("type") + "token": "tok+\\"}}})
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
	wiki *fakeWiki
}

func newEnv(t *testing.T) *env {
	w := &fakeWiki{}
	srv := httptest.NewServer(w)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("MW_CONFIG_DIR", dir+"/mw")
	t.Setenv("MW_TOKEN", "")
	t.Setenv("MW_HOST", "")
	host, _ := config.HostOf(srv.URL)
	return &env{t: t, dir: dir + "/mw", api: srv.URL + "/w/api.php", host: host, wiki: w}
}

func (e *env) run(stdin string, args ...string) (string, string, int) {
	ios, in, out, errOut := iostreams.Test()
	in.WriteString(stdin)
	f := &cmdutil.Factory{IOStreams: ios, Config: func() (*config.Config, error) { return config.LoadDir(e.dir) }}
	cmd := NewCmdRoot(f)
	cmd.SetArgs(args)
	_, err := cmd.ExecuteC()
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
	assert.Contains(t, out, "- Token: good*****")
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

	t.Setenv("MW_HOST", e.host)
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
	assert.True(t, bytes.Contains([]byte(out), []byte("https://github.com/zetaoss/mwcli/releases/")))
}
