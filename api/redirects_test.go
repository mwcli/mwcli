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

func TestFollowRedirectsReads(t *testing.T) {
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		queries = append(queries, r.Form)
		w.Header().Set("Content-Type", "application/json")
		title := r.Form.Get("titles")
		follow := r.Form.Get("redirects") == "1"
		switch {
		case title == "Interwiki" && follow:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{
					"redirects": []any{map[string]string{"from": "Interwiki", "to": "w:Target"}},
					"interwiki": []any{map[string]string{"title": "w:Target", "iw": "w"}},
				},
			})
		case title == "wikipedia:Foo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{
					"interwiki": []any{map[string]string{"title": "wikipedia:Foo", "iw": "wikipedia"}},
				},
			})
		case title == "BrokenRedir" && follow:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{
					"redirects": []any{map[string]string{"from": "BrokenRedir", "to": "MissingTarget"}},
					"pages":     []any{map[string]any{"title": "MissingTarget", "missing": true}},
				},
			})
		case r.Form.Get("rvprop") == "ids|timestamp|user|comment|size|flags|tags":
			target := title
			revs := []any{map[string]any{"revid": 10, "parentid": 0, "timestamp": "2026-11-28T00:00:00Z", "user": "A", "comment": "redir", "size": 20}}
			if title == "Alias" && follow {
				target = "Target"
				revs = []any{
					map[string]any{"revid": 22, "parentid": 21, "timestamp": "2026-11-28T01:00:00Z", "user": "B", "comment": "target 2", "size": 40},
					map[string]any{"revid": 21, "parentid": 0, "timestamp": "2026-11-28T00:30:00Z", "user": "B", "comment": "target 1", "size": 30},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"pages": []any{map[string]any{"title": target, "revisions": revs}}},
			})
		case strings.HasPrefix(r.Form.Get("prop"), "info|revisions"):
			target, redir, content := title, true, "#REDIRECT [[Target]]"
			if title == "Alias" && follow {
				target, redir, content = "Target", false, "Target body"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"pages": []any{map[string]any{
					"title": target, "pageid": 9, "ns": 0, "redirect": redir,
					"revisions": []any{map[string]any{"revid": 99, "slots": map[string]any{"main": map[string]string{"contentmodel": "wikitext", "content": content}}}},
				}}},
			})
		case r.Form.Get("rvprop") == "ids|timestamp|user|content|contentmodel":
			target, revid, content := title, 11, "#REDIRECT [[Target]]"
			if title == "Alias" && follow {
				target, revid, content = "Target", 22, "Target body"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"pages": []any{map[string]any{
					"title": target,
					"revisions": []any{map[string]any{
						"revid": revid, "parentid": 0, "timestamp": "2026-11-28T00:00:00Z", "user": "A",
						"slots": map[string]any{"main": map[string]string{"contentmodel": "wikitext", "content": content}},
					}},
				}}},
			})
		default:
			content := "#REDIRECT [[Target]]"
			if title == "Alias" && follow {
				content = "Target body"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"pages": []any{map[string]any{
					"title": title, "revisions": []any{map[string]any{"slots": map[string]any{"main": map[string]string{"content": content}}}},
				}}},
			})
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	ctx := context.Background()

	txt, err := c.Page(ctx, "Alias", PageOptions{})
	require.NoError(t, err)
	assert.Equal(t, "#REDIRECT [[Target]]", txt)

	txt, err = c.Page(ctx, "Alias", PageOptions{FollowRedirects: true})
	require.NoError(t, err)
	assert.Equal(t, "Target body", txt)

	info, err := c.PageInfo(ctx, "Alias", PageInfoOptions{Content: true, FollowRedirects: true})
	require.NoError(t, err)
	assert.Equal(t, "Target", info.Title)
	assert.False(t, info.Redirect)
	assert.Equal(t, "Target body", info.Content)

	revs, err := c.History(ctx, "Alias", HistoryOptions{Limit: 10, FollowRedirects: true})
	require.NoError(t, err)
	require.Len(t, revs, 2)
	assert.Equal(t, 22, revs[0].RevID)

	rev, err := c.LatestRevision(ctx, "Alias", PageOptions{FollowRedirects: true})
	require.NoError(t, err)
	assert.Equal(t, "Target", rev.Title)
	assert.Equal(t, 22, rev.RevID)

	_, err = c.Page(ctx, "Interwiki", PageOptions{FollowRedirects: true})
	assert.ErrorContains(t, err, `redirect "Interwiki" points to another wiki (w:Target)`)

	_, err = c.PageInfo(ctx, "Interwiki", PageInfoOptions{FollowRedirects: true})
	assert.ErrorContains(t, err, `redirect "Interwiki" points to another wiki (w:Target)`)

	_, err = c.History(ctx, "Interwiki", HistoryOptions{Limit: 5, FollowRedirects: true})
	assert.ErrorContains(t, err, `redirect "Interwiki" points to another wiki (w:Target)`)

	_, err = c.LatestRevision(ctx, "Interwiki", PageOptions{FollowRedirects: true})
	assert.ErrorContains(t, err, `redirect "Interwiki" points to another wiki (w:Target)`)

	for _, call := range []struct {
		name string
		fn   func() error
	}{
		{"Page", func() error { _, err := c.Page(ctx, "wikipedia:Foo", PageOptions{}); return err }},
		{"PageInfo", func() error { _, err := c.PageInfo(ctx, "wikipedia:Foo", PageInfoOptions{}); return err }},
		{"History", func() error { _, err := c.History(ctx, "wikipedia:Foo", HistoryOptions{Limit: 5}); return err }},
		{"LatestRevision", func() error { _, err := c.LatestRevision(ctx, "wikipedia:Foo", PageOptions{}); return err }},
	} {
		err := call.fn()
		assert.ErrorContains(t, err, `"wikipedia:Foo" is an interwiki title (wikipedia:Foo)`, call.name)
		assert.NotContains(t, err.Error(), "redirect", call.name)
	}

	for _, call := range []struct {
		name string
		fn   func() error
	}{
		{"Page", func() error { _, err := c.Page(ctx, "BrokenRedir", PageOptions{FollowRedirects: true}); return err }},
		{"PageInfo", func() error {
			_, err := c.PageInfo(ctx, "BrokenRedir", PageInfoOptions{FollowRedirects: true})
			return err
		}},
		{"History", func() error {
			_, err := c.History(ctx, "BrokenRedir", HistoryOptions{Limit: 5, FollowRedirects: true})
			return err
		}},
		{"LatestRevision", func() error {
			_, err := c.LatestRevision(ctx, "BrokenRedir", PageOptions{FollowRedirects: true})
			return err
		}},
	} {
		err := call.fn()
		assert.ErrorIs(t, err, ErrMissingPage, call.name)
		assert.ErrorContains(t, err, `redirect "BrokenRedir" points to missing page MissingTarget`, call.name)
	}
	_ = queries
}

func TestPagesTextFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		follow := r.Form.Get("redirects") == "1"
		resp := map[string]any{
			"query": map[string]any{
				"normalized": []any{map[string]string{"from": "alias1", "to": "Alias1"}},
				"interwiki":  []any{map[string]string{"title": "wikipedia:Foo", "iw": "wikipedia"}},
				"pages": []any{
					map[string]any{"title": "Target", "revisions": []any{map[string]any{"revid": 50, "slots": map[string]any{"main": map[string]string{"contentmodel": "wikitext", "content": "Target text"}}}}},
					map[string]any{"title": "BrokenTarget", "missing": true},
				},
			},
		}
		if follow {
			resp["query"].(map[string]any)["redirects"] = []any{
				map[string]string{"from": "Alias1", "to": "Alias2"},
				map[string]string{"from": "Alias2", "to": "Target"},
				map[string]string{"from": "Broken", "to": "BrokenTarget"},
				map[string]string{"from": "External", "to": "en:Main Page"},
			}
			resp["query"].(map[string]any)["interwiki"] = []any{
				map[string]string{"title": "en:Main Page", "iw": "en"},
				map[string]string{"title": "wikipedia:Foo", "iw": "wikipedia"},
			}
		} else {
			resp["query"].(map[string]any)["pages"] = []any{
				map[string]any{"title": "Alias1", "revisions": []any{map[string]any{"revid": 49, "slots": map[string]any{"main": map[string]string{"contentmodel": "wikitext", "content": "#REDIRECT [[Alias2]]"}}}}},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	ctx := context.Background()

	got, err := c.PagesText(ctx, []string{"alias1", "wikipedia:Foo"}, PageOptions{})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "#REDIRECT [[Alias2]]", got[0].Content)
	assert.False(t, got[0].Redirected)
	assert.Equal(t, "wikipedia:Foo", got[1].Interwiki)
	assert.False(t, got[1].Redirected)

	got, err = c.PagesText(ctx, []string{"alias1", "Broken", "External", "wikipedia:Foo"}, PageOptions{FollowRedirects: true})
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, "Target", got[0].Title)
	assert.Equal(t, "Target text", got[0].Content)
	assert.True(t, got[0].Redirected)
	assert.True(t, got[1].Missing)
	assert.Equal(t, "BrokenTarget", got[1].Title)
	assert.True(t, got[1].Redirected)
	assert.Equal(t, "en:Main Page", got[2].Interwiki)
	assert.True(t, got[2].Redirected)
	assert.Equal(t, "wikipedia:Foo", got[3].Interwiki)
	assert.False(t, got[3].Redirected)
}

