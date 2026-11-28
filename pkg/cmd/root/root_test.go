package root

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MakeNowJust/heredoc"
	"github.com/mwcli/mwcli/internal/apicache"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/internal/discovery"
	"github.com/mwcli/mwcli/internal/sites"
	"github.com/mwcli/mwcli/pkg/cmdutil"
	"github.com/mwcli/mwcli/pkg/iostreams"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

const goodToken = "good-token"

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

type fakeWiki struct {
	edits      []map[string]string
	extraToken string
	rvprop     string
	prop       string
	lists      []url.Values
	botLogins  int
	// editFlags are the flags of the last edit, such as "nocreate minor".
	editFlags string
	created   map[string]bool
	// siteinfos counts general siteinfo requests, which discovery makes.
	siteinfos int
	// maxBody, when set, refuses larger requests with 413, counted in tooLarge.
	maxBody  int64
	tooLarge int
	// uploads are the action=upload requests.
	uploads []uploadRequest
	// rests are the requests to rest.php.
	rests []restRequest
	// writes are the forms of move and delete requests.
	writes []url.Values
	// batches are the numbers of titles of each request for many pages.
	batches []int
	// siprops are the siprop parameters of general siteinfo requests.
	siprops []string
	// pstModels are the contentmodel parameters of pre-save transforms.
	pstModels []string
	// domains are the domain or lgdomain parameters of logins; logouts counts
	// action=logout requests.
	domains []string
	logouts int
}

// uploadRequest is an action=upload request to the fake wiki; Content is the
// start of the file or chunk.
type uploadRequest struct {
	Filename, Offset, FileSize, FileKey, Comment, Text, Token, Field, Content string
	// Chunked is set for a body sent without Content-Length.
	Stash, Ignore, Chunked bool
	Size                   int
}

// restRequest is a request to the fake wiki's rest.php.
type restRequest struct {
	Method, Path, Body, Type string
	Query                    url.Values
	Authed                   bool
}

