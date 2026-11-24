package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/mwcli/mwcli/api"
	"github.com/mwcli/mwcli/internal/config"
	"github.com/mwcli/mwcli/pkg/cmdutil"
)

const oauth2Callback = "http://localhost:8765/"

type oauthCallback struct {
	code string
	err  error
}

func oauth2WebLogin(ctx context.Context, f *cmdutil.Factory, t cmdutil.Target, clientID string) (*config.Account, string, error) {
	listener, err := net.Listen("tcp", "localhost:8765")
	if err != nil {
		return nil, "", fmt.Errorf("listen for OAuth callback at %s: %w", oauth2Callback, err)
	}
	defer listener.Close()

	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, "", fmt.Errorf("generate OAuth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier := oauth2.GenerateVerifier()
	endpoints, err := cmdutil.OAuth2Endpoint(t.API)
	if err != nil {
		return nil, "", err
	}
	conf := &oauth2.Config{
		ClientID:    clientID,
		RedirectURL: oauth2Callback,
		Endpoint:    endpoints,
	}
	callback := make(chan oauthCallback, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(w, "OAuth state did not match", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			detail := r.URL.Query().Get("error_description")
			http.Error(w, "Authorization was not granted. Return to mw for details.", http.StatusBadRequest)
			select {
			case callback <- oauthCallback{err: fmt.Errorf("wiki rejected OAuth authorization: %s %s", oauthErr, detail)}:
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "OAuth callback did not include a code", http.StatusBadRequest)
			select {
			case callback <- oauthCallback{err: errors.New("OAuth callback did not include a code")}:
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "Authorization complete. You may return to mw.")
		select {
		case callback <- oauthCallback{code: code}:
		default:
		}
	})
	server.Handler = mux
	go func() { _ = server.Serve(listener) }()
	defer server.Shutdown(context.Background())

	authURL := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	fmt.Fprintf(f.IOStreams.ErrOut, "Opening your browser to authorize mw on %s\n", t.Name)
	if err := f.Browse(authURL); err != nil {
		return nil, "", fmt.Errorf("open browser: %w", err)
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "Waiting for authorization at %s\n", oauth2Callback)
	select {
	case <-time.After(3 * time.Minute):
		return nil, "", errors.New("timed out waiting for OAuth authorization")
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case result := <-callback:
		if result.err != nil {
			return nil, "", result.err
		}
		return oauth2Exchange(ctx, conf, verifier, result.code, t)
	}
}

func oauth2Exchange(ctx context.Context, conf *oauth2.Config, verifier, code string, t cmdutil.Target) (*config.Account, string, error) {
	token, err := conf.Exchange(cmdutil.WithUserAgent(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", fmt.Errorf("exchange OAuth code: %w", err)
	}
	account := &config.Account{Auth: config.AuthOAuth2, OAuthToken: token.AccessToken, OAuthRefreshToken: token.RefreshToken, OAuthExpiry: token.Expiry}
	c, err := cmdutil.NewClient(t.API, account)
	if err != nil {
		return nil, "", err
	}
	u, err := c.User(ctx)
	if errors.Is(err, api.ErrNotLoggedIn) {
		return nil, "", fmt.Errorf("the OAuth token was not accepted by %s", t.Name)
	}
	if err != nil {
		return nil, "", fmt.Errorf("check OAuth token: %w", err)
	}
	return account, u.Name, nil
}