func TestListPagesRedirects(t *testing.T) {
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seen = append(seen, r.Form)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Form.Get("prop") == "redirects":
			if r.Form.Get("titles") == "Bad<" {
				_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": []any{map[string]any{"title": "Bad<", "invalid": true}}}})
				return
			}
			if r.Form.Get("rdcontinue") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"query":    map[string]any{"pages": []any{map[string]any{"title": "Main Page", "redirects": []any{map[string]any{"pageid": 10, "ns": 0, "title": "R1"}, map[string]any{"pageid": 11, "ns": 0, "title": "R2"}}}}},
					"continue": map[string]string{"rdcontinue": "12", "continue": "||"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"pages": []any{map[string]any{"title": "Main Page", "redirects": []any{map[string]any{"pageid": 12, "ns": 0, "title": "R3"}}}}},
			})
		case r.Form.Get("list") == "backlinks" && r.Form.Get("blredirect") == "1":
			if r.Form.Get("blfilterredir") == "redirects" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"query": map[string]any{"backlinks": []any{
						map[string]any{"pageid": 1, "ns": 0, "title": "Direct"},
						map[string]any{
							"pageid": 2, "ns": 0, "title": "RedirA", "redirect": true,
							"redirlinks": []any{
								map[string]any{"pageid": 4, "ns": 0, "title": "DoubleRedir"},
							},
						},
					}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"backlinks": []any{
					map[string]any{"pageid": 1, "ns": 0, "title": "Direct"},
					map[string]any{
						"pageid": 2, "ns": 0, "title": "RedirA", "redirect": true,
						"redirlinks": []any{
							map[string]any{"pageid": 1, "ns": 0, "title": "Direct"},
							map[string]any{"pageid": 3, "ns": 0, "title": "ViaRedir"},
						},
					},
				}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{r.Form.Get("list"): []any{map[string]any{"pageid": 9, "ns": 0, "title": "Filtered"}}},
			})
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	ctx := context.Background()

	ns := 0
	redirs, err := c.ListPages(ctx, ListOptions{Source: RedirectsTo, Value: "Main Page", Namespace: &ns, Limit: 3})
	require.NoError(t, err)
	assert.Equal(t, []PageRef{
		{PageID: 10, NS: 0, Title: "R1"},
		{PageID: 11, NS: 0, Title: "R2"},
		{PageID: 12, NS: 0, Title: "R3"},
	}, redirs)
	assert.Equal(t, "0", seen[0].Get("rdnamespace"))
	assert.Equal(t, "1", seen[1].Get("rdlimit"))

	// FollowRedirects with LinksTo flattens redirlinks, deduplicates, and honors nonredirects on first-level redirects.
	seen = nil
	bl, err := c.ListPages(ctx, ListOptions{Source: LinksTo, Value: "Main Page", Limit: 300, FollowRedirects: true, FilterRedirects: "nonredirects"})
	require.NoError(t, err)
	assert.Equal(t, []PageRef{
		{PageID: 1, NS: 0, Title: "Direct"},
		{PageID: 3, NS: 0, Title: "ViaRedir"},
	}, bl)
	assert.Equal(t, "250", seen[0].Get("bllimit"))
	assert.Equal(t, "nonredirects", seen[0].Get("blfilterredir"))

	// FollowRedirects with redirects filter also filters out non-redirect first-level links.
	seen = nil
	blRedir, err := c.ListPages(ctx, ListOptions{Source: LinksTo, Value: "Main Page", Limit: 10, FollowRedirects: true, FilterRedirects: "redirects"})
	require.NoError(t, err)
	assert.Equal(t, []PageRef{
		{PageID: 2, NS: 0, Title: "RedirA"},
		{PageID: 4, NS: 0, Title: "DoubleRedir"},
	}, blRedir)
	assert.Equal(t, "redirects", seen[0].Get("blfilterredir"))

	// Without nonredirects filter, the first-level redirect itself is included too.
	blAll, err := c.ListPages(ctx, ListOptions{Source: LinksTo, Value: "Main Page", Limit: 10, FollowRedirects: true})
	require.NoError(t, err)
	assert.Equal(t, []PageRef{
		{PageID: 1, NS: 0, Title: "Direct"},
		{PageID: 2, NS: 0, Title: "RedirA"},
		{PageID: 3, NS: 0, Title: "ViaRedir"},
	}, blAll)

	// FilterRedirects passes through to allpages and embeddedin.
	seen = nil
	_, err = c.ListPages(ctx, ListOptions{Source: AllPages, Value: "A", Limit: 5, FilterRedirects: "redirects"})
	require.NoError(t, err)
	assert.Equal(t, "redirects", seen[0].Get("apfilterredir"))
	seen = nil
	_, err = c.ListPages(ctx, ListOptions{Source: EmbeddedIn, Value: "T", Limit: 5, FilterRedirects: "nonredirects"})
	require.NoError(t, err)
	assert.Equal(t, "nonredirects", seen[0].Get("eifilterredir"))

	// Invalid combinations return errors.
	_, err = c.ListPages(ctx, ListOptions{Source: AllPages, FilterRedirects: "bad"})
	assert.Error(t, err)
	_, err = c.ListPages(ctx, ListOptions{Source: CategoryMembers, Value: "Cat:A", FilterRedirects: "redirects"})
	assert.Error(t, err)
	_, err = c.ListPages(ctx, ListOptions{Source: AllPages, FollowRedirects: true})
	assert.Error(t, err)
	_, err = c.ListPages(ctx, ListOptions{Source: RedirectsTo, Value: "Bad<", Limit: 5})
	assert.Error(t, err)
	_, err = c.ListPages(ctx, ListOptions{Source: RedirectsTo, Value: "Foobar|Main Page", Limit: 5})
	assert.ErrorContains(t, err, `invalid title "Foobar|Main Page"`)
}