func (w *fakeWiki) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if w.maxBody > 0 && r.ContentLength > w.maxBody {
		// As nginx in front of a wiki answers a request over its limit.
		w.tooLarge++
		rw.Header().Set("Content-Type", "text/html")
		rw.WriteHeader(http.StatusRequestEntityTooLarge)
		fmt.Fprint(rw, "<html><head><title>413 Request Entity Too Large</title></head><body>nginx</body></html>")
		return
	}
	_ = r.ParseForm()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		_ = r.ParseMultipartForm(32 << 20)
	}
	p := func(k string) string { return r.Form.Get(k) }
	authed := r.Header.Get("Authorization") == "Bearer "+goodToken || (w.extraToken != "" && r.Header.Get("Authorization") == "Bearer "+w.extraToken)
	// The api package checks OAuth 1.0a signatures; here the keys are enough.
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "OAuth ") {
		// Reasons as the OAuth extension gives them.
		reason := ""
		switch {
		case !strings.Contains(h, `oauth_consumer_key="ck"`):
			reason = "Invalid consumer"
		case !strings.Contains(h, `oauth_token="at"`):
			reason = "No approved grant was found for that authorization token."
		case !strings.Contains(h, "oauth_signature="):
			reason = "Invalid signature"
		}
		if reason != "" {
			rw.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(rw).Encode(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "The authorization headers in your request are not valid: " + reason}})
			return
		}
		authed = true
	}
	if ck, err := r.Cookie("fakesession"); err == nil && ck.Value == "bot" {
		authed = true
	}
	send := func(v any) { rw.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(rw).Encode(v) }
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && !authed {
		send(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "Invalid access token"}})
		return
	}
	if r.URL.Path == "/index.php" && r.URL.Query().Get("wiki") != "" {
		// A farm that picks the wiki by query: each wiki has its own API.
		rw.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(rw, `<html><head><link rel="EditURI" type="application/rsd+xml" href="http://%s/%s/api.php?action=rsd"/></head></html>`, r.Host, r.URL.Query().Get("wiki"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/wiki/") {
		// A page of the wiki points to its API, as MediaWiki's pages do.
		rw.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(rw, `<html><head><link rel="EditURI" type="application/rsd+xml" href="http://%s/w/api.php?action=rsd"/></head></html>`, r.Host)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/w/rest.php/") {
		b, _ := io.ReadAll(r.Body)
		w.rests = append(w.rests, restRequest{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.Query(), Body: string(b), Type: r.Header.Get("Content-Type"), Authed: authed})
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/w/rest.php/v1/page/Sandbox/bare", "/w/rest.php/v1/page/A%2FB/bare":
			fmt.Fprintf(rw, `{"id":7,"title":"Sandbox","latest":{"id":303}}`)
		case "/w/rest.php/v1/page":
			rw.WriteHeader(http.StatusCreated)
			fmt.Fprintf(rw, `{"id":8,"echo":%s}`, b)
		case "/w/rest.php/v1/text":
			rw.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(rw, "plain")
		default:
			rw.WriteHeader(http.StatusNotFound)
			fmt.Fprint(rw, `{"errorKey":"rest-nonexistent-title","httpCode":404}`)
		}
		return
	}
	if r.URL.Path == "/gone/api.php" {
		http.NotFound(rw, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/files/") {
		files := map[string]string{
			"/files/Example.jpg": "jpeg-bytes",
			"/files/Shared.png":  "shared-png-bytes",
			"/files/Audio.ogg":   "ogg-bytes",
			"/files/A_B.jpg":     "colon-file-bytes",
			"/files/BadHash.png": "corrupt-bytes",
		}
		if body, ok := files[r.URL.Path]; ok {
			_, _ = io.WriteString(rw, body)
			return
		}
		http.NotFound(rw, r)
		return
	}
	switch {
	case p("meta") == "siteinfo" && p("siprop") == "namespaces|namespacealiases":
		send(map[string]any{"query": map[string]any{
			"namespaces": map[string]any{
				"0":  map[string]any{"id": 0, "name": ""},
				"6":  map[string]any{"id": 6, "name": "파일", "canonical": "File"},
				"12": map[string]any{"id": 12, "name": "도움말", "canonical": "Help"},
				"14": map[string]any{"id": 14, "name": "분류", "canonical": "Category"},
			},
			"namespacealiases": []any{
				map[string]any{"id": 6, "alias": "Image"},
				map[string]any{"id": 14, "alias": "Cat"},
			},
		}})
	case p("action") == "parse" && p("onlypst") != "":
		// The pre-save transform: for wikitext, trailing white space goes and
		// ~~~ signs; JSON, the model of a .json title or page, is reformatted.
		model := p("contentmodel")
		if model == "" && strings.HasSuffix(p("title"), ".json") {
			model = "json"
		}
		w.pstModels = append(w.pstModels, p("contentmodel"))
		if model == "json" {
			var v any
			_ = json.Unmarshal([]byte(p("text")), &v)
			b, _ := json.MarshalIndent(v, "", "\t")
			send(map[string]any{"parse": map[string]string{"text": string(b)}})
			return
		}
		sig := "[[Special:Contributions/127.0.0.1|127.0.0.1]]"
		if authed {
			sig = "[[User:Jmnote|Jmnote]]"
		}
		send(map[string]any{"parse": map[string]string{"text": strings.ReplaceAll(strings.TrimRight(p("text"), " \n"), "~~~", sig)}})
	case p("action") == "clientlogin":
		// Jmnote logs in with pw; TwoFA also needs the code 123456 next.
		if r.Form.Has("loginmessageformat") && p("loginmessageformat") != "wikitext" && p("loginmessageformat") != "html" {
			send(map[string]any{"error": map[string]string{"code": "badvalue", "info": "Unrecognized value for parameter \"loginmessageformat\""}})
			return
		}
		w.domains = append(w.domains, p("domain"))
		pass := func() {
			http.SetCookie(rw, &http.Cookie{Name: "fakesession", Value: "bot", Path: "/"})
			send(map[string]any{"clientlogin": map[string]string{"status": "PASS", "username": "Jmnote"}})
		}
		switch {
		case p("logincontinue") != "" && p("OATHToken") == "123456":
			pass()
		case p("logincontinue") != "":
			send(map[string]any{"clientlogin": map[string]string{"status": "FAIL", "message": "Verification failed."}})
		case p("password") != "pw" || p("logintoken") != "tok+\\":
			send(map[string]any{"clientlogin": map[string]string{"status": "FAIL", "message": "Incorrect username or password entered."}})
		case p("username") == "TwoFA":
			send(map[string]any{"clientlogin": map[string]any{"status": "UI", "message": "Enter a verification code from your authentication device.",
				"requests": []any{map[string]any{"id": "TOTPAuthenticationRequest", "fields": map[string]any{
					"OATHToken":   map[string]string{"type": "string", "label": "Token"},
					"hiddenfield": map[string]string{"type": "hidden"}}}}}})
		default:
			pass()
		}
	case p("action") == "logout":
		if p("token") == "tok+\\" && authed {
			w.logouts++
		}
		send(map[string]any{})
	case p("action") == "login":
		w.domains = append(w.domains, p("lgdomain"))
		w.botLogins++
		if (p("lgname") != "Jmnote@mw" && p("lgname") != "Jmnote#mw") || p("lgpassword") != "botpw" || p("lgtoken") != "tok+\\" {
			send(map[string]any{"login": map[string]string{"result": "Failed", "reason": "Incorrect username or password entered."}})
			return
		}
		http.SetCookie(rw, &http.Cookie{Name: "fakesession", Value: "bot", Path: "/"})
		send(map[string]any{"login": map[string]string{"result": "Success", "lgusername": "Jmnote"}})
	case p("meta") == "siteinfo":
		w.siteinfos++
		w.siprops = append(w.siprops, p("siprop"))
		q := map[string]any{"general": map[string]string{"sitename": "Fake", "generator": "MediaWiki 1.43.1", "server": "//fake.test", "script": "/w/index.php", "articlepath": "/wiki/$1",
			"mainpage": "Main Page", "base": "https://fake.test/wiki/Main_Page", "scriptpath": "/w", "lang": "ko", "phpversion": "8.3.1", "dbtype": "mysql", "dbversion": "10.11.6-MariaDB", "timezone": "Asia/Seoul"}}
		if strings.Contains(p("siprop"), "statistics") {
			q["statistics"] = map[string]int{"pages": 1234567, "articles": 89012, "edits": 3456789, "images": 12, "users": 345, "activeusers": 6, "admins": 2, "jobs": 0}
		}
		if strings.Contains(p("siprop"), "extensions") {
			q["extensions"] = []any{
				map[string]string{"type": "parserhook", "name": "Cite", "version": "", "url": "https://www.mediawiki.org/wiki/Extension:Cite", "license-name": "GPL-2.0-or-later", "license": "/wiki/Special:Version/License/Cite"},
				map[string]string{"type": "skin", "name": "Vector", "version": "1.0.0", "url": "https://www.mediawiki.org/wiki/Skin:Vector"},
			}
		}
		send(map[string]any{"query": q})
	case p("meta") == "userinfo":
		if !authed {
			send(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 0, "name": "127.0.0.1", "anon": true, "rights": []string{"read", "edit"}}}})
			return
		}
		send(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 7, "name": "Jmnote", "rights": []string{"read", "edit", "minoredit"}}}})
	case p("meta") == "tokens":
		send(map[string]any{"query": map[string]any{"tokens": map[string]string{p("type") + "token": "tok+\\"}}})
	case p("list") == "usercontribs":
		w.lists = append(w.lists, r.Form)
		if p("ucuser") != "Jmnote" {
			send(map[string]any{"query": map[string]any{"usercontribs": []any{}}})
			return
		}
		all := []any{
			map[string]any{"revid": 303, "parentid": 302, "pageid": 7, "ns": 0, "title": "Sandbox", "timestamp": "2026-11-22T08:41:00Z", "user": "Jmnote", "comment": "Fix\ttypo\nagain", "size": 120, "sizediff": -3, "minor": true, "new": false, "top": true, "tags": []string{"mw-undo"}},
			map[string]any{"revid": 205, "parentid": 0, "pageid": 9, "ns": 2, "title": "User:Jmnote/새 문서", "timestamp": "2026-11-21T01:02:03Z", "user": "Jmnote", "comment": "New page", "size": 240, "sizediff": 240, "minor": false, "new": true, "top": true},
			map[string]any{"revid": 101, "parentid": 100, "pageid": 7, "ns": 0, "title": "Sandbox", "timestamp": "2026-11-20T00:00:00Z", "user": "Jmnote", "comment": "", "size": 123, "sizediff": 0, "minor": false, "new": false, "top": false},
		}
		start, n := 0, 2
		fmt.Sscan(p("uccontinue"), &start)
		fmt.Sscan(p("uclimit"), &n)
		end := min(start+min(n, 2), len(all))
		resp := map[string]any{"query": map[string]any{"usercontribs": all[start:end]}}
		if end < len(all) {
			resp["continue"] = map[string]any{"uccontinue": fmt.Sprint(end), "continue": "-||"}
		}
		send(resp)
	case p("prop") == "redirects":
		w.lists = append(w.lists, r.Form)
		all := []any{
			map[string]any{"pageid": 10, "ns": 0, "title": "R1"},
			map[string]any{"pageid": 11, "ns": 0, "title": "R2"},
		}
		if p("rdnamespace") == "14" {
			all = []any{map[string]any{"pageid": 12, "ns": 14, "title": "Category:R3"}}
		}
		send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": p("titles"), "redirects": all}}}})
	case p("list") != "":
		w.lists = append(w.lists, r.Form)
		key := p("list")
		if key == "backlinks" && p("blredirect") == "1" {
			if p("blfilterredir") == "redirects" {
				send(map[string]any{"query": map[string]any{"backlinks": []any{
					map[string]any{"pageid": 1, "ns": 0, "title": "Direct"},
					map[string]any{"pageid": 2, "ns": 0, "title": "Redir", "redirect": true, "redirlinks": []any{
						map[string]any{"pageid": 4, "ns": 0, "title": "DoubleRedir"},
					}},
				}}})
				return
			}
			send(map[string]any{"query": map[string]any{"backlinks": []any{
				map[string]any{"pageid": 1, "ns": 0, "title": "Direct"},
				map[string]any{"pageid": 2, "ns": 0, "title": "Redir", "redirect": true, "redirlinks": []any{
					map[string]any{"pageid": 1, "ns": 0, "title": "Direct"},
					map[string]any{"pageid": 3, "ns": 0, "title": "ViaRedir"},
				}},
			}}})
			return
		}
		all := []any{}
		if key == "categorymembers" && p("cmnamespace") == "6" {
			all = []any{
				map[string]any{"pageid": 101, "ns": 6, "title": "File:Example.jpg"},
				map[string]any{"pageid": 102, "ns": 6, "title": "File:Audio.ogg"},
			}
		} else {
			for i := 1; i <= 7; i++ {
				all = append(all, map[string]any{"pageid": i, "ns": 0, "title": fmt.Sprintf("P%d", i)})
			}
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
	case p("prop") == "imageinfo":
		titles := strings.Split(p("titles"), "|")
		var normalized, pages []any
		sum := func(s string) string {
			h := sha1.Sum([]byte(s))
			return hex.EncodeToString(h[:])
		}
		for _, t := range titles {
			n := strings.ReplaceAll(t, "_", " ")
			switch {
			case n == "File:example.jpg":
				normalized = append(normalized, map[string]string{"from": t, "to": "File:Example.jpg"})
				n = "File:Example.jpg"
			case n == "File:Shared.png" || n == "Image:Shared.png":
				normalized = append(normalized, map[string]string{"from": t, "to": "파일:Shared.png"})
				n = "파일:Shared.png"
			}
			switch {
			case strings.Contains(n, "<"):
				pages = append(pages, map[string]any{"title": t, "invalid": true})
			case n == "File:Example.jpg":
				pages = append(pages, map[string]any{
					"title": n, "imagerepository": "local",
					"imageinfo": []any{map[string]any{
						"url":            "http://" + r.Host + "/files/Example.jpg?utm_source=fake&utm_campaign=imageinfo",
						"descriptionurl": "http://" + r.Host + "/wiki/File:Example.jpg",
						"size":           len("jpeg-bytes"), "width": 300, "height": 200,
						"mime": "image/jpeg", "mediatype": "BITMAP",
						"timestamp": "2026-11-22T01:02:03Z", "user": "Jmnote",
						"comment": "Sample\tphoto\nof\nwiki", "sha1": sum("jpeg-bytes"),
					}},
				})
			case n == "파일:Shared.png":
				pages = append(pages, map[string]any{
					"title": n, "missing": true, "known": true, "imagerepository": "shared",
					"imageinfo": []any{map[string]any{
						"url":            "http://" + r.Host + "/files/Shared.png?utm_source=fake",
						"descriptionurl": "//commons.example/wiki/File:Shared.png",
						"size":           len("shared-png-bytes"), "width": 64, "height": 64,
						"mime": "image/png", "mediatype": "BITMAP",
						"timestamp": "2026-11-20T00:00:00Z", "user": "CommonsUser",
						"comment": "From commons", "sha1": sum("shared-png-bytes"),
					}},
				})
			case n == "File:Audio.ogg":
				pages = append(pages, map[string]any{
					"title": n, "imagerepository": "local",
					"imageinfo": []any{map[string]any{
						"url":            "http://" + r.Host + "/files/Audio.ogg",
						"descriptionurl": "http://" + r.Host + "/wiki/File:Audio.ogg",
						"size":           len("ogg-bytes"), "width": 0, "height": 0,
						"mime": "application/ogg", "mediatype": "AUDIO",
						"timestamp": "2026-11-21T12:00:00Z", "user": "",
						"comment": "", "sha1": sum("ogg-bytes"),
					}},
				})
			case n == "File:A: B.jpg":
				pages = append(pages, map[string]any{
					"title": n, "imagerepository": "local",
					"imageinfo": []any{map[string]any{
						"url":            "http://" + r.Host + "/files/A_B.jpg",
						"descriptionurl": "http://" + r.Host + "/wiki/File:A:_B.jpg",
						"size":           len("colon-file-bytes"), "width": 10, "height": 10,
						"mime": "image/jpeg", "mediatype": "BITMAP",
						"timestamp": "2026-11-22T05:00:00Z", "user": "Jmnote",
						"comment": "Colon", "sha1": sum("colon-file-bytes"),
					}},
				})
			case n == "File:BadHash.png":
				pages = append(pages, map[string]any{
					"title": n, "imagerepository": "local",
					"imageinfo": []any{map[string]any{
						"url":            "http://" + r.Host + "/files/BadHash.png",
						"descriptionurl": "http://" + r.Host + "/wiki/File:BadHash.png",
						"size":           len("corrupt-bytes"), "width": 10, "height": 10,
						"mime": "image/png", "mediatype": "BITMAP",
						"timestamp": "2026-11-22T06:00:00Z", "user": "Jmnote",
						"comment": "Bad", "sha1": strings.Repeat("0", 40),
					}},
				})
			case n == "File:Hidden.png":
				pages = append(pages, map[string]any{
					"title": n, "imagerepository": "local",
					"imageinfo": []any{map[string]any{"timestamp": "2026-11-22T07:00:00Z"}},
				})
			default:
				pages = append(pages, map[string]any{"title": n, "missing": true, "imagerepository": ""})
			}
		}
		resp := map[string]any{"query": map[string]any{"normalized": normalized, "pages": pages}}
		if len(titles) == 1 && titles[0] == "File:Example.jpg" {
			resp["continue"] = map[string]string{"iistart": "2005-01-01T00:00:00Z", "continue": "||"}
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
		if p("titles") == "Alias" && p("redirects") == "1" {
			rev := map[string]any{"revid": 43, "parentid": 42, "timestamp": "2026-11-22T00:00:00Z", "user": "Jmnote", "comment": "target", "size": 11,
				"slots": map[string]any{"main": map[string]string{"content": "Target text"}}}
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{
				"title": "Target", "pageid": 8, "ns": 0, "fullurl": "https://fake.test/wiki/Target", "contentmodel": "wikitext",
				"length": 11, "touched": "2026-11-22T00:00:01Z", "revisions": []any{rev},
			}}}})
			return
		}
		if p("titles") == "Alias" {
			rev := map[string]any{"revid": 40, "parentid": 0, "timestamp": "2026-11-22T00:00:00Z", "user": "Jmnote", "comment": "redir", "size": 20,
				"slots": map[string]any{"main": map[string]string{"content": "#REDIRECT [[Target]]"}}}
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{
				"title": "Alias", "pageid": 18, "ns": 0, "fullurl": "https://fake.test/wiki/Alias", "contentmodel": "wikitext",
				"length": 20, "touched": "2026-11-22T00:00:01Z", "redirect": true, "revisions": []any{rev},
			}}}})
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
	case p("prop") == "revisions" && p("rvprop") == "ids|timestamp|content":
		// Many titles at once: normalized, missing and invalid ones, and Big,
		// whose content comes only in the continued response.
		titles := strings.Split(p("titles"), "|")
		w.batches = append(w.batches, len(titles))
		follow := p("redirects") == "1"
		var normalized, redirects, interwiki, pages []any
		for _, t := range titles {
			if t == "wikipedia:Foo" {
				interwiki = append(interwiki, map[string]string{"title": "wikipedia:Foo", "iw": "wikipedia"})
				continue
			}
			n := strings.ReplaceAll(t, "_", " ")
			if n != "" {
				n = strings.ToUpper(n[:1]) + n[1:]
			}
			if n != t {
				normalized = append(normalized, map[string]string{"from": t, "to": n})
			}
			if follow && n == "Alias" {
				redirects = append(redirects, map[string]string{"from": n, "to": "Target"})
				n = "Target"
			}
			if follow && n == "CrossWiki" {
				redirects = append(redirects, map[string]string{"from": n, "to": "w:Target"})
				interwiki = append(interwiki, map[string]string{"title": "w:Target", "iw": "w"})
				continue
			}
			switch {
			case strings.Contains(n, "<"):
				pages = append(pages, map[string]any{"title": t, "invalid": true})
			case strings.HasPrefix(n, "Missing"):
				pages = append(pages, map[string]any{"title": n, "missing": true})
			case n == "Big" && p("rvcontinue") == "":
				pages = append(pages, map[string]any{"title": n})
			default:
				model := "wikitext"
				if strings.HasSuffix(n, ".css") {
					model = "css"
				} else if strings.HasPrefix(n, "Module:") {
					model = "Scribunto"
				}
				content := "Text of " + n
				if !follow && n == "Alias" {
					content = "#REDIRECT [[Target]]"
				}
				pages = append(pages, map[string]any{"title": n, "revisions": []any{map[string]any{"revid": 500 + len(n), "timestamp": "2026-11-22T00:00:00Z",
					"slots": map[string]any{"main": map[string]string{"contentmodel": model, "content": content}}}}})
			}
		}
		resp := map[string]any{"query": map[string]any{"normalized": normalized, "redirects": redirects, "interwiki": interwiki, "pages": pages}}
		if strings.Contains(p("titles"), "Big") && p("rvcontinue") == "" {
			resp["continue"] = map[string]any{"rvcontinue": "1|2", "continue": "||"}
		}
		send(resp)
	case p("prop") == "revisions" && p("rvprop") == "ids|timestamp|user|content|contentmodel":
		// Diff: Sandbox has revisions 201 and 202; 203 of Hidden has its text hidden.
		revs := map[int]map[string]any{
			201: {"title": "Sandbox", "parentid": 0, "content": "a\nb\nc"},
			202: {"title": "Sandbox", "parentid": 201, "content": "a\nB\nc"},
			203: {"title": "Hidden", "parentid": 202, "hidden": true},
			// Only the final newline differs between 204 and 205.
			204: {"title": "Newline", "parentid": 0, "content": "hello"},
			205: {"title": "Newline", "parentid": 204, "content": "hello\n"},
			206: {"title": "Empty", "parentid": 0, "content": ""},
			207: {"title": "Empty", "parentid": 206, "noslot": true},
			208: {"title": "Empty", "parentid": 206, "nocontent": true},
			209: {"title": "Data.json", "parentid": 0, "content": "{\n\t\"a\": 1\n}", "model": "json"},
			210: {"title": "Alias", "parentid": 0, "content": "#REDIRECT [[Sandbox]]"},
		}
		id := 0
		switch {
		case p("revids") != "":
			fmt.Sscan(p("revids"), &id)
			if revs[id] == nil {
				send(map[string]any{"query": map[string]any{"badrevids": map[string]any{p("revids"): map[string]any{"revid": id, "missing": true}}}})
				return
			}
		case p("titles") == "Sandbox" || (p("titles") == "Alias" && p("redirects") == "1"):
			id = 202
		case p("titles") == "Alias":
			id = 210
		case p("titles") == "Data.json":
			id = 209
		default:
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": p("titles"), "missing": true}}}})
			return
		}
		r := revs[id]
		model := "wikitext"
		if m, ok := r["model"].(string); ok {
			model = m
		}
		slot := map[string]any{"content": r["content"], "contentmodel": model}
		switch {
		case r["hidden"] == true:
			slot = map[string]any{"texthidden": true}
		case r["nocontent"] == true:
			slot = map[string]any{"contentmodel": "wikitext"}
		}
		slots := map[string]any{"main": slot}
		if r["noslot"] == true {
			slots = map[string]any{}
		}
		send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": r["title"], "revisions": []any{map[string]any{
			"revid": id, "parentid": r["parentid"], "timestamp": "2026-11-22T00:00:00Z", "user": "Jmnote", "slots": slots}}}}}})
	case p("prop") == "revisions" && strings.Contains(p("rvprop"), "flags"):
		// History: revisions 105 down to 101, two per response.
		if p("titles") == "Missing" {
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": "Missing", "missing": true}}}})
			return
		}
		w.lists = append(w.lists, r.Form)
		if p("titles") == "Alias" && p("redirects") != "1" {
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": "Alias", "revisions": []any{
				map[string]any{"revid": 90, "parentid": 0, "timestamp": "2026-11-01T00:00:00Z", "user": "Jmnote", "comment": "redir", "size": 20, "tags": []string{}},
			}}}}})
			return
		}
		start := 105
		if c := p("rvcontinue"); c != "" {
			fmt.Sscan(c, &start)
		}
		n := 2
		fmt.Sscan(p("rvlimit"), &n)
		n = min(n, 2)
		var revs []any
		for id := start; id > start-n && id > 100; id-- {
			revs = append(revs, map[string]any{"revid": id, "parentid": id - 1, "timestamp": fmt.Sprintf("2026-11-%02dT08:41:11Z", id-90),
				"user": "Jmnote", "comment": fmt.Sprintf(" edit\r\n%d  %s\t", id, strings.Repeat("가", 70)), "size": id * 10, "minor": id%2 == 0, "tags": []string{}})
		}
		resp := map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": p("titles"), "revisions": revs}}}}
		if next := start - n; next > 100 {
			resp["continue"] = map[string]string{"rvcontinue": fmt.Sprint(next), "continue": "||"}
		}
		send(resp)
	case p("prop") == "revisions":
		if p("titles") == "wikipedia:Foo" {
			send(map[string]any{"query": map[string]any{"interwiki": []any{map[string]string{"title": "wikipedia:Foo", "iw": "wikipedia"}}}})
			return
		}
		if p("titles") == "Missing" {
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"missing": true}}}})
			return
		}
		if p("titles") == "Alias" {
			content := "#REDIRECT [[Target]]"
			if p("redirects") == "1" {
				content = "Target text"
			}
			send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"revisions": []any{map[string]any{"slots": map[string]any{"main": map[string]string{"content": content}}}}}}}})
			return
		}
		send(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"revisions": []any{map[string]any{"slots": map[string]any{"main": map[string]string{"content": "Hello"}}}}}}}})
	case p("action") == "upload":
		req := uploadRequest{Filename: p("filename"), Stash: p("stash") != "", Offset: p("offset"), FileSize: p("filesize"), FileKey: p("filekey"),
			Ignore: p("ignorewarnings") != "", Comment: p("comment"), Text: p("text"), Token: p("token"), Chunked: len(r.TransferEncoding) > 0}
		for _, field := range []string{"file", "chunk"} {
			if fh, _, err := r.FormFile(field); err == nil {
				b, _ := io.ReadAll(fh)
				req.Field, req.Size, req.Content = field, len(b), string(b[:min(len(b), 16)])
			}
		}
		w.uploads = append(w.uploads, req)
		if p("assert") != "user" || !authed {
			send(map[string]any{"error": map[string]string{"code": "assertuserfailed", "info": "not logged in"}})
			return
		}
		if req.Stash && req.Filename == "broken.bin" && req.Offset != "0" {
			send(map[string]any{"error": map[string]string{"code": "stashfailed", "info": "Could not store the chunk."}})
			return
		}
		if req.Stash {
			var off, size int
			fmt.Sscan(req.Offset, &off)
			fmt.Sscan(req.FileSize, &size)
			result := "Continue"
			if off+req.Size >= size {
				result = "Success"
			}
			send(map[string]any{"upload": map[string]any{"result": result, "filekey": "fk.1", "offset": off + req.Size}})
			return
		}
		switch {
		case req.Filename == "Exists.jpg" && !req.Ignore:
			send(map[string]any{"upload": map[string]any{"result": "Warning", "warnings": map[string]any{"exists": "Exists.jpg"}, "filekey": "w.1"}})
		case req.Filename == "Dup.jpg" && !req.Ignore:
			send(map[string]any{"upload": map[string]any{"result": "Warning", "warnings": map[string]any{"duplicate": []string{"Other.jpg", "Third.jpg"}}}})
		case req.Filename == "Same.jpg":
			send(map[string]any{"error": map[string]string{"code": "fileexists-no-change", "info": "The upload is an exact duplicate of the current version."}})
		default:
			send(map[string]any{"upload": map[string]any{"result": "Success", "filename": strings.ReplaceAll(req.Filename, " ", "_"),
				"imageinfo": map[string]any{"url": "//fake.test/images/" + strings.ReplaceAll(req.Filename, " ", "_") + "?utm_source=fake&utm_content=original"}}})
		}
	case p("action") == "move" || p("action") == "delete":
		w.writes = append(w.writes, r.Form)
		if p("assert") != "user" || !authed {
			send(map[string]any{"error": map[string]string{"code": "assertuserfailed", "info": "not logged in"}})
			return
		}
		title := p("from") + p("title")
		switch {
		case strings.HasPrefix(title, "Missing"):
			send(map[string]any{"error": map[string]string{"code": "missingtitle", "info": "The page you specified doesn't exist."}})
		case p("to") == "Taken":
			send(map[string]any{"error": map[string]string{"code": "articleexists", "info": "A page of that name already exists."}})
		case p("action") == "move" && p("from") == p("to"):
			send(map[string]any{"error": map[string]string{"code": "selfmove", "info": "The title is the same."}})
		case p("action") == "delete":
			send(map[string]any{"delete": map[string]any{"title": title, "reason": p("reason"), "logid": 9}})
		default:
			m := map[string]any{"from": p("from"), "to": p("to"), "reason": p("reason"), "redirectcreated": p("noredirect") == ""}
			if p("movetalk") != "" {
				m["talkfrom"], m["talkto"] = "Talk:"+p("from"), "Talk:"+p("to")
			}
			if p("movesubpages") != "" {
				m["subpages"] = []any{map[string]string{"from": p("from") + "/A", "to": p("to") + "/A"}}
			}
			send(map[string]any{"move": m})
		}
	case p("action") == "edit":
		if p("assert") != "user" || !authed {
			send(map[string]any{"error": map[string]string{"code": "assertuserfailed", "info": "not logged in"}})
			return
		}
		var flags []string
		for _, k := range []string{"nocreate", "createonly", "minor", "bot"} {
			if p(k) != "" {
				flags = append(flags, k)
			}
		}
		if p("baserevid") != "" {
			flags = append(flags, "baserevid="+p("baserevid"))
		}
		w.editFlags = strings.Join(flags, " ")
		title := p("title")
		// Pages under New/ and Missing do not exist until created.
		exists := !(title == "Missing" || strings.HasPrefix(title, "New/")) || w.created[title]
		switch {
		case p("nocreate") != "" && !exists:
			send(map[string]any{"error": map[string]string{"code": "missingtitle", "info": "The page you specified doesn't exist."}})
			return
		case p("baserevid") != "" && p("baserevid") != "202" && p("text") != "merges":
			// Sandbox's latest revision is 202; an edit from an older one
			// conflicts unless the wiki can merge it.
			send(map[string]any{"error": map[string]string{"code": "editconflict", "info": "Edit conflict."}})
			return
		case p("createonly") != "" && exists:
			send(map[string]any{"error": map[string]string{"code": "articleexists", "info": "The article you tried to create has been created already."}})
			return
		case exists && p("text") == "Hello":
			send(map[string]any{"edit": map[string]any{"result": "Success", "title": title, "nochange": true}})
			return
		}
		w.edits = append(w.edits, map[string]string{"title": title, "text": p("text"), "summary": p("summary"), "token": p("token")})
		if !exists {
			if w.created == nil {
				w.created = map[string]bool{}
			}
			w.created[title] = true
		}
		send(map[string]any{"edit": map[string]any{"result": "Success", "title": title, "new": !exists}})
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
	// stdoutTTY makes standard output a terminal; interactive also makes
	// standard input and error terminals, so that mw prompts.
	stdoutTTY, interactive bool
}

