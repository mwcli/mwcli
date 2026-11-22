package api

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilesInfoStopsAtIIStart(t *testing.T) {
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"continue": map[string]string{
				"iistart":  "2005-01-01T00:00:00Z",
				"continue": "||",
			},
			"query": map[string]any{
				"pages": []any{
					map[string]any{
						"title":           "File:Example.jpg",
						"imagerepository": "local",
						"imageinfo": []any{
							map[string]any{
								"timestamp": "2024-03-01T09:11:38Z",
								"url":       srv.URL + "/Example.jpg",
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	infos, err := c.FilesInfo(context.Background(), []string{"File:Example.jpg"})
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, 1, calls)
	assert.Equal(t, "2024-03-01T09:11:38Z", infos[0].Timestamp)
	assert.Equal(t, srv.URL+"/Example.jpg", infos[0].URL)
}

func TestFilesInfoFollowsIIContinue(t *testing.T) {
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("iicontinue") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue": map[string]string{
					"iicontinue": "File:B.png|20260103000000",
					"continue":   "||",
				},
				"query": map[string]any{
					"pages": []any{
						map[string]any{
							"title":           "File:A.png",
							"imagerepository": "local",
							"imageinfo": []any{
								map[string]any{"timestamp": "2026-01-02T00:00:00Z", "url": srv.URL + "/A.png"},
							},
						},
						map[string]any{
							"title":           "File:B.png",
							"imagerepository": "local",
						},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"continue": map[string]string{
				"iistart":  "2019-01-01T00:00:00Z",
				"continue": "||",
			},
			"query": map[string]any{
				"pages": []any{
					map[string]any{
						"title":           "File:A.png",
						"imagerepository": "local",
						"imageinfo": []any{
							map[string]any{"timestamp": "2020-01-01T00:00:00Z", "url": srv.URL + "/old-A.png"},
						},
					},
					map[string]any{
						"title":           "File:B.png",
						"imagerepository": "local",
						"imageinfo": []any{
							map[string]any{"timestamp": "2026-01-03T00:00:00Z", "url": srv.URL + "/B.png"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	infos, err := c.FilesInfo(context.Background(), []string{"File:A.png", "File:B.png"})
	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "2026-01-02T00:00:00Z", infos[0].Timestamp)
	assert.Equal(t, srv.URL+"/A.png", infos[0].URL)
	assert.Equal(t, "2026-01-03T00:00:00Z", infos[1].Timestamp)
	assert.Equal(t, srv.URL+"/B.png", infos[1].URL)
}

func TestFilesInfoSharedRepoAndURLNormalization(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": map[string]any{
				"normalized": []any{
					map[string]string{"from": "File:Example.jpg", "to": "파일:Example.jpg"},
				},
				"pages": []any{
					map[string]any{
						"title":           "파일:Example.jpg",
						"missing":         true,
						"known":           true,
						"imagerepository": "shared",
						"imageinfo": []any{
							map[string]any{
								"timestamp":      "2010-03-14T17:20:20Z",
								"url":            "/w/images/Example.jpg?v=2&utm_source=wiki&utm_campaign=imageinfo",
								"descriptionurl": "//commons.example/wiki/File:Example.jpg",
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	infos, err := c.FilesInfo(context.Background(), []string{"File:Example.jpg"})
	require.NoError(t, err)
	require.Len(t, infos, 1)
	fi := infos[0]
	assert.False(t, fi.Missing)
	assert.Equal(t, "File:Example.jpg", fi.Asked)
	assert.Equal(t, "파일:Example.jpg", fi.Title)
	assert.Equal(t, "shared", fi.Repository)
	assert.Equal(t, srv.URL+"/w/images/Example.jpg?v=2", fi.URL)
	assert.Equal(t, "http://commons.example/wiki/File:Example.jpg", fi.DescriptionURL)
}

func TestFetchCredentialsHostScoped(t *testing.T) {
	var apiAuth, apiUA string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiAuth = r.Header.Get("Authorization")
		apiUA = r.Header.Get("User-Agent")
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("same-origin bytes"))
	}))
	defer apiSrv.Close()

	var cdnAuth, cdnUA string
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnAuth = r.Header.Get("Authorization")
		cdnUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("cdn bytes"))
	}))
	defer cdnSrv.Close()

	c, err := New(apiSrv.URL+"/w/api.php", WithOAuth2Token("secret"), WithUserAgent("test-ua"))
	require.NoError(t, err)

	var buf bytes.Buffer
	sum, err := c.Fetch(context.Background(), apiSrv.URL+"/private/a.bin", &buf)
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret", apiAuth)
	assert.Equal(t, "test-ua", apiUA)
	assert.Equal(t, "same-origin bytes", buf.String())
	h1 := sha1.Sum([]byte("same-origin bytes"))
	assert.Equal(t, hex.EncodeToString(h1[:]), sum)

	buf.Reset()
	sum, err = c.Fetch(context.Background(), cdnSrv.URL+"/public/b.bin", &buf)
	require.NoError(t, err)
	assert.Empty(t, cdnAuth)
	assert.Equal(t, "test-ua", cdnUA)
	assert.Equal(t, "cdn bytes", buf.String())
	h2 := sha1.Sum([]byte("cdn bytes"))
	assert.Equal(t, hex.EncodeToString(h2[:]), sum)

	buf.Reset()
	err = func() error {
		_, err := c.Fetch(context.Background(), apiSrv.URL+"/missing", &buf)
		return err
	}()
	var he *HTTPError
	require.ErrorAs(t, err, &he)
	assert.Equal(t, "404 Not Found", he.Status)
}
