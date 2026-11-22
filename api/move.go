package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// MoveOptions are the choices of Move, named as in the API.
type MoveOptions struct {
	Reason string
	// MoveTalk moves the talk page too, MoveSubpages the subpages.
	MoveTalk, MoveSubpages bool
	// NoRedirect leaves no redirect at the old title; it needs the
	// suppressredirect right.
	NoRedirect bool
}

// MoveResult is the outcome of a move.
type MoveResult struct {
	From, To string
	// RedirectCreated is true when a redirect was left at the old title.
	RedirectCreated bool
	// TalkFrom and TalkTo are set when the talk page moved.
	TalkFrom, TalkTo string
	// Subpages are the subpages that moved, old title to new.
	Subpages [][2]string
}

// ErrSelfMove means a page was to be moved to its own title.
var ErrSelfMove = errors.New("the new title is the same as the old one")

// Move renames a page. It fails with ErrMissingPage when the page does not
// exist and with ErrPageExists when the new title is taken.
func (c *Client) Move(ctx context.Context, from, to string, opts MoveOptions) (*MoveResult, error) {
	token, err := c.TokenOf(ctx, "csrf")
	if err != nil {
		return nil, err
	}
	form := url.Values{"action": {"move"}, "assert": {"user"}, "from": {from}, "to": {to}, "reason": {opts.Reason}, "token": {token}}
	for k, on := range map[string]bool{"movetalk": opts.MoveTalk, "movesubpages": opts.MoveSubpages, "noredirect": opts.NoRedirect} {
		if on {
			form.Set(k, "1")
		}
	}
	var result struct {
		Move struct {
			From            string `json:"from"`
			To              string `json:"to"`
			RedirectCreated bool   `json:"redirectcreated"`
			TalkFrom        string `json:"talkfrom"`
			TalkTo          string `json:"talkto"`
			Subpages        []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"subpages"`
		} `json:"move"`
	}
	err = c.request(ctx, http.MethodPost, form, &result)
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case "missingtitle":
			return nil, fmt.Errorf("%w: %s", ErrMissingPage, from)
		case "articleexists":
			return nil, fmt.Errorf("%w: %s", ErrPageExists, to)
		case "selfmove":
			return nil, fmt.Errorf("%w: %s", ErrSelfMove, from)
		}
	}
	if err != nil {
		return nil, err
	}
	m := result.Move
	res := &MoveResult{From: m.From, To: m.To, RedirectCreated: m.RedirectCreated, TalkFrom: m.TalkFrom, TalkTo: m.TalkTo}
	for _, s := range m.Subpages {
		if s.From != "" && s.To != "" {
			res.Subpages = append(res.Subpages, [2]string{s.From, s.To})
		}
	}
	return res, nil
}

// Delete deletes a page with all its revisions, failing with ErrMissingPage
// when it does not exist. It returns the title the wiki deleted.
func (c *Client) Delete(ctx context.Context, title, reason string) (string, error) {
	token, err := c.TokenOf(ctx, "csrf")
	if err != nil {
		return "", err
	}
	form := url.Values{"action": {"delete"}, "assert": {"user"}, "title": {title}, "reason": {reason}, "token": {token}}
	var result struct {
		Delete struct {
			Title string `json:"title"`
		} `json:"delete"`
	}
	err = c.request(ctx, http.MethodPost, form, &result)
	var ae *APIError
	if errors.As(err, &ae) && ae.Code == "missingtitle" {
		return "", fmt.Errorf("%w: %s", ErrMissingPage, title)
	}
	if err != nil {
		return "", err
	}
	return result.Delete.Title, nil
}