func newEnv(t *testing.T) *env {
	w := &fakeWiki{}
	srv := httptest.NewServer(w)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("MW_CONFIG_DIR", dir+"/mw")
	t.Setenv("MW_CACHE_DIR", dir+"/cache")
	t.Setenv("MW_TOKEN", "")
	t.Setenv("MW_WIKI", "")
	host, _ := config.HostOf(srv.URL)
	return &env{t: t, dir: dir + "/mw", api: srv.URL + "/w/api.php", host: host, base: srv.URL, wiki: w}
}

func (e *env) run(stdin string, args ...string) (string, string, int) {
	ios, in, out, errOut := iostreams.Test()
	if e.stdoutTTY {
		ios.SetTTY(false, true, false)
	}
	if e.interactive {
		ios.SetTTY(true, false, true)
	}
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
	cfg.Farms[e.host] = []config.FarmSite{{Host: e.host, API: e.api}}
	cfg.Login(e.host, user, a)
	require.NoError(e.t, cfg.Save())
	return cfg
}

func (e *env) login() {
	_, stderr, code := e.run(goodToken+"\n", "auth", "login", "--wiki", e.api, "--with-token")
	require.Equal(e.t, 0, code, stderr)
}

func TestLoginStatusTokenLogout(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "auth", "status")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "not logged into any wikis")

	_, stderr, code = e.run("bad\n", "auth", "login", "--wiki", e.api, "--with-token")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "check token")

	e.login()
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, e.api, cfg.Context)
	require.Contains(t, cfg.Farms, e.host, "the first login adds the site")
	assert.Equal(t, e.api, cfg.Farms[e.host][0].API)
	assert.Equal(t, "//fake.test", cfg.Farms[e.host][0].Server)

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
	_, stderr, code := e.run(goodToken, "auth", "login", "--wiki", e.api, "--with-token")
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
	out, _, code := e.run("", "page", "view", "Sandbox", "--wiki", e.api)
	assert.Equal(t, 0, code)
	assert.Equal(t, "Hello", out)

	_, stderr, code := e.run("", "page", "view", "Missing", "--wiki", e.api)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist")

	_, _, code = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "x")
	assert.Equal(t, 4, code)

	e.login()
	_, _, code = e.run("", "page", "edit", "Sandbox", "--text", "x", "-F", "-")
	assert.Equal(t, 2, code)

	_, stderr, code = e.run("New text", "page", "edit", "Sandbox", "-F", "-", "-s", "sum")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Updated Sandbox on "+e.host)
	require.Len(t, e.wiki.edits, 1)
	assert.Equal(t, map[string]string{"title": "Sandbox", "text": "New text", "summary": "sum", "token": "tok+\\"}, e.wiki.edits[0])
	assert.Equal(t, "nocreate", e.wiki.editFlags)

	_, stderr, code = e.run("Hello", "page", "edit", "Sandbox", "-F", "-", "--minor", "--bot")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "No changes to Sandbox on "+e.host)
	assert.Equal(t, "nocreate minor bot", e.wiki.editFlags)
	require.Len(t, e.wiki.edits, 1)

	// edit does not create a page; create does not change one.
	_, stderr, code = e.run("", "page", "edit", "New/Page one", "--text", "x")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist: New/Page one on "+e.host+"; to make it, run the same command with `mw page create` in place of `mw page edit`")
	_, stderr, code = e.run("", "page", "create", "Sandbox", "--text", "x")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page already exists: Sandbox on "+e.host+"; to change it, run the same command with `mw page edit` in place of `mw page create`")

	_, stderr, code = e.run("", "page", "create", "New/Page one", "--text", "First", "-s", "new")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Created New/Page one on "+e.host)
	assert.Equal(t, "createonly", e.wiki.editFlags)
	require.Len(t, e.wiki.edits, 2)
	assert.Equal(t, "First", e.wiki.edits[1]["text"])

	_, _, code = e.run("", "page", "create", "New/Page two")
	assert.Equal(t, 2, code)
}

