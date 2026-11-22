package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lagServer refuses the first refusals requests for lag, then answers.
func lagServer(t *testing.T, refusals int) (*httptest.Server, *[]url.Values) {
	var got []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = append(got, r.Form)
		w.Header().Set("Content-Type", "application/json")
		if len(got) <= refusals {
			w.Header().Set("Retry-After", "2")
			w.Header().Set("X-Database-Lag", "7")
			_, _ = w.Write([]byte(`{"error":{"code":"maxlag","info":"Waiting for 10.0.0.1: 7 seconds lagged."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"query":{"userinfo":{"id":7,"name":"Alice"}},"edit":{"result":"Success","title":"A"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestMaxlagRetries(t *testing.T) {
	srv, got := lagServer(t, 2)
	var waits []time.Duration
	var lags []string
	c, err := New(srv.URL+"/w/api.php", OnMaxlag(func(d time.Duration, lag string) { lags = append(lags, lag) }))
	require.NoError(t, err)
	c.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	u, err := c.User(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Alice", u.Name)
	assert.Equal(t, []time.Duration{2 * time.Second, 2 * time.Second}, waits)
	assert.Equal(t, []string{"7", "7"}, lags)
	require.Len(t, *got, 3)
	for _, f := range *got {
		assert.Equal(t, "5", f.Get("maxlag"))
	}
}

// An edit refused for lag was not done, so it is sent again.
func TestMaxlagRetriesPost(t *testing.T) {
	srv, got := lagServer(t, 1)
	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	c.sleep = func(context.Context, time.Duration) error { return nil }
	body, err := c.Raw(context.Background(), http.MethodPost, url.Values{"action": {"edit"}, "format": {"json"}, "title": {"A"}, "text": {"x"}, "token": {"t"}})
	require.NoError(t, err)
	assert.NoError(t, CheckError(body))
	require.Len(t, *got, 2)
	assert.Equal(t, "x", (*got)[1].Get("text"))
}

func TestMaxlagGivesUp(t *testing.T) {
	srv, got := lagServer(t, 100)
	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	c.sleep = func(context.Context, time.Duration) error { return nil }
	_, err = c.User(context.Background())
	var ae *APIError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "maxlag", ae.Code)
	assert.Len(t, *got, 1+maxlagRetries)
}

func TestMaxlagOptions(t *testing.T) {
	srv, got := lagServer(t, 0)
	c, err := New(srv.URL+"/w/api.php", WithMaxlag(0))
	require.NoError(t, err)
	_, err = c.User(context.Background())
	require.NoError(t, err)
	assert.False(t, (*got)[0].Has("maxlag"))

	c, err = New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	_, err = c.Raw(context.Background(), http.MethodGet, url.Values{"action": {"query"}, "maxlag": {"1"}})
	require.NoError(t, err)
	assert.Equal(t, "1", (*got)[1].Get("maxlag"), "a request's own maxlag stays")
}

// A wait for lag ends when the context does.
func TestMaxlagWaitCancels(t *testing.T) {
	srv, _ := lagServer(t, 100)
	c, err := New(srv.URL + "/w/api.php")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.User(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second)
}

func TestRetryAfter(t *testing.T) {
	assert.Equal(t, 3*time.Second, retryAfter(http.Header{"Retry-After": {"3"}}))
	assert.Equal(t, 5*time.Second, retryAfter(http.Header{}))
	assert.Equal(t, 5*time.Second, retryAfter(http.Header{"Retry-After": {"soon"}}))
}
