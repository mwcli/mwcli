package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/pkg/cmdutil"
)

type oauth1Reply struct {
	Token  string `json:"token"`
	Secret string `json:"key"`
}

func oauth1WebLogin(ctx context.Context, f *cmdutil.Factory, t cmdutil.Target, consumerKey, consumerSecret string) (*config.Account, string, error) {
	consumerKey = strings.TrimSpace(consumerKey)
	consumerSecret = strings.TrimSpace(consumerSecret)
	if consumerKey == "" || consumerSecret == "" {
		return nil, "", cmdutil.FlagErrorf("no OAuth 1.0a consumer credentials given")
	}
	base := &api.OAuth1{ConsumerKey: consumerKey, ConsumerSecret: consumerSecret}
	requestTokenURL, err := oauth1URL(t.PageURL("Special:OAuth/initiate"), url.Values{
		"format":         {"json"},
		"oauth_callback": {"oob"},
	})
	if err != nil {
		return nil, "", err
	}
	requestURL, _ := url.Parse(requestTokenURL)
	if requestURL.Scheme != "https" {
		return nil, "", errors.New("OAuth 1.0a browser login requires an HTTPS wiki")
	}
	requestToken, err := oauth1Request(ctx, http.MethodGet, requestTokenURL, base)
	if err != nil {
		return nil, "", fmt.Errorf("request OAuth authorization: %w", err)
	}
	if requestToken.Token == "" || requestToken.Secret == "" {
		return nil, "", errors.New("wiki returned an incomplete OAuth request token")
	}

	authorizeURL, err := oauth1URL(t.PageURL("Special:OAuth/authorize"), url.Values{"oauth_token": {requestToken.Token}})
	if err != nil {
		return nil, "", err
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "Opening your browser to authorize mw on %s\n", t.Name)
	if err := f.Browse(authorizeURL); err != nil {
		return nil, "", fmt.Errorf("open browser: %w", err)
	}
	verifier, err := f.IOStreams.PromptContext(ctx, "? Authorization code from the wiki: ")
	if err != nil {
		return nil, "", err
	}
	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		return nil, "", cmdutil.FlagErrorf("no OAuth authorization code given")
	}

	accessURL, err := oauth1URL(t.PageURL("Special:OAuth/token"), url.Values{
		"format":         {"json"},
		"oauth_verifier": {verifier},
	})
	if err != nil {
		return nil, "", err
	}
	requestAuth := &api.OAuth1{ConsumerKey: consumerKey, ConsumerSecret: consumerSecret, AccessToken: requestToken.Token, AccessSecret: requestToken.Secret}
	access, err := oauth1Request(ctx, http.MethodGet, accessURL, requestAuth)
	if err != nil {
		return nil, "", fmt.Errorf("exchange OAuth authorization: %w", err)
	}
	if access.Token == "" || access.Secret == "" {
		return nil, "", errors.New("wiki returned incomplete OAuth access credentials")
	}
	account := &config.Account{Auth: config.AuthOAuth1, ConsumerKey: consumerKey, ConsumerSecret: consumerSecret, AccessToken: access.Token, AccessSecret: access.Secret}
	c, err := cmdutil.NewClient(t.API, account)
	if err != nil {
		return nil, "", err
	}
	u, err := c.User(ctx)
	if errors.Is(err, api.ErrNotLoggedIn) {
		return nil, "", fmt.Errorf("the OAuth 1.0a credentials were not accepted by %s", t.Name)
	}
	if err != nil {
		if hint := oauth1Hint(err); hint != "" {
			return nil, "", fmt.Errorf("check OAuth credentials: %w\n%s", err, hint)
		}
		return nil, "", fmt.Errorf("check OAuth credentials: %w", err)
	}
	return account, u.Name, nil
}

func oauth1URL(raw string, params url.Values) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("build OAuth URL: %w", err)
	}
	q := u.Query()
	for key, values := range params {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func oauth1Request(ctx context.Context, method, rawURL string, auth *api.OAuth1) (*oauth1Reply, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	auth.Sign(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wiki returned HTTP %s", resp.Status)
	}
	var reply oauth1Reply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, fmt.Errorf("decode wiki response: %w", err)
	}
	return &reply, nil
}