func TestAPI(t *testing.T) {
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
	secondAPI := e.base + "/second/api.php"
	_, stderr, code = e.run("", "site", "add", "second", secondAPI)
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
	assert.Contains(t, out, "* second  "+secondAPI+"\n")

	_, stderr, code = e.run("", "site", "remove", "other")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Logged out of other")
	cfg, _ = config.LoadDir(e.dir)
	assert.NotContains(t, cfg.Farms, "other")
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
	_, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", e.base+"/gone/api.php")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mw tried "+e.base+"/gone/api.php; check the URL, or add the wiki with `mw site add NAME URL`")
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
	assert.Equal(t, len(sites.Builtin())+len(sites.BuiltinPatterns()), strings.Count(out, "\n"))
	assert.Contains(t, out, "*.wikipedia.org           https://{host}/w/api.php\n")
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
	out, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", e.api, "--json", "pageid,url,revision,protection")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `{"pageid":7,"url":"https://fake.test/wiki/Sandbox",
		"revision":{"revid":42,"parentid":41,"timestamp":"2026-11-22T00:00:00Z","user":"Jmnote","comment":"fix","size":5},
		"protection":[{"type":"edit","level":"sysop","expiry":"infinity"}]}`, out)
	assert.NotContains(t, e.wiki.rvprop, "content")
	assert.Equal(t, "info|revisions", e.wiki.prop)

	out, _, code = e.run("", "page", "view", "Sandbox", "--wiki", e.api, "--json", "categories,content", "--jq", ".categories[]")
	assert.Equal(t, 0, code)
	assert.Equal(t, "Category:A\nCategory:B\nCategory:C\n", out)
	assert.Contains(t, e.wiki.rvprop, "content")

	out, _, _ = e.run("", "page", "view", "Sandbox", "--wiki", e.api, "--json", "content", "--jq", ".content")
	assert.Equal(t, "Hello\n", out)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", e.api, "--json")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "Specify one or more comma-separated fields")
	assert.Contains(t, stderr, "categories")

	for _, args := range [][]string{{"--json="}, {"--json", ""}, {"--web", "--json="}} {
		out, stderr, code = e.run("", append([]string{"page", "view", "Sandbox", "--wiki", e.api}, args...)...)
		assert.Equal(t, 2, code, args)
		assert.Empty(t, out, args)
		assert.Contains(t, stderr, "Specify one or more comma-separated fields", args)
	}
	assert.Empty(t, e.browsed)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", e.api, "--json", "nope")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `Unknown JSON field: "nope"`)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", e.api, "--jq", ".")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "without specifying `--json`")

	_, stderr, code = e.run("", "page", "view", "Missing", "--wiki", e.api, "--json", "title")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist")
}

func TestPageViewWeb(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "page", "view", "Main Page", "--wiki", e.api, "--web")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, e.base+"/w/index.php?title=Main+Page", e.browsed)

	_, _, code = e.run("", "page", "view", "Main Page", "--wiki", e.api, "--web", "--json", "title")
	assert.Equal(t, 2, code)
}

func TestPageList(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "page", "list", "--wiki", e.api, "-L", "5")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "P1\nP2\nP3\nP4\nP5\n", out)
	require.Len(t, e.wiki.lists, 2)
	assert.Equal(t, "allpages", e.wiki.lists[0].Get("list"))
	assert.Equal(t, "5", e.wiki.lists[0].Get("aplimit"))
	assert.Equal(t, "3", e.wiki.lists[1].Get("fakecontinue"))
	assert.Equal(t, "2", e.wiki.lists[1].Get("aplimit"))

	e.wiki.lists = nil
	out, _, code = e.run("", "page", "list", "--wiki", e.api, "--category", "Living people", "-n", "0", "--json", "title,pageid", "-L", "2")
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
		_, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--category", in)
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, want, e.wiki.lists[0].Get("cmtitle"), in)
	}

	out, _, code = e.run("", "page", "list", "--wiki", e.api, "--prefix", "Nothing", "--json", "title")
	require.Equal(t, 0, code)
	assert.Equal(t, "[]\n", out)

	e.wiki.lists = nil
	out, stderr, code = e.run("", "page", "list", "--wiki", e.api, "-S", "jq", "-L", "100", "--json", "title", "--jq", "length")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "7\n", out)
	require.Len(t, e.wiki.lists, 3)
	assert.Equal(t, "6", e.wiki.lists[2].Get("sroffset"))

	for flag, list := range map[string]string{"--links-to": "backlinks", "--embeds": "embeddedin", "--prefix": "allpages"} {
		e.wiki.lists = nil
		_, stderr, code = e.run("", "page", "list", "--wiki", e.api, flag, "X", "-L", "1")
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, list, e.wiki.lists[0].Get("list"), flag)
	}

	_, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--prefix", "A", "--search", "b")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "specify only one of --prefix, --search")
	_, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--category", "")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--category needs a value")
	_, _, code = e.run("", "page", "list", "--wiki", e.api, "-L", "0")
	assert.Equal(t, 2, code)
}

func TestBotPassword(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_PASSWORD", "wrong")
	_, stderr, code := e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote@mw")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "bot password login failed for Jmnote@mw: Incorrect username or password")

	t.Setenv("MW_PASSWORD", "botpw")
	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote@mw")
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
	out, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", other)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Hello", out)
	_, _, code = e.run("x", "page", "edit", "Sandbox", "--wiki", other, "-F", "-")
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
	_, stderr, code := e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote#mw", "--bot-password")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, config.AuthBotPassword, cfg.Logins[e.host].Users["Jmnote"].Auth)
	assert.Equal(t, "Jmnote#mw", cfg.Logins[e.host].Users["Jmnote"].BotName)

	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--bot-password")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--bot-password needs --username")
}

func TestStatusEnvTokenForFarmSite(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_TOKEN", goodToken)
	cfg := &config.Config{Context: "handbook",
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

func TestSiteAddRequiresExplicitFarmToShareExistingLogin(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "site", "add", "docs", e.base)
	require.Equal(t, 0, code, stderr)
	_, stderr, code = e.run(goodToken, "auth", "login", "--wiki", "docs", "--with-token")
	require.Equal(t, 0, code, stderr)

	secondAPI := e.base + "/two/api.php"
	_, stderr, code = e.run("", "site", "add", "docs", secondAPI)
	require.Equal(t, 2, code)
	assert.Contains(t, stderr, `use --farm "docs" to explicitly share its login`)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	require.Len(t, cfg.Farms["docs"], 1)
	require.Contains(t, cfg.Logins, "docs")

	_, stderr, code = e.run("", "site", "add", "handbook", secondAPI, "--farm", "docs")
	require.Equal(t, 0, code, stderr)
	cfg, err = config.LoadDir(e.dir)
	require.NoError(t, err)
	require.Len(t, cfg.Farms["docs"], 2)
	assert.Contains(t, cfg.Farms["docs"][1].Aliases, "handbook")
	out, stderr, code := e.run("", "auth", "token", "--wiki", "handbook")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, goodToken+"\n", out)
}

func TestSiteListAndRemoveDisambiguateSameHostAPIs(t *testing.T) {
	e := newEnv(t)
	firstAPI := e.base + "/one/api.php"
	secondAPI := e.base + "/two/api.php"
	cfg := &config.Config{Dir: e.dir, Context: secondAPI, Farms: map[string][]config.FarmSite{
		"multi": {
			{Host: e.host, API: firstAPI},
			{Host: e.host, API: secondAPI},
		},
	}}
	require.NoError(t, cfg.Save())

	out, stderr, code := e.run("", "site", "list")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "  "+firstAPI+"  "+firstAPI+"\n")
	assert.Contains(t, out, "* "+secondAPI+"  "+secondAPI+"\n")

	_, stderr, code = e.run("", "site", "remove", e.host)
	require.Equal(t, 2, code)
	assert.Contains(t, stderr, "matches multiple wikis")

	_, stderr, code = e.run("", "site", "remove", firstAPI)
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	require.Len(t, cfg.Farms["multi"], 1)
	assert.Equal(t, secondAPI, cfg.Farms["multi"][0].API)
	assert.Equal(t, secondAPI, cfg.Context)

	_, stderr, code = e.run("", "site", "remove", secondAPI)
	require.Equal(t, 0, code, stderr)
	cfg, err = config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Empty(t, cfg.Farms["multi"])
	assert.Empty(t, cfg.Context)
}

func TestStatusUsesFarmAPIWhenSiteAliasConflicts(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Dir: e.dir, Farms: map[string][]config.FarmSite{
		"other":     {{Host: "other.example.org", API: "https://other.example.org/api.php", Aliases: []string{"wikimedia"}}},
		"wikimedia": {{Host: e.host, API: e.api}},
	}, Logins: map[string]*config.Login{}}
	cfg.Login("wikimedia", "Jmnote", &config.Account{Auth: config.AuthOAuth2, OAuthToken: goodToken})
	require.NoError(t, cfg.Save())
	out, stderr, code := e.run("", "auth", "status")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "API: "+e.api)
	assert.NotContains(t, out, "API: https://other.example.org/api.php")
}

func TestFirstLoginRejectsFarmNameCollision(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Dir: e.dir, Farms: map[string][]config.FarmSite{e.host: {{Host: "wiki.example.org"}}}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run(goodToken, "auth", "login", "--wiki", e.base, "--with-token")
	require.Equal(t, 2, code, stderr)
	assert.Contains(t, stderr, "conflicts with a farm name")
	cfg, _ = config.LoadDir(e.dir)
	assert.Equal(t, 1, len(cfg.Farms[e.host]))
	assert.Equal(t, "wiki.example.org", cfg.Farms[e.host][0].Host)
}

func TestFarmAliasAndAPISelectSameWikiAndPreserveDefault(t *testing.T) {
	e := newEnv(t)
	secondAPI := e.base + "/two/api.php"
	cfg := &config.Config{Dir: e.dir, Farms: map[string][]config.FarmSite{
		"multi": {
			{Host: e.host, Aliases: []string{"first"}, API: e.api},
			{Host: e.host, Aliases: []string{"second"}, API: secondAPI},
		},
	}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run("", "page", "view", "Main Page", "--wiki", "second")
	require.Equal(t, 0, code, stderr)
	_, stderr, code = e.run("", "page", "view", "Main Page", "--wiki", secondAPI)
	require.Equal(t, 0, code, stderr)

	_, stderr, code = e.run(goodToken, "auth", "login", "--wiki", secondAPI, "--with-token")
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
	cfg := &config.Config{Dir: e.dir, Farms: map[string][]config.FarmSite{
		"multi": {
			{Host: e.host, API: e.api},
			{Host: e.host, API: secondAPI},
		},
	}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run(goodToken, "auth", "login", "--wiki", secondAPI, "--with-token")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Equal(t, secondAPI, cfg.Context)
	_, stderr, code = e.run("", "page", "view", "Main Page")
	require.Equal(t, 0, code, stderr)
}

func TestSiteRemoveClearsAPIDefault(t *testing.T) {
	e := newEnv(t)
	cfg := &config.Config{Dir: e.dir, Context: e.api, Farms: map[string][]config.FarmSite{"docs": {{Host: e.host, API: e.api}}}}
	require.NoError(t, cfg.Save())
	_, stderr, code := e.run("", "site", "remove", "docs")
	require.Equal(t, 0, code, stderr)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Empty(t, cfg.Context)
}

func TestOAuth1Login(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("ck cs at", "auth", "login", "--wiki", e.api, "--oauth1", "--with-token")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "standard input has 3 values")

	_, stderr, code = e.run("bad cs at as", "auth", "login", "--wiki", e.api, "--oauth1", "--with-token")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mwoauth-invalid-authorization")
	assert.Contains(t, stderr, "The wiki does not know the consumer token")

	_, stderr, code = e.run("ck cs bad as", "auth", "login", "--wiki", e.api, "--oauth1", "--with-token")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "The wiki does not know the access token")

	_, stderr, code = e.run("ck\ncs\nat\nas\n", "auth", "login", "--wiki", e.api, "--oauth1", "--with-token")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Logged in to "+e.host+" as Jmnote")
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	a := cfg.Logins[e.host].Users["Jmnote"]
	require.NotNil(t, a)
	loaded := *a
	loaded.CredentialRef = ""
	assert.Equal(t, config.Account{Auth: config.AuthOAuth1, ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}, loaded)

	out, stderr, code := e.run("", "auth", "status")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "- Auth: OAuth 1.0a consumer ck")
	assert.Contains(t, out, "- Token: ********")
	assert.NotContains(t, out, "cs")

	_, stderr, code = e.run("New", "page", "edit", "Sandbox", "-F", "-")
	require.Equal(t, 0, code, stderr)
	require.Len(t, e.wiki.edits, 1)

	_, stderr, code = e.run("", "auth", "token")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "uses OAuth 1.0a keys")

	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--oauth1", "--username", "Jmnote")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "only one of --oauth1 and --username")

	cfg.Logins[e.host].Users["Jmnote"].ConsumerKey = "disabled"
	require.NoError(t, cfg.Save())
	out, _, code = e.run("", "auth", "status")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "The OAuth 1.0a keys are invalid")
}

func TestAPIPaginate(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "api", "--wiki", e.api, "action=query", "list=allpages", "--paginate", "--jq", ".query.allpages[].title")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "P1\nP2\nP3\nP4\nP5\nP6\nP7\n", out)
	require.Len(t, e.wiki.lists, 3)
	assert.Equal(t, "6", e.wiki.lists[2].Get("fakecontinue"))

	// Numbers in continue, as sroffset, are sent as they are.
	out, stderr, code = e.run("", "api", "--wiki", e.api, "action=query", "list=search", "srsearch=x", "--paginate", "--slurp")
	require.Equal(t, 0, code, stderr)
	var pages []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &pages))
	assert.Len(t, pages, 3)
	assert.Equal(t, "6", e.wiki.lists[5].Get("sroffset"))

	out, stderr, code = e.run("", "api", "--wiki", e.api, "action=query", "list=allpages", "--jq", ".query.allpages | length")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "3\n", out)

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--slurp"}, "`--slurp` requires `--paginate`"},
		{[]string{"--paginate", "--slurp", "--jq", "."}, "not supported with `--jq`"},
		{[]string{"--paginate", "format=xml"}, "need format=json"},
		{[]string{"--jq", ".["}, "invalid --jq expression"},
	} {
		_, stderr, code := e.run("", append([]string{"api", "--wiki", e.api, "action=query", "list=allpages"}, c.args...)...)
		assert.Equal(t, 2, code, c.args)
		assert.Contains(t, stderr, c.want, c.args)
	}
}

// A continue value that a later response drops is not sent again: each
// request is the original one with the last continue values.
func TestAPIPaginateResetsContinue(t *testing.T) {
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = append(got, r.Form)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"query": map[string]any{"n": len(got)}}
		switch len(got) {
		case 1:
			resp["continue"] = map[string]string{"a": "1", "continue": "x"}
		case 2:
			resp["continue"] = map[string]string{"b": "2", "continue": "x"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	e := newEnv(t)
	out, stderr, code := e.run("", "api", "--wiki", srv.URL+"/w/api.php", "action=query", "list=x", "--paginate", "-q", ".query.n")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "1\n2\n3\n", out)
	require.Len(t, got, 3)
	assert.Equal(t, "1", got[1].Get("a"))
	assert.False(t, got[2].Has("a"))
	assert.Equal(t, "2", got[2].Get("b"))
	assert.Equal(t, "x", got[2].Get("list"))
}

// A continue value replaces a start value of the same name in the API URL, and
// --jq keeps integers beyond 2^53 exact.
func TestAPIPaginateURLQueryAndBigNumbers(t *testing.T) {
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		if len(got) == 1 {
			_, _ = w.Write([]byte(`{"query":{"id":9007199254740993,"big":123456789012345678901234567890,"f":1.5},"continue":{"sroffset":10,"continue":"-||"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"query":{"id":9007199254740995}}`))
	}))
	defer srv.Close()
	e := newEnv(t)
	out, stderr, code := e.run("", "api", "--wiki", srv.URL+"/w/api.php?sroffset=5&wiki=a", "action=query", "list=search", "--paginate", "-q", ".query.id")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "9007199254740993\n9007199254740995\n", out)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"5"}, got[0]["sroffset"])
	assert.Equal(t, []string{"10"}, got[1]["sroffset"])
	assert.Equal(t, "a", got[1].Get("wiki"))

	got = nil
	out, stderr, code = e.run("", "api", "--wiki", srv.URL+"/w/api.php", "action=query", "-q", ".query | [.big, .f]")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[123456789012345678901234567890, 1.5]`, out)
	assert.Contains(t, out, "123456789012345678901234567890")
}

