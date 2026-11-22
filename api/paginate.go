package api

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
)

// Paginate yields the response to a request and to each request that follows
// its continue values, until a response has none. Each request is params as
// they are then, so the loop may change one such as a limit, with the continue
// values of the last response, as MediaWiki asks: values a later response
// drops are not sent again. format=json and formatversion=2 are added unless
// given.
//
// A response that reports an API error is yielded with that error, and the
// iteration stops; an HTTP or network error is yielded with a nil body.
func (c *Client) Paginate(ctx context.Context, method string, params url.Values) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		if !params.Has("format") {
			params.Set("format", "json")
		}
		if !params.Has("formatversion") && params.Get("format") == "json" {
			params.Set("formatversion", "2")
		}
		cont := map[string]string{}
		for {
			p := url.Values{}
			for k, v := range params {
				p[k] = v
			}
			for k, v := range cont {
				p.Set(k, v)
			}
			body, err := c.Raw(ctx, method, p)
			if err != nil {
				yield(nil, err)
				return
			}
			if err := CheckError(body); err != nil {
				yield(body, err)
				return
			}
			next, err := continueValues(body)
			if err != nil {
				yield(body, err)
				return
			}
			if !yield(body, nil) || next == nil {
				return
			}
			cont = next
		}
	}
}

// continueValues returns the continue values of a response, or nil when the
// result is complete. Values are strings or, like sroffset, numbers.
func continueValues(body []byte) (map[string]string, error) {
	var resp struct {
		Continue map[string]json.RawMessage `json:"continue"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode API response: %w", err)
	}
	if len(resp.Continue) == 0 {
		return nil, nil
	}
	next := map[string]string{}
	for k, raw := range resp.Continue {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = string(raw)
		}
		next[k] = s
	}
	return next, nil
}
