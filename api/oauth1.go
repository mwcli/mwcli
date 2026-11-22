package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OAuth1 holds the four keys of an owner-only OAuth 1.0a consumer. Requests are
// signed with HMAC-SHA1 (RFC 5849), which the MediaWiki OAuth extension accepts.
// No module in go.mod signs OAuth 1.0a requests, and the signing is small.
type OAuth1 struct {
	ConsumerKey, ConsumerSecret, AccessToken, AccessSecret string
}

// header returns the Authorization header for a request to endpoint, a URL
// without query, with params from the query or the form body.
func (o *OAuth1) header(method, endpoint string, params url.Values, timestamp, nonce string) string {
	oauth := map[string]string{
		"oauth_consumer_key":     o.ConsumerKey,
		"oauth_token":            o.AccessToken,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        timestamp,
		"oauth_nonce":            nonce,
		"oauth_version":          "1.0",
	}
	all := url.Values{}
	for k, vs := range params {
		all[k] = append([]string(nil), vs...)
	}
	for k, v := range oauth {
		all.Add(k, v)
	}
	mac := hmac.New(sha1.New, []byte(percentEncode(o.ConsumerSecret)+"&"+percentEncode(o.AccessSecret)))
	mac.Write([]byte(baseString(method, endpoint, all)))
	oauth["oauth_signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	keys := make([]string, 0, len(oauth))
	for k := range oauth {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("OAuth ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(percentEncode(k) + `="` + percentEncode(oauth[k]) + `"`)
	}
	return b.String()
}

// baseString is the signature base string of RFC 5849 3.4.1: the encoded
// parameters are sorted by name and, for the same name, by value.
func baseString(method, endpoint string, params url.Values) string {
	type pair struct{ k, v string }
	var pairs []pair
	for k, vs := range params {
		for _, v := range vs {
			pairs = append(pairs, pair{percentEncode(k), percentEncode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	joined := make([]string, len(pairs))
	for i, p := range pairs {
		joined[i] = p.k + "=" + p.v
	}
	return strings.ToUpper(method) + "&" + percentEncode(endpoint) + "&" + percentEncode(strings.Join(joined, "&"))
}

// sign sets the Authorization header of req, signing the query of its URL and
// form, the form body values it carries. A name may appear in both.
func (o *OAuth1) sign(req *http.Request, form url.Values) {
	params := req.URL.Query()
	for k, vs := range form {
		params[k] = append(params[k], vs...)
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	req.Header.Set("Authorization", o.header(req.Method, baseURL(req.URL), params, strconv.FormatInt(time.Now().Unix(), 10), hex.EncodeToString(nonce)))
}

// baseURL is the base string URI of RFC 5849 3.4.1.2: lowercase scheme and
// host, no default port, no query or fragment.
func baseURL(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && !(scheme == "http" && port == "80") && !(scheme == "https" && port == "443") {
		host += ":" + port
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + path
}

// percentEncode encodes s as RFC 5849 3.6 requires: every byte but the
// unreserved characters, with uppercase hex digits.
func percentEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
		}
	}
	return b.String()
}