func TestPageHistory(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "page", "history", "Sandbox", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 5)
	// Only line breaks and tabs become spaces; other spacing stays.
	assert.Equal(t, "105\t2026-11-15T08:41:11Z\tJmnote\t1050\t\t edit 105  "+strings.Repeat("가", 70)+" ", lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "104\t2026-11-14T08:41:11Z\tJmnote\t1040\tm\t"))
	assert.Len(t, e.wiki.lists, 3)

	out, stderr, code = e.run("", "page", "history", "Sandbox", "--wiki", e.api, "-L", "3", "--json", "revid,minor,tags")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{"revid":105,"minor":false,"tags":[]},{"revid":104,"minor":true,"tags":[]},{"revid":103,"minor":false,"tags":[]}]`, out)
	assert.Equal(t, "1", e.wiki.lists[len(e.wiki.lists)-1].Get("rvlimit"))

	e.stdoutTTY = true
	out, stderr, code = e.run("", "page", "history", "Sandbox", "--wiki", e.api, "-L", "1")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "105  2026-11-15 08:41  Jmnote  1050    edit 105 "+strings.Repeat("가", 25)+"…\n", out)
	e.stdoutTTY = false

	_, stderr, code = e.run("", "page", "history", "Missing", "--wiki", e.api)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist: Missing")
	_, stderr, code = e.run("", "page", "history", "Sandbox", "--wiki", e.api, "-L", "0")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "invalid value for --limit")
}

func TestPageDiff(t *testing.T) {
	e := newEnv(t)
	// The API leaves off the final newline; the diff marks it as diff -u does.
	want := "--- Sandbox\trevision 201 (2026-11-22T00:00:00Z, Jmnote)\n" +
		"+++ Sandbox\trevision 202 (2026-11-22T00:00:00Z, Jmnote)\n" +
		"@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n\\ No newline at end of file\n"
	for _, args := range [][]string{{"Sandbox"}, {"202"}, {"201", "202"}, {"201", "Sandbox"}} {
		out, stderr, code := e.run("", append([]string{"page", "diff", "--wiki", e.api}, args...)...)
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, want, out, args)
	}

	// The first revision is compared with empty text.
	out, stderr, code := e.run("", "page", "diff", "--wiki", e.api, "201")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "--- /dev/null\n+++ Sandbox\trevision 201 (2026-11-22T00:00:00Z, Jmnote)\n@@ -0,0 +1,3 @@\n+a\n+b\n+c\n\\ No newline at end of file\n", out)

	// A change of the final newline alone shows.
	out, stderr, code = e.run("", "page", "diff", "--wiki", e.api, "205")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "--- Newline\trevision 204 (2026-11-22T00:00:00Z, Jmnote)\n+++ Newline\trevision 205 (2026-11-22T00:00:00Z, Jmnote)\n@@ -1 +1 @@\n-hello\n\\ No newline at end of file\n+hello\n", out)

	// An empty text is a text; a revision whose text did not come is an error.
	out, stderr, code = e.run("", "page", "diff", "--wiki", e.api, "206")
	require.Equal(t, 0, code, stderr)
	assert.Empty(t, out)

	out, stderr, code = e.run("", "page", "diff", "--wiki", e.api, "202", "202")
	require.Equal(t, 0, code, stderr)
	assert.Empty(t, out)

	out, _, _ = e.run("", "page", "diff", "--wiki", e.api, "--color", "always", "Sandbox")
	assert.Contains(t, out, "\x1b[31m-b\x1b[0m\n\x1b[32m+B\x1b[0m\n")
	assert.Contains(t, out, "\x1b[36m@@ -1,3 +1,3 @@\x1b[0m")
	e.stdoutTTY = true
	t.Setenv("NO_COLOR", "1")
	out, _, _ = e.run("", "page", "diff", "--wiki", e.api, "Sandbox")
	assert.Equal(t, want, out)
	e.stdoutTTY = false

	_, stderr, code = e.run("", "page", "diff", "--wiki", e.api, "Sandbox", "--web")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, e.browsed, "Special%3ADiff%2F201%2F202")

	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"999"}, 1, "revision does not exist: revision 999"},
		{[]string{"Nowhere"}, 1, "page does not exist: Nowhere"},
		{[]string{"203"}, 1, "the text of revision 203 of Hidden is hidden or missing"},
		{[]string{"207"}, 1, "the API returned no text for revision 207 of Empty"},
		{[]string{"208"}, 1, "the API returned no text for revision 208 of Empty"},
		{[]string{"Sandbox", "--color", "sometimes"}, 2, "invalid value for --color"},
		{[]string{"1", "2", "3"}, 2, "accepts between 1 and 2 arg(s)"},
	} {
		_, stderr, code := e.run("", append([]string{"page", "diff", "--wiki", e.api}, c.args...)...)
		assert.Equal(t, c.code, code, c.args)
		assert.Contains(t, stderr, c.want, c.args)
	}
}

// A page URL of a wiki that was not added is looked up once and cached; a
// cached API that stops answering is forgotten and found again next time.
func TestWikiURLAndAPICache(t *testing.T) {
	e := newEnv(t)
	page := e.base + "/wiki/Sandbox"
	out, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", page)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Hello", out)
	found := e.wiki.siteinfos
	assert.Positive(t, found)

	c := apicache.Load()
	entry, key, ok := c.Get(page)
	require.True(t, ok)
	assert.Equal(t, e.api, entry.API)
	assert.Equal(t, e.base+"/wiki/", key)

	// Another page of the same wiki uses the cache too.
	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", e.base+"/wiki/Other_page")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, found, e.wiki.siteinfos, "the second run uses the cache")

	out, stderr, code = e.run("", "site", "list", "--cached")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, e.base+"/wiki/  "+e.api+"  ")

	// A cached API that no longer answers is forgotten.
	old := e.base + "/old/Page"
	require.NoError(t, c.Put(old, &config.Site{API: e.base + "/gone/api.php"}))
	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", old)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "mw had found the API of "+apicache.Key(old)+" at "+e.base+"/gone/api.php and has now forgotten it")
	old = e.base + "/wiki/Old"
	out, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", old)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Hello", out)

	_, stderr, code = e.run("", "site", "remove", e.base+"/wiki/")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Forgot the cached API of")
	_, _, ok = apicache.Load().Get(page)
	assert.False(t, ok)

	// An api.php URL is used as given, without looking or caching.
	before := e.wiki.siteinfos
	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, before, e.wiki.siteinfos)
	assert.Empty(t, apicache.Load().Entries)

	_, stderr, code = e.run("", "page", "view", "Sandbox", "--api", e.api)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "unknown flag: --api")
}

// Page URLs that differ only in a query that picks the wiki are found and
// cached apart.
func TestWikiURLQueryPicksWiki(t *testing.T) {
	e := newEnv(t)
	a := e.base + "/index.php?wiki=a&title=Main_Page"
	b := e.base + "/index.php?wiki=b&title=Main_Page"
	_, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", a)
	require.Equal(t, 0, code, stderr)
	before := e.wiki.siteinfos
	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", b)
	require.Equal(t, 0, code, stderr)
	assert.Greater(t, e.wiki.siteinfos, before, "the second wiki is looked up, not taken from the first")
	c := apicache.Load()
	ea, _, ok := c.Get(a)
	require.True(t, ok)
	eb, _, ok := c.Get(b)
	require.True(t, ok)
	assert.Equal(t, e.base+"/a/api.php", ea.API)
	assert.Equal(t, e.base+"/b/api.php", eb.API)
}

// A farm whose api.php picks the wiki by a query: discovery keeps that query
// from the EditURI link, and the cache gives it back on the next run.
func TestWikiURLAPIQueryPicksWiki(t *testing.T) {
	var siteinfos int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		wiki := q.Get("wiki")
		switch r.URL.Path {
		case "/index.php", "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<link rel="EditURI" type="application/rsd+xml" href="/api.php?wiki=%s&amp;action=rsd"/>`, wiki)
		case "/api.php":
			w.Header().Set("Content-Type", "application/json")
			if wiki == "" {
				_, _ = w.Write([]byte(`{"error":{"code":"nowiki","info":"no wiki chosen"}}`))
				return
			}
			if q.Get("meta") == "siteinfo" {
				siteinfos++
				_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"general": map[string]string{"sitename": wiki, "generator": "MediaWiki 1.43.1", "server": "http://" + r.Host, "script": "/index.php", "articlepath": "/index.php?wiki=" + wiki + "&title=$1"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"revisions": []any{map[string]any{"slots": map[string]any{"main": map[string]string{"content": "Wiki " + wiki}}}}}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e := newEnv(t)
	// A page URL, and a root URL without a path whose query picks the wiki.
	for _, page := range []string{srv.URL + "/index.php?wiki=a&title=Main_Page", srv.URL + "/index.php?wiki=b&title=Main_Page", srv.URL + "?wiki=c", srv.URL + "?wiki=d"} {
		wiki := page[strings.Index(page, "wiki=")+5 : strings.Index(page, "wiki=")+6]
		out, stderr, code := e.run("", "page", "view", "Sandbox", "--wiki", page)
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, "Wiki "+wiki, out)
		entry, _, ok := apicache.Load().Get(page)
		require.True(t, ok)
		assert.Equal(t, srv.URL+"/api.php?wiki="+wiki, entry.API)

		before := siteinfos
		out, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", page)
		require.Equal(t, 0, code, stderr)
		assert.Equal(t, "Wiki "+wiki, out)
		assert.Equal(t, before, siteinfos, "the cache is used")
	}
}

// A user's pattern with another API does not hide the built-in one when the
// wiki is given by its API URL: the Wikimedia login is found.
func TestPatternAPIURLFindsBuiltinLogin(t *testing.T) {
	e := newEnv(t)
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	cfg.Farms["mywiki"] = []config.FarmSite{{Host: "*.wikipedia.org", API: "https://{host}/api.php"}}
	cfg.Login("wikimedia", "Alice", &config.Account{Auth: config.AuthOAuth2, OAuthToken: "wm-token"})
	require.NoError(t, cfg.Save())
	out, stderr, code := e.run("", "auth", "token", "--wiki", "https://de.wikipedia.org/w/api.php")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "wm-token\n", out)
}

// --dry-run prints what the edit would change, as the wiki would save the
// text, and saves nothing.
func TestPageEditDryRun(t *testing.T) {
	e := newEnv(t)
	// Trailing white space is dropped on save, so this changes nothing.
	_, stderr, code := e.run("a\nB\nc  \n", "page", "edit", "Sandbox", "--wiki", e.api, "-F", "-", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "No changes to Sandbox on "+e.host)

	// No login needed; a signature is the one the wiki would give.
	out, stderr, code := e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "a\nX ~~~\nc", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "--- Sandbox\trevision 202 (2026-11-22T00:00:00Z, Jmnote)\n+++ Sandbox\t(new text, not saved)\n"+
		"@@ -1,3 +1,3 @@\n a\n-B\n+X [[Special:Contributions/127.0.0.1|127.0.0.1]]\n c\n\\ No newline at end of file\n", out)
	e.login()
	out, stderr, code = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "a\nX ~~~\nc", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "+X [[User:Jmnote|Jmnote]]\n")

	out, stderr, code = e.run("", "page", "create", "New/Draft", "--wiki", e.api, "--text", "Draft", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "--- /dev/null\n+++ New/Draft\t(new text, not saved)\n@@ -0,0 +1 @@\n+Draft\n\\ No newline at end of file\n", out)

	_, stderr, code = e.run("", "page", "create", "Sandbox", "--wiki", e.api, "--text", "x", "--dry-run")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page already exists: Sandbox on "+e.host+"; to change it, run the same command with `mw page edit`")
	_, stderr, code = e.run("", "page", "edit", "Nowhere", "--wiki", e.api, "--text", "x", "--dry-run")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist: Nowhere on "+e.host+"; to make it, run the same command with `mw page create`")

	// A page is transformed in its own model; a new one in its title's.
	e.wiki.pstModels = nil
	out, stderr, code = e.run("", "page", "edit", "Data.json", "--wiki", e.api, "--text", `{"a":2,"b":"~~~"}`, "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "-\t\"a\": 1\n+\t\"a\": 2,\n+\t\"b\": \"~~~\"\n")
	_, stderr, code = e.run("", "page", "create", "New/Draft.json", "--wiki", e.api, "--text", `{"x":1}`, "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []string{"json", ""}, e.wiki.pstModels)

	assert.Empty(t, e.wiki.edits, "nothing is saved")
}

// A password login answers the fields the wiki asks for next, such as a
// two-factor code, from MW_OTP or a prompt.
func TestPasswordLoginTwoFactor(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_PASSWORD", "pw")
	t.Setenv("MW_OTP", "")
	_, stderr, code := e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote")
	require.Equal(t, 0, code, stderr)
	cfg, _ := config.LoadDir(e.dir)
	assert.Equal(t, config.AuthSession, cfg.Logins[e.host].Users["Jmnote"].Auth)

	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--username", "TwoFA")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "the wiki asks for Token: Enter a verification code from your authentication device.")
	assert.Contains(t, stderr, "set MW_OTP")

	t.Setenv("MW_OTP", "000000")
	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--username", "TwoFA")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "login FAIL: Verification failed.")

	t.Setenv("MW_OTP", "123456")
	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--username", "TwoFA")
	require.Equal(t, 0, code, stderr)

	// Asked at a prompt, with the wiki's message; hidden fields are not asked.
	t.Setenv("MW_OTP", "")
	e.interactive = true
	_, stderr, code = e.run("123456\n", "auth", "login", "--wiki", e.api, "--username", "TwoFA")
	e.interactive = false
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Enter a verification code from your authentication device.\n? Token: ")
	assert.NotContains(t, stderr, "hiddenfield")
}

