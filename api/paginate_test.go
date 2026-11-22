package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// continueServer answers three responses: the first two with continue values
// that change keys, the third complete; with fail=2, the second is an error.
func continueServer(t *testing.T, fail int) (*httptest.Server, *[]url.Values) {
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = append(got, r.Form)
		w.Header().Set("Content-Type", "application/json")
		n := len(got)
		if n == fail {
			_, _ = w.Write([]byte(`{"error":{"code":"badcontinue","info":"bad"}}`))
			return
		}
		resp := map[string]any{"query": map[string]any{"n": n}}
		switch n {
		case 1:
			resp["continue"] = map[string]any{"a": "1", "continue": "-||"}
		case 2:
			resp["continue"] = map[string]any{"b": 2, "continue": "-||"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestPaginate(t *testing.T) {
	srv, got := continueServer(t, 0)
	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	params := url.Values{"action": {"query"}, "list": {"x"}, "xlimit": {"10"}}
	var ns []int
	for body, err := range c.Paginate(context.Background(), http.MethodGet, params) {
		require.NoError(t, err)
		var r struct {
			Query struct{ N int } `json:"query"`
		}
		require.NoError(t, json.Unmarshal(body, &r))
		ns = append(ns, r.Query.N)
		// The loop may change a parameter for the next request.
		params.Set("xlimit", "3")
	}
	assert.Equal(t, []int{1, 2, 3}, ns)
	require.Len(t, *got, 3)
	assert.Equal(t, "json", (*got)[0].Get("format"))
	assert.Equal(t, "2", (*got)[0].Get("formatversion"))
	assert.Equal(t, "10", (*got)[0].Get("xlimit"))
	assert.Equal(t, "3", (*got)[1].Get("xlimit"))
	assert.Equal(t, "1", (*got)[1].Get("a"))
	// A value a later response drops is not sent again; numbers stay as sent.
	assert.False(t, (*got)[2].Has("a"))
	assert.Equal(t, "2", (*got)[2].Get("b"))
	assert.False(t, params.Has("a"), "continue values do not change the caller's params")
}

func TestPaginateStops(t *testing.T) {
	srv, got := continueServer(t, 0)
	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	for range c.Paginate(context.Background(), http.MethodGet, url.Values{"action": {"query"}}) {
		break
	}
	assert.Len(t, *got, 1, "breaking the loop sends no more requests")
}

func TestPaginateAPIError(t *testing.T) {
	srv, got := continueServer(t, 2)
	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	var bodies [][]byte
	var errs []error
	for body, err := range c.Paginate(context.Background(), http.MethodGet, url.Values{"action": {"query"}}) {
		bodies, errs = append(bodies, body), append(errs, err)
	}
	require.Len(t, errs, 2)
	assert.NoError(t, errs[0])
	var ae *APIError
	require.ErrorAs(t, errs[1], &ae)
	assert.Equal(t, "badcontinue", ae.Code)
	assert.Contains(t, string(bodies[1]), "badcontinue", "the error response is yielded too")
	assert.Len(t, *got, 2)
}
