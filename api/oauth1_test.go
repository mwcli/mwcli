package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The example of the OAuth Core 1.0 specification, appendix A.5.
func TestOAuth1SpecExample(t *testing.T) {
	o := &OAuth1{ConsumerKey: "dpf43f3p2l4k3l03", ConsumerSecret: "kd94hf93k423kf44", AccessToken: "nnch734d00sl2jdk", AccessSecret: "pfkkdhi9sl3r4s00"}
	h := o.header("GET", "http://photos.example.net/photos", url.Values{"file": {"vacation.jpg"}, "size": {"original"}}, "1191242096", "kllo9940pd9333jh")
	assert.Contains(t, h, `oauth_signature="tR3%2BTy81lMeYAr%2FFid0kMTYa%2FWM%3D"`)
	assert.True(t, strings.HasPrefix(h, "OAuth "))
}

// Names that are prefixes of others sort by name first: a before a-b, though
// "a-b=" sorts before "a=" as a string. Values of one name sort by value.
func TestBaseStringOrder(t *testing.T) {
	got := baseString("post", "https://example.com/w/api.php", url.Values{"a-b": {"2"}, "a": {"z", "1"}, "b": {"x"}})
	assert.Equal(t, "POST&https%3A%2F%2Fexample.com%2Fw%2Fapi.php&"+percentEncode("a=1&a=z&a-b=2&b=x"), got)
}

func TestPercentEncode(t *testing.T) {
	assert.Equal(t, "Ladies%20%2B%20Gentlemen", percentEncode("Ladies + Gentlemen"))
	assert.Equal(t, "An%20encoded%20string%21", percentEncode("An encoded string!"))
	assert.Equal(t, "%E2%98%83-._~", percentEncode("☃-._~"))
}

func TestBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"HTTP://Example.COM:80/w/api.php?a=1": "http://example.com/w/api.php",
		"https://example.com:443/w/api.php":   "https://example.com/w/api.php",
		"https://example.com:8443/w/api.php":  "https://example.com:8443/w/api.php",
		"https://example.com":                 "https://example.com/",
		"https://[::1]:8443/w/api.php":        "https://[::1]:8443/w/api.php",
		"https://[2001:DB8::1]:443/w/api.php": "https://[2001:db8::1]/w/api.php",
	} {
		u, err := url.Parse(in)
		require.NoError(t, err)
		assert.Equal(t, want, baseURL(u), in)
	}
}

// verifyOAuth1 checks a request as the wiki does: it recomputes the signature
// from the request URL, its query and form values, and the oauth_ parameters.
func verifyOAuth1(r *http.Request, o *OAuth1) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "OAuth ") {
		return false
	}
	got := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(h, "OAuth "), ", ") {
		k, v, _ := strings.Cut(part, "=")
		v, _ = url.QueryUnescape(strings.Trim(v, `"`))
		got[k] = v
	}
	if got["oauth_consumer_key"] != o.ConsumerKey || got["oauth_token"] != o.AccessToken {
		return false
	}
	_ = r.ParseForm()
	u := *r.URL
	u.Scheme, u.Host = "http", r.Host
	want := o.header(r.Method, baseURL(&u), r.Form, got["oauth_timestamp"], got["oauth_nonce"])
	return strings.Contains(want, `oauth_signature="`+percentEncode(got["oauth_signature"])+`"`)
}

func TestOAuth1SignsGetAndPost(t *testing.T) {
	keys := &OAuth1{ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}
	var edits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !verifyOAuth1(r, keys) {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "bad signature"}})
			return
		}
		switch r.Form.Get("action") {
		case "edit":
			edits++
			_ = json.NewEncoder(w).Encode(map[string]any{"edit": map[string]string{"result": "Success", "title": r.Form.Get("title")}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"tokens": map[string]string{"csrftoken": "t+\\"}}})
		}
	}))
	defer srv.Close()
	account := OAuth1{ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}
	c, err := New(srv.URL+"/w/api.php", WithOAuth1(account))
	require.NoError(t, err)
	res, err := c.Edit(context.Background(), "A b/c", "Text with spaces, + and ~ and 한글", EditOptions{Summary: "summary!"})
	require.NoError(t, err)
	assert.Equal(t, "A b/c", res.Title)
	assert.Equal(t, 1, edits)

	account.AccessSecret = "wrong"
	c, err = New(srv.URL+"/w/api.php", WithOAuth1(account))
	require.NoError(t, err)
	_, err = c.Edit(context.Background(), "A", "x", EditOptions{})
	assert.ErrorContains(t, err, "mwoauth-invalid-authorization")
}

// A GET redirected within the host is signed for its new URL; one to another
// host carries no OAuth header.
func TestOAuth1Redirect(t *testing.T) {
	keys := &OAuth1{ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}
	var otherAuth string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 0, "name": "1.2.3.4", "anon": true}}})
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old/api.php":
			http.Redirect(w, r, "/w/api.php?"+r.URL.RawQuery, http.StatusMovedPermanently)
		case "/away/api.php":
			http.Redirect(w, r, other.URL+"/w/api.php?"+r.URL.RawQuery, http.StatusMovedPermanently)
		default:
			w.Header().Set("Content-Type", "application/json")
			if !verifyOAuth1(r, keys) {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "bad signature"}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"userinfo": map[string]any{"id": 7, "name": "Alice"}}})
		}
	}))
	defer srv.Close()
	account := OAuth1{ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}
	c, err := New(srv.URL+"/old/api.php", WithOAuth1(account))
	require.NoError(t, err)
	u, err := c.User(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Alice", u.Name)

	c, err = New(srv.URL+"/away/api.php", WithOAuth1(account))
	require.NoError(t, err)
	_, err = c.CurrentUser(context.Background())
	require.NoError(t, err)
	assert.Empty(t, otherAuth)
}

// A POST to an API URL with a query signs the query and the form body. A
// parameter of the request replaces a query value of the same name. The GET
// for the token keeps the query too, so both reach the same wiki.
func TestOAuth1PostWithQuery(t *testing.T) {
	keys := &OAuth1{ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !verifyOAuth1(r, keys) {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "mwoauth-invalid-authorization", "info": "bad signature"}})
			return
		}
		forms = append(forms, r.Form)
		switch r.PostForm.Get("action") {
		case "edit":
			_ = json.NewEncoder(w).Encode(map[string]any{"edit": map[string]string{"result": "Success", "title": r.PostForm.Get("title")}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"tokens": map[string]string{"csrftoken": "t+\\"}}})
		}
	}))
	defer srv.Close()
	account := OAuth1{ConsumerKey: "ck", ConsumerSecret: "cs", AccessToken: "at", AccessSecret: "as"}
	c, err := New(srv.URL+"/w/api.php?wiki=a&title=Q", WithOAuth1(account))
	require.NoError(t, err)
	_, err = c.Edit(context.Background(), "A", "x", EditOptions{})
	require.NoError(t, err)
	require.Len(t, forms, 2)
	assert.Equal(t, "a", forms[0].Get("wiki"))
	assert.Equal(t, []string{"Q"}, forms[0]["title"])
	assert.Equal(t, "tokens", forms[0].Get("meta"))
	assert.Equal(t, []string{"A"}, forms[1]["title"])
	assert.Equal(t, "a", forms[1].Get("wiki"))
}