// --domain goes with a password or bot password login, and a bot password
// keeps it for the logins of later runs.
func TestLoginDomain(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_PASSWORD", "pw")
	_, stderr, code := e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote", "--domain", "corp")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []string{"corp"}, e.wiki.domains)

	t.Setenv("MW_PASSWORD", "botpw")
	e.wiki.domains = nil
	_, stderr, code = e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote@mw", "--domain", "corp")
	require.Equal(t, 0, code, stderr)
	cfg, _ := config.LoadDir(e.dir)
	assert.Equal(t, "corp", cfg.Logins[e.host].Users["Jmnote"].Domain)
	_, stderr, code = e.run("", "page", "view", "Sandbox", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []string{"corp", "corp"}, e.wiki.domains)

	_, stderr, code = e.run("tok", "auth", "login", "--wiki", e.api, "--with-token", "--domain", "corp")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--domain is for --username logins")
}

// Logging out of an account password ends its session on the wiki; a token
// has no session to end.
func TestLogoutEndsSession(t *testing.T) {
	e := newEnv(t)
	t.Setenv("MW_PASSWORD", "pw")
	_, stderr, code := e.run("", "auth", "login", "--wiki", e.api, "--username", "Jmnote")
	require.Equal(t, 0, code, stderr)
	_, stderr, code = e.run("", "auth", "logout", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, 1, e.wiki.logouts)
	assert.NotContains(t, stderr, "Could not end the session")

	e.login()
	_, stderr, code = e.run("", "auth", "logout", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, 1, e.wiki.logouts)
}

// --baserevid makes an edit made from an older revision fail with an edit
// conflict, unless the wiki merges it, rather than undo the edits since.
func TestPageEditBaserevid(t *testing.T) {
	e := newEnv(t)
	e.login()
	_, stderr, code := e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "Mine", "--baserevid", "202")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "nocreate baserevid=202", e.wiki.editFlags)

	_, stderr, code = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "Mine", "--baserevid", "201")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "edit conflict: Sandbox changed since revision 201 on "+e.host+"; view the latest text with `mw page view`")
	_, stderr, code = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "merges", "--baserevid", "201")
	require.Equal(t, 0, code, stderr)

	// A dry run says when the page changed since the base revision.
	_, stderr, code = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "Mine", "--baserevid", "201", "--dry-run")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Sandbox changed since revision 201 (now 202)")
	_, stderr, _ = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "Mine", "--baserevid", "202", "--dry-run")
	assert.NotContains(t, stderr, "changed since")

	_, stderr, code = e.run("", "page", "edit", "Sandbox", "--wiki", e.api, "--text", "x", "--baserevid", "0")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--baserevid must be a revision ID greater than 0")
	_, stderr, code = e.run("", "page", "create", "New/Three", "--wiki", e.api, "--text", "x", "--baserevid", "202")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "unknown flag: --baserevid")
}

// A first login that fails adds no site and does not say it did.
func TestFailedFirstLoginAddsNoSite(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "auth", "login", "--wiki", e.api)
	assert.Equal(t, 2, code)
	assert.NotContains(t, stderr, "Added site")
	cfg, err := config.LoadDir(e.dir)
	require.NoError(t, err)
	assert.Empty(t, cfg.Farms)

	_, stderr, code = e.run(goodToken+"\n", "auth", "login", "--wiki", e.api, "--with-token")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "Added site "+e.host)
	assert.Less(t, strings.Index(stderr, "Added site"), strings.Index(stderr, "Logged in"))
}

func TestBrowse(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "browse", "--wiki", e.api, "Main Page", "-n")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, e.base+"/w/index.php?title=Main+Page\n", out)
	assert.Empty(t, e.browsed)

	out, _, code = e.run("", "browse", "--wiki", e.api, "--no-browser")
	require.Equal(t, 0, code)
	assert.Equal(t, e.base+"/w/index.php\n", out)
	out, _, code = e.run("", "browse", "--wiki", e.api, "", "-n")
	require.Equal(t, 0, code)
	assert.Equal(t, e.base+"/w/index.php\n", out)

	e.stdoutTTY = true
	out, stderr, code = e.run("", "browse", "--wiki", e.api, "Special:RecentChanges")
	require.Equal(t, 0, code, stderr)
	assert.Empty(t, out)
	assert.Equal(t, e.base+"/w/index.php?title=Special%3ARecentChanges", e.browsed)
	assert.Contains(t, stderr, "Opening "+e.browsed+" in your browser.")
	e.stdoutTTY = false

	_, _, code = e.run("", "browse", "--wiki", e.api, "A", "B")
	assert.Equal(t, 2, code)
}

func TestSiteView(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "site", "view", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, heredoc.Doc(`
		sitename:	Fake
		url:	https://fake.test/wiki/Main_Page
		api:	`+e.api+`
		generator:	MediaWiki 1.43.1
		phpversion:	8.3.1
		dbtype:	mysql
		dbversion:	10.11.6-MariaDB
		lang:	ko
		pages:	1234567
		articles:	89012
		edits:	3456789
		images:	12
		users:	345
		activeusers:	6
		admins:	2
		extensions:	2
		readonly:	false
	`), out)
	assert.Equal(t, "general|statistics|extensions", e.wiki.siprops[len(e.wiki.siprops)-1])

	e.stdoutTTY = true
	out, stderr, code = e.run("", "site", "view", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "\x1b[1mFake\x1b[0m\n"+heredoc.Doc(`
		https://fake.test/wiki/Main_Page

		Software    MediaWiki 1.43.1 (PHP 8.3.1, mysql 10.11.6-MariaDB)
		Language    ko
		API         `+e.api+`
		Pages       1,234,567 (89,012 articles)
		Edits       3,456,789
		Files       12
		Users       345 (6 active, 2 admins)
		Extensions  2
	`), out)
	e.stdoutTTY = false

	out, stderr, code = e.run("", "site", "view", e.api, "--json", "sitename,api,extensions")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `{"sitename":"Fake","api":"`+e.api+`","extensions":[
		{"name":"Cite","type":"parserhook","version":"","url":"https://www.mediawiki.org/wiki/Extension:Cite","license":"GPL-2.0-or-later"},
		{"name":"Vector","type":"skin","version":"1.0.0","url":"https://www.mediawiki.org/wiki/Skin:Vector","license":""}]}`, out)
	assert.Equal(t, "general|extensions", e.wiki.siprops[len(e.wiki.siprops)-1])

	out, _, code = e.run("", "site", "view", e.api, "--json", "statistics", "--jq", ".statistics.articles")
	require.Equal(t, 0, code)
	assert.Equal(t, "89012\n", out)

	_, stderr, code = e.run("", "site", "view", e.api, "--web")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, e.base+"/w/index.php", e.browsed)

	_, stderr, code = e.run("", "site", "view", e.api, "--wiki", e.api)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "either as an argument or with --wiki")
	_, _, code = e.run("", "site", "view", e.api, "--web", "--json", "api")
	assert.Equal(t, 2, code)
}

func TestUserContribs(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "user", "contribs", "Jmnote", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "303\t2026-11-22T08:41:00Z\tSandbox\t-3\tm*\tFix typo again\n"+
		"205\t2026-11-21T01:02:03Z\tUser:Jmnote/새 문서\t+240\tN*\tNew page\n"+
		"101\t2026-11-20T00:00:00Z\tSandbox\t0\t\t\n", out)
	require.Len(t, e.wiki.lists, 2)
	assert.Equal(t, "Jmnote", e.wiki.lists[0].Get("ucuser"))
	assert.Equal(t, "30", e.wiki.lists[0].Get("uclimit"))
	assert.Equal(t, "2", e.wiki.lists[1].Get("uccontinue"))
	assert.False(t, e.wiki.lists[0].Has("ucnamespace"))

	e.stdoutTTY = true
	out, stderr, code = e.run("", "user", "contribs", "Jmnote", "--wiki", e.api, "-L", "2")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, heredoc.Doc(`
		2026-11-22 08:41  Sandbox              -3    m*  Fix typo again
		2026-11-21 01:02  User:Jmnote/새 문서  +240  N*  New page
	`), out)

	_, stderr, code = e.run("", "user", "contribs", "Nobody", "--wiki", e.api)
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "No contributions found")
	e.stdoutTTY = false

	e.wiki.lists = nil
	out, stderr, code = e.run("", "user", "contribs", "Jmnote", "--wiki", e.api, "-n", "2", "-L", "1", "--json", "title,new,tags")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{"title":"Sandbox","new":false,"tags":["mw-undo"]}]`, out)
	assert.Equal(t, "2", e.wiki.lists[0].Get("ucnamespace"))
	assert.Equal(t, "1", e.wiki.lists[0].Get("uclimit"))

	out, _, code = e.run("", "user", "contribs", "Jmnote", "--wiki", e.api, "--json", "tags", "--jq", ".[2].tags")
	require.Equal(t, 0, code)
	assert.Equal(t, "[]\n", out)

	out, _, code = e.run("", "user", "contribs", "Nobody", "--wiki", e.api, "--json", "title")
	require.Equal(t, 0, code)
	assert.Equal(t, "[]\n", out)

	_, stderr, code = e.run("", "user", "contribs", "Jmnote", "--wiki", e.api, "--web")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, e.base+"/w/index.php?title=Special%3AContributions%2FJmnote", e.browsed)

	for _, args := range [][]string{{"a|b"}, {" "}, {"Jmnote", "-L", "0"}, {"Jmnote", "--web", "--json", "title"}} {
		_, _, code = e.run("", append([]string{"user", "contribs", "--wiki", e.api}, args...)...)
		assert.Equal(t, 2, code, args)
	}
}

func TestPageListPrefixNamespace(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		prefix, ns, apprefix string
	}{
		{"help:Edit", "12", "Edit"},
		{"도움말:편집", "12", "편집"},
		{"Help: Edit", "12", "Edit"},
		{"Help:", "12", ""},
		{"Star Wars: E", "", "Star Wars: E"},
		{":Main", "", ":Main"},
	} {
		e.wiki.lists = nil
		_, stderr, code := e.run("", "page", "list", "--wiki", e.api, "--prefix", c.prefix, "-L", "1")
		require.Equal(t, 0, code, c.prefix, stderr)
		require.NotEmpty(t, e.wiki.lists, c.prefix)
		assert.Equal(t, c.ns, e.wiki.lists[0].Get("apnamespace"), c.prefix)
		assert.Equal(t, c.apprefix, e.wiki.lists[0].Get("apprefix"), c.prefix)
	}

	e.wiki.lists = nil
	_, stderr, code := e.run("", "page", "list", "--wiki", e.api, "--prefix", "Help:Edit", "-n", "12", "-L", "1")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "12", e.wiki.lists[0].Get("apnamespace"))

	_, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--prefix", "Help:Edit", "-n", "0")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, `--prefix "Help:Edit" is in namespace 12, not 0 of --namespace`)
}

func TestPageDownload(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err, name)
		return string(b)
	}
	_, stderr, code := e.run("", "page", "download", "--wiki", e.api, "-D", dir, "Help:Edit/ko", "sandbox", "Sandbox", "Module:Args", "MediaWiki:Common.css", "Big", "50%")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Text of Help:Edit/ko", read("Help%3AEdit%2Fko.wiki"))
	assert.Equal(t, "Text of Sandbox", read("Sandbox.wiki"))
	assert.Equal(t, "Text of Module:Args", read("Module%3AArgs.lua"))
	assert.Equal(t, "Text of MediaWiki:Common.css", read("MediaWiki%3ACommon.css"))
	assert.Equal(t, "Text of Big", read("Big.wiki"))
	assert.Equal(t, "Text of 50%", read("50%25.wiki"))
	entries, _ := os.ReadDir(dir)
	assert.Len(t, entries, 6)

	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "-D", dir, "Sandbox")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Sandbox.wiki exists; use --clobber to overwrite it or --skip-existing to keep it")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Sandbox.wiki"), []byte("mine"), 0o644))
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "-D", dir, "Sandbox", "--skip-existing")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "mine", read("Sandbox.wiki"))
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "-D", dir, "Sandbox", "--clobber")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "Text of Sandbox", read("Sandbox.wiki"))

	// Missing and invalid pages are reported after the others are saved.
	_, stderr, code = e.run("A\n\n Missing page \r\nBad<title\n", "page", "download", "--wiki", e.api, "-D", dir, "-")
	assert.Equal(t, 1, code)
	assert.Equal(t, "Text of A", read("A.wiki"))
	assert.Contains(t, stderr, "page does not exist: Missing page")
	assert.Contains(t, stderr, `invalid title "Bad<title"`)

	// Fifty titles a request; a list source picks the pages as page list does.
	e.wiki.batches = nil
	sub := filepath.Join(dir, "all")
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "-D", sub, "--prefix", "P", "-L", "7")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []int{7}, e.wiki.batches)
	assert.Equal(t, "Text of P7", read(filepath.Join("all", "P7.wiki")))
	e.wiki.batches = nil
	var many []string
	for i := range 120 {
		many = append(many, fmt.Sprintf("Page %d", i))
	}
	_, stderr, code = e.run(strings.Join(many, "\n"), "page", "download", "--wiki", e.api, "-D", filepath.Join(dir, "many"), "-")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []int{50, 50, 20}, e.wiki.batches)

	for _, args := range [][]string{
		{},
		{"A", "--category", "X"},
		{"A", "--clobber", "--skip-existing"},
		{"-", "-"},
		{" "},
		{"--prefix", "P", "-L", "0"},
	} {
		_, _, code = e.run("", append([]string{"page", "download", "--wiki", e.api, "-D", dir}, args...)...)
		assert.Equal(t, 2, code, args)
	}
}

func TestPageMove(t *testing.T) {
	e := newEnv(t)
	_, stderr, code := e.run("", "page", "move", "--wiki", e.api, "Old", "New")
	assert.Equal(t, 4, code)
	assert.Contains(t, stderr, "not logged in")
	assert.Empty(t, e.wiki.writes)

	e.login()
	_, stderr, code = e.run("", "page", "move", "--wiki", e.api, "Old", "New", "--reason", "Better name")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "✓ Moved Old to New on "+e.host+"\n", stderr)
	w := e.wiki.writes[len(e.wiki.writes)-1]
	assert.Equal(t, "Better name", w.Get("reason"))
	assert.Equal(t, "tok+\\", w.Get("token"))
	for _, k := range []string{"movetalk", "movesubpages", "noredirect"} {
		assert.False(t, w.Has(k), k)
	}

	_, stderr, code = e.run("", "page", "move", "--wiki", e.api, "Old", "New", "--move-talk", "--move-subpages", "--no-redirect")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "  Moved Talk:Old to Talk:New\n")
	assert.Contains(t, stderr, "  Moved Old/A to New/A\n")
	assert.Contains(t, stderr, "  Left no redirect at Old\n")
	w = e.wiki.writes[len(e.wiki.writes)-1]
	for _, k := range []string{"movetalk", "movesubpages", "noredirect"} {
		assert.Equal(t, "1", w.Get(k), k)
	}

	for _, c := range []struct {
		from, to, want string
	}{
		{"Missing", "New", "page does not exist: Missing on " + e.host},
		{"Old", "Taken", "page already exists: Taken on " + e.host + "; choose another title"},
		{"Same", "Same", "the new title is the same as the old one: Same on " + e.host},
	} {
		_, stderr, code = e.run("", "page", "move", "--wiki", e.api, c.from, c.to)
		assert.Equal(t, 1, code, c.from)
		assert.Contains(t, stderr, c.want)
	}
	_, _, code = e.run("", "page", "move", "--wiki", e.api, "Old")
	assert.Equal(t, 2, code)
	_, _, code = e.run("", "page", "move", "--wiki", e.api, "Old", " ")
	assert.Equal(t, 2, code)
}

func TestPageDelete(t *testing.T) {
	e := newEnv(t)
	e.login()
	_, stderr, code := e.run("", "page", "delete", "--wiki", e.api, "Old page")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--yes required when not running interactively")
	assert.Empty(t, e.wiki.writes)

	_, stderr, code = e.run("", "page", "delete", "--wiki", e.api, "Old page", "--yes", "--reason", "Spam")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "✓ Deleted Old page on "+e.host+"\n", stderr)
	assert.Equal(t, "Spam", e.wiki.writes[0].Get("reason"))

	e.interactive = true
	_, stderr, code = e.run("Old pag\n", "page", "delete", "--wiki", e.api, "Old page")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Type Old page to delete it from "+e.host+": ")
	assert.Contains(t, stderr, "the title did not match; nothing was deleted")
	assert.Len(t, e.wiki.writes, 1)

	_, stderr, code = e.run("Old page\n", "page", "delete", "--wiki", e.api, "Old page")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, stderr, "✓ Deleted Old page on "+e.host)
	e.interactive = false

	_, stderr, code = e.run("", "page", "delete", "--wiki", e.api, "Missing", "--yes")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "page does not exist: Missing on "+e.host)
}

func TestAPIREST(t *testing.T) {
	e := newEnv(t)
	out, stderr, code := e.run("", "api", "--wiki", e.api, "/v1/page/Sandbox/bare", "--jq", ".latest.id")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "303\n", out)
	r := e.wiki.rests[0]
	assert.Equal(t, "GET", r.Method)
	assert.Empty(t, r.Body)

	_, stderr, code = e.run("", "api", "--wiki", e.api, "/v1/page/A%2FB/bare?redirect=no", "limit=5")
	require.Equal(t, 0, code, stderr)
	r = e.wiki.rests[1]
	assert.Equal(t, "/w/rest.php/v1/page/A%2FB/bare", r.Path)
	assert.Equal(t, "no", r.Query.Get("redirect"))
	assert.Equal(t, "5", r.Query.Get("limit"))

	out, stderr, code = e.run("", "api", "--wiki", e.api, "/v1/page/Missing/bare")
	assert.Equal(t, 1, code)
	assert.JSONEq(t, `{"errorKey":"rest-nonexistent-title","httpCode":404}`, out)
	assert.Contains(t, stderr, "MediaWiki returned 404 Not Found")

	out, _, code = e.run("", "api", "--wiki", e.api, "/v1/text")
	require.Equal(t, 0, code)
	assert.Equal(t, "plain\n", out)
	_, stderr, code = e.run("", "api", "--wiki", e.api, "/v1/text", "--jq", ".")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "not JSON")

	// Fields make a JSON body; without OAuth a CSRF token is added.
	out, stderr, code = e.run("", "api", "--wiki", e.api, "-X", "POST", "/v1/page", "title=New", "-f", "source=Hi", "-F", "n=3", "-F", "ok=true", "-F", "none=null")
	require.Equal(t, 0, code, stderr)
	r = e.wiki.rests[len(e.wiki.rests)-1]
	assert.Equal(t, "application/json", r.Type)
	assert.JSONEq(t, `{"title":"New","source":"Hi","n":3,"ok":true,"none":null,"token":"tok+\\"}`, r.Body)
	assert.Contains(t, out, `"id":8`)

	e.login()
	_, stderr, code = e.run("{\"source\":\"x\"}", "api", "--wiki", e.api, "-X", "POST", "/v1/page", "--input", "-", "q=1")
	require.Equal(t, 0, code, stderr)
	r = e.wiki.rests[len(e.wiki.rests)-1]
	assert.True(t, r.Authed)
	assert.Equal(t, `{"source":"x"}`, r.Body, "the input is sent as it is, with no token for OAuth")
	assert.Equal(t, "1", r.Query.Get("q"))

	_, stderr, code = e.run("", "api", "--wiki", e.api, "-X", "POST", "/v1/page", "title=A")
	require.Equal(t, 0, code, stderr)
	assert.NotContains(t, e.wiki.rests[len(e.wiki.rests)-1].Body, "token")

	for _, args := range [][]string{
		{"/v1/page", "--paginate"},
		{"/v1/page", "--slurp"},
		{"action=query", "--input", "x.json"},
		{"-X", "POST", "/v1/page", "a=1", "a=2"},
		{"/v1/page", "nokey"},
	} {
		_, _, code = e.run("", append([]string{"api", "--wiki", e.api}, args...)...)
		assert.Equal(t, 2, code, args)
	}
}

func TestFileView(t *testing.T) {
	e := newEnv(t)
	sum := func(s string) string {
		h := sha1.Sum([]byte(s))
		return hex.EncodeToString(h[:])
	}

	out, stderr, code := e.run("", "file", "view", "--wiki", e.api, "Example.jpg")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, heredoc.Doc(`
		title:	File:Example.jpg
		url:	`+e.base+`/files/Example.jpg
		descriptionurl:	`+e.base+`/wiki/File:Example.jpg
		size:	10
		width:	300
		height:	200
		mime:	image/jpeg
		mediatype:	BITMAP
		timestamp:	2026-11-22T01:02:03Z
		user:	Jmnote
		comment:	Sample photo of wiki
		sha1:	`+sum("jpeg-bytes")+`
		repository:	local
	`), out)

	e.stdoutTTY = true
	out, stderr, code = e.run("", "file", "view", "--wiki", e.api, "Example.jpg")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "\x1b[1mFile:Example.jpg\x1b[0m\n"+heredoc.Doc(`
		`+e.base+`/files/Example.jpg

		Size      10 bytes (300 × 200)
		Type      image/jpeg (BITMAP)
		Uploaded  2026-11-22 01:02 by Jmnote
		Summary   Sample photo of wiki
	`), out)

	out, stderr, code = e.run("", "file", "view", "--wiki", e.api, "파일:Shared.png")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "Repository  shared, http://commons.example/wiki/File:Shared.png")

	out, stderr, code = e.run("", "file", "view", "--wiki", e.api, "Audio.ogg")
	require.Equal(t, 0, code, stderr)
	assert.Contains(t, out, "Size      9 bytes\n")
	assert.NotContains(t, out, "0 × 0")
	assert.Contains(t, out, "Uploaded  2026-11-21 12:00\n")
	assert.NotContains(t, out, " by ")
	assert.NotContains(t, out, "Summary")
	e.stdoutTTY = false

	out, stderr, code = e.run("", "file", "view", "--wiki", e.api, "A: B.jpg", "--json", "title,url,sha1,repository")
	require.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `{"title":"File:A: B.jpg","url":"`+e.base+`/files/A_B.jpg","sha1":"`+sum("colon-file-bytes")+`","repository":"local"}`, out)

	out, _, code = e.run("", "file", "view", "--wiki", e.api, "Example.jpg", "--json", "sha1", "--jq", ".sha1")
	require.Equal(t, 0, code)
	assert.Equal(t, sum("jpeg-bytes")+"\n", out)

	_, stderr, code = e.run("", "file", "view", "--wiki", e.api, "Shared.png", "--web")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "http://commons.example/wiki/File:Shared.png", e.browsed)

	_, stderr, code = e.run("", "file", "view", "--wiki", e.api, "NoSuch.png")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "file does not exist: File:NoSuch.png on "+e.host)

	_, stderr, code = e.run("", "file", "view", "--wiki", e.api, "Bad<file.png")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `invalid file name "Bad<file.png"`)

	for _, args := range [][]string{
		{},
		{"Example.jpg", "Other.jpg"},
		{" "},
		{"Example.jpg", "--web", "--json", "url"},
	} {
		_, _, code = e.run("", append([]string{"file", "view", "--wiki", e.api}, args...)...)
		assert.Equal(t, 2, code, args)
	}
}

func TestFileDownload(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err, name)
		return string(b)
	}

	_, stderr, code := e.run("", "file", "download", "--wiki", e.api, "-D", dir, "Example.jpg", "example.jpg", "파일:Shared.png", "A: B.jpg")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "jpeg-bytes", read("Example.jpg"))
	assert.Equal(t, "shared-png-bytes", read("Shared.png"))
	assert.Equal(t, "colon-file-bytes", read("A%3A B.jpg"))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	_, stderr, code = e.run("", "file", "download", "--wiki", e.api, "-D", dir, "Example.jpg")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Example.jpg exists; use --clobber to overwrite it or --skip-existing to keep it")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Example.jpg"), []byte("mine"), 0o644))
	_, stderr, code = e.run("", "file", "download", "--wiki", e.api, "-D", dir, "Example.jpg", "--skip-existing")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "mine", read("Example.jpg"))

	_, stderr, code = e.run("", "file", "download", "--wiki", e.api, "-D", dir, "Example.jpg", "--clobber")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "jpeg-bytes", read("Example.jpg"))

	badDir := t.TempDir()
	_, stderr, code = e.run("", "file", "download", "--wiki", e.api, "-D", badDir, "BadHash.png")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "does not match the wiki's")
	badEntries, err := os.ReadDir(badDir)
	require.NoError(t, err)
	assert.Empty(t, badEntries)

	stdinDir := t.TempDir()
	_, stderr, code = e.run("Audio.ogg\n\n Missing.png \r\nBad<file\nHidden.png\n", "file", "download", "--wiki", e.api, "-D", stdinDir, "-")
	assert.Equal(t, 1, code)
	b, err := os.ReadFile(filepath.Join(stdinDir, "Audio.ogg"))
	require.NoError(t, err)
	assert.Equal(t, "ogg-bytes", string(b))
	assert.Contains(t, stderr, "file does not exist: File:Missing.png")
	assert.Contains(t, stderr, `invalid file name "File:Bad<file"`)
	assert.Contains(t, stderr, "file has no readable version: File:Hidden.png")

	e.wiki.lists = nil
	e.stdoutTTY = true
	catDir := filepath.Join(dir, "cat")
	_, stderr, code = e.run("", "file", "download", "--wiki", e.api, "--category", "Test files", "-L", "5", "-D", catDir)
	e.stdoutTTY = false
	require.Equal(t, 0, code, stderr)
	require.NotEmpty(t, e.wiki.lists)
	assert.Equal(t, "Category:Test files", e.wiki.lists[0].Get("cmtitle"))
	assert.Equal(t, "6", e.wiki.lists[0].Get("cmnamespace"))
	assert.Equal(t, "5", e.wiki.lists[0].Get("cmlimit"))
	assert.Contains(t, stderr, "Downloading File:Example.jpg (10 bytes)")
	assert.Contains(t, stderr, "\x1b[32m✓\x1b[0m Downloaded 2 file(s) to "+catDir)
	assert.Equal(t, "jpeg-bytes", read(filepath.Join("cat", "Example.jpg")))
	assert.Equal(t, "ogg-bytes", read(filepath.Join("cat", "Audio.ogg")))

	for _, args := range [][]string{
		{},
		{"Example.jpg", "--category", "X"},
		{"Example.jpg", "--clobber", "--skip-existing"},
		{"-", "-"},
		{" "},
		{"--category", " "},
		{"--category", "X", "-L", "0"},
	} {
		_, _, code = e.run("", append([]string{"file", "download", "--wiki", e.api, "-D", dir}, args...)...)
		assert.Equal(t, 2, code, args)
	}
}

func TestFileUpload(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	write := func(name string, size int) string {
		p := filepath.Join(dir, name)
		b := make([]byte, size)
		copy(b, "content of "+name)
		require.NoError(t, os.WriteFile(p, b, 0o644))
		return p
	}
	photo := write("photo.jpg", 100)

	_, stderr, code := e.run("", "file", "upload", "--wiki", e.api, photo)
	assert.Equal(t, 4, code)
	assert.Contains(t, stderr, "not logged in")
	assert.Empty(t, e.wiki.uploads)

	e.login()
	out, stderr, code := e.run("", "file", "upload", "--wiki", e.api, photo, "-s", "New photo", "--text", "{{Information}}")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "✓ Uploaded File:photo.jpg on "+e.host+"\n", stderr)
	assert.Equal(t, "http://fake.test/images/photo.jpg\n", out, "protocol-relative URL resolved, utm_ dropped")
	u := e.wiki.uploads[len(e.wiki.uploads)-1]
	assert.Equal(t, uploadRequest{Filename: "photo.jpg", Comment: "New photo", Text: "{{Information}}", Token: "tok+\\", Field: "file", Size: 100, Content: "content of photo"}, u)

	desc := filepath.Join(dir, "desc.wiki")
	require.NoError(t, os.WriteFile(desc, []byte("== Summary =="), 0o644))
	for _, name := range []string{"Report 1.png", "File:Report 1.png", "파일:Report 1.png"} {
		_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, photo, "--name", name, "--text-file", desc)
		require.Equal(t, 0, code, stderr)
		u = e.wiki.uploads[len(e.wiki.uploads)-1]
		assert.Equal(t, "Report 1.png", u.Filename, name)
		assert.Equal(t, "== Summary ==", u.Text)
	}

	// Warnings stop that file but not the others.
	exists, dup := write("Exists.jpg", 10), write("Dup.jpg", 10)
	e.wiki.uploads = nil
	out, stderr, code = e.run("", "file", "upload", "--wiki", e.api, exists, dup, photo)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "Not uploaded "+exists+": the wiki warned about the upload: exists: Exists.jpg")
	assert.Contains(t, stderr, "Not uploaded "+dup+": the wiki warned about the upload: duplicate: Other.jpg, Third.jpg")
	assert.Contains(t, stderr, "--ignore-warnings")
	assert.Contains(t, stderr, "✓ Uploaded File:photo.jpg")
	assert.Equal(t, "http://fake.test/images/photo.jpg\n", out)
	assert.Len(t, e.wiki.uploads, 3)

	_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, exists, "--ignore-warnings")
	require.Equal(t, 0, code, stderr)
	assert.True(t, e.wiki.uploads[len(e.wiki.uploads)-1].Ignore)

	same := write("Same.jpg", 10)
	_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, same)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "the file is the same as the latest version")

	// A file over 5 MiB goes in parts to the stash, then is published.
	big := write("big.bin", 11<<20)
	e.wiki.uploads = nil
	_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, big, "-s", "Big")
	require.Equal(t, 0, code, stderr)
	require.Len(t, e.wiki.uploads, 4)
	for i, want := range []struct {
		offset string
		size   int
	}{{"0", 5 << 20}, {"5242880", 5 << 20}, {"10485760", 1 << 20}} {
		u := e.wiki.uploads[i]
		assert.True(t, u.Stash, i)
		assert.Equal(t, "chunk", u.Field, i)
		assert.Equal(t, want.offset, u.Offset, i)
		assert.Equal(t, want.size, u.Size, i)
		assert.Equal(t, "11534336", u.FileSize, i)
		assert.Empty(t, u.Comment, i)
	}
	assert.Equal(t, "content of big.b", e.wiki.uploads[0].Content)
	assert.Empty(t, e.wiki.uploads[0].FileKey)
	assert.Equal(t, "fk.1", e.wiki.uploads[1].FileKey)
	for _, u := range e.wiki.uploads {
		assert.False(t, u.Chunked, "every body has a Content-Length")
	}
	final := e.wiki.uploads[3]
	assert.Equal(t, uploadRequest{Filename: "big.bin", FileKey: "fk.1", Comment: "Big", Token: "tok+\\"}, final)

	// A part that fails ends the progress line before the error.
	broken := write("broken.bin", 6<<20)
	e.stdoutTTY = true
	_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, broken)
	e.stdoutTTY = false
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "\rbroken.bin: 83%\nupload "+broken+": upload part at byte 5242880: MediaWiki API error stashfailed")

	for _, args := range [][]string{
		{},
		{photo, exists, "--name", "X.jpg"},
		{photo, "--name", " "},
		{photo, "--text", "a", "--text-file", desc},
		{photo, "--text-file", ""},
		{photo, "--name", "File:"},
		{photo, "--name", "파일: _"},
		{dir},
	} {
		_, _, code = e.run("", append([]string{"file", "upload", "--wiki", e.api}, args...)...)
		assert.Equal(t, 2, code, args)
	}
	_, _, code = e.run("", "file", "upload", "--wiki", e.api, filepath.Join(dir, "nope.jpg"))
	assert.Equal(t, 1, code)
}

func TestFileUploadSmallerParts(t *testing.T) {
	e := newEnv(t)
	e.login()
	dir := t.TempDir()
	p := filepath.Join(dir, "scan.png")
	require.NoError(t, os.WriteFile(p, make([]byte, 3<<20), 0o644))

	// A server in front of the wiki takes 1 MiB a request: 3 MiB in one
	// request, then 1.5 MiB parts are refused, and 768 KiB parts go through.
	e.wiki.maxBody = 1 << 20
	_, stderr, code := e.run("", "file", "upload", "--wiki", e.api, p)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, 2, e.wiki.tooLarge)
	assert.Contains(t, stderr, "The server refused a request that large; sending scan.png in parts of 1.5 MiB\n")
	assert.Contains(t, stderr, "The server refused a request that large; sending scan.png in parts of 768 KiB\n")
	assert.Contains(t, stderr, "✓ Uploaded File:scan.png")
	var offsets []string
	for _, u := range e.wiki.uploads {
		if u.Stash {
			offsets = append(offsets, u.Offset)
			assert.Equal(t, 768<<10, u.Size)
		}
	}
	assert.Equal(t, []string{"0", "786432", "1572864", "2359296"}, offsets)
	final := e.wiki.uploads[len(e.wiki.uploads)-1]
	assert.Equal(t, "fk.1", final.FileKey)

	// The next file of the same command starts at the smaller size.
	q := filepath.Join(dir, "scan2.png")
	require.NoError(t, os.WriteFile(q, make([]byte, 3<<20), 0o644))
	e.wiki.uploads, e.wiki.tooLarge = nil, 0
	_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, p, q)
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, 2, e.wiki.tooLarge, "only the first file meets the limit")
	assert.Equal(t, 1, strings.Count(stderr, "parts of 768 KiB"))

	// Below the smallest part, the error says so in one line.
	e.wiki.maxBody = 10 << 10
	_, stderr, code = e.run("", "file", "upload", "--wiki", e.api, p)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "upload part at byte 0: MediaWiki returned 413 Request Entity Too Large: HTML error page (a server in front of the wiki limits the size of a request)")
	assert.NotContains(t, stderr, "<html>")
}

func TestPageRedirects(t *testing.T) {
	e := newEnv(t)

	// page view without --follow-redirects keeps redirect wikitext.
	stdout, stderr, code := e.run("", "page", "view", "--wiki", e.api, "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "#REDIRECT [[Target]]", stdout)

	// page view --follow-redirects resolves the target page and metadata.
	stdout, stderr, code = e.run("", "page", "view", "--wiki", e.api, "--follow-redirects", "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "Target text", stdout)

	stdout, stderr, code = e.run("", "page", "view", "--wiki", e.api, "--follow-redirects", "--json", "title,pageid,redirect,content", "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `{"title":"Target","pageid":8,"redirect":false,"content":"Target text"}`, stdout)

	_, stderr, code = e.run("", "page", "view", "--wiki", e.api, "--web", "--follow-redirects", "Alias")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "--follow-redirects")

	// page history without vs with --follow-redirects.
	stdout, stderr, code = e.run("", "page", "history", "--wiki", e.api, "--json", "revid", "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{"revid":90}]`, stdout)

	stdout, stderr, code = e.run("", "page", "history", "--wiki", e.api, "--follow-redirects", "--json", "revid", "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.JSONEq(t, `[{"revid":105},{"revid":104},{"revid":103},{"revid":102},{"revid":101}]`, stdout)

	// page diff without vs with --follow-redirects.
	stdout, stderr, code = e.run("", "page", "diff", "--wiki", e.api, "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "+++ Alias\t")
	assert.NotContains(t, stdout, "+++ Sandbox\t")

	stdout, stderr, code = e.run("", "page", "diff", "--wiki", e.api, "--follow-redirects", "Alias")
	assert.Equal(t, 0, code, stderr)
	assert.Contains(t, stdout, "+++ Sandbox\t")

	// page list --redirects-to and namespace filter.
	stdout, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--redirects-to", "Main Page")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "R1\nR2\n", stdout)

	stdout, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--redirects-to", "Main Page", "-n", "14")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "Category:R3\n", stdout)

	// page list --links-to with --follow-redirects and --filter-redirects.
	stdout, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--links-to", "Main Page", "--follow-redirects")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "Direct\nRedir\nViaRedir\n", stdout)

	stdout, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--links-to", "Main Page", "--follow-redirects", "--filter-redirects", "nonredirects")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "Direct\nViaRedir\n", stdout)

	stdout, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--links-to", "Main Page", "--follow-redirects", "--filter-redirects", "redirects")
	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "Redir\nDoubleRedir\n", stdout)

	// Explicit --filter-redirects all is a no-op with any source.
	_, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--category", "Foo", "--filter-redirects", "all")
	assert.Equal(t, 0, code, stderr)
	_, stderr, code = e.run("", "page", "list", "--wiki", e.api, "--redirects-to", "Main Page", "--filter-redirects", "all")
	assert.Equal(t, 0, code, stderr)

	// Invalid page list flag combinations return exit code 2.
	for _, args := range [][]string{
		{"page", "list", "--wiki", e.api, "--redirects-to", "Main Page", "--prefix", "A"},
		{"page", "list", "--wiki", e.api, "--redirects-to", "Main Page", "--filter-redirects", "redirects"},
		{"page", "list", "--wiki", e.api, "--prefix", "A", "--follow-redirects"},
		{"page", "list", "--wiki", e.api, "--filter-redirects", "invalid"},
	} {
		_, stderr, code = e.run("", args...)
		assert.Equal(t, 2, code, "args=%v stderr=%s", args, stderr)
	}

	// Direct interwiki title reports an interwiki title error without calling it a redirect.
	_, stderr, code = e.run("", "page", "view", "--wiki", e.api, "wikipedia:Foo")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `"wikipedia:Foo" is an interwiki title (wikipedia:Foo)`)
	assert.NotContains(t, stderr, "redirect")

	// page download --follow-redirects writes under target filename.
	dlDir := filepath.Join(t.TempDir(), "dl-follow")
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "--follow-redirects", "--dir", dlDir, "Alias")
	assert.Equal(t, 0, code, stderr)
	got, err := os.ReadFile(filepath.Join(dlDir, "Target.wiki"))
	assert.NoError(t, err)
	assert.Equal(t, "Text of Target", string(got))

	// page download --filter-redirects all with positional titles succeeds.
	allDir := filepath.Join(t.TempDir(), "dl-all")
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "--filter-redirects", "all", "--dir", allDir, "Alias")
	assert.Equal(t, 0, code, stderr)
	gotAlias, err := os.ReadFile(filepath.Join(allDir, "Alias.wiki"))
	assert.NoError(t, err)
	assert.Equal(t, "#REDIRECT [[Target]]", string(gotAlias))

	// page download --follow-redirects errors on interwiki redirect, and direct interwiki uses distinct wording.
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "--follow-redirects", "--dir", filepath.Join(t.TempDir(), "dl-iw"), "CrossWiki")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `redirect "CrossWiki" points to another wiki (w:Target)`)

	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "--dir", filepath.Join(t.TempDir(), "dl-direct-iw"), "wikipedia:Foo")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `"wikipedia:Foo" is an interwiki title (wikipedia:Foo)`)
	assert.NotContains(t, stderr, "redirect")

	// page download --redirects-to downloads all redirects to target.
	redDir := filepath.Join(t.TempDir(), "dl-redir")
	_, stderr, code = e.run("", "page", "download", "--wiki", e.api, "--redirects-to", "Main Page", "--dir", redDir)
	assert.Equal(t, 0, code, stderr)
	gotR1, err := os.ReadFile(filepath.Join(redDir, "R1.wiki"))
	assert.NoError(t, err)
	assert.Equal(t, "Text of R1", string(gotR1))
	gotR2, err := os.ReadFile(filepath.Join(redDir, "R2.wiki"))
	assert.NoError(t, err)
	assert.Equal(t, "Text of R2", string(gotR2))

	// Invalid page download flag combinations return exit code 2.
	for _, args := range [][]string{
		{"page", "download", "--wiki", e.api, "--redirects-to", "Main Page", "Extra"},
		{"page", "download", "--wiki", e.api, "--redirects-to", "Main Page", "--filter-redirects", "redirects"},
		{"page", "download", "--wiki", e.api, "--filter-redirects", "invalid"},
	} {
		_, stderr, code = e.run("", args...)
		assert.Equal(t, 2, code, "args=%v stderr=%s", args, stderr)
	}
}
