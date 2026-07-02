package tokenauth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

type Config struct {
	Bearer       string `mapstructure:"bearer"`
	OAuthURL     string `mapstructure:"oauth-url"`
	Realm        string `mapstructure:"realm"`
	ClientID     string `mapstructure:"client-id"`
	ClientSecret string `mapstructure:"client-secret"`
	Username     string `mapstructure:"oauth-username"`
	Password     string `mapstructure:"oauth-password"`
	CallbackPort int    `mapstructure:"oauth-callback-port"`
	Insecure     bool   `mapstructure:"insecure"`
}

func DefaultConfig() Config {
	return Config{
		OAuthURL:     "https://pki.localhost/kc",
		Realm:        "otpki",
		ClientID:     "otpki-service",
		ClientSecret: "dev-service-secret-change-me",
		CallbackPort: 8085,
		Insecure:     true,
	}
}

// Describes the configured token mode, or "" when none is configured
func (cfg Config) Describe() string {
	switch {
	case cfg.Bearer != "":
		return "static bearer token"
	case cfg.OAuthURL != "" && cfg.Username != "":
		return fmt.Sprintf("OIDC password grant (%s, realm=%s, client=%s, user=%s)", cfg.OAuthURL, cfg.Realm, cfg.ClientID, cfg.Username)
	case cfg.OAuthURL != "":
		return fmt.Sprintf("OIDC browser login (%s, realm=%s, client=%s)", cfg.OAuthURL, cfg.Realm, cfg.ClientID)
	}
	return ""
}

// Builds token source for auth mode, or nil when no mode configured
func BuildTokenSource(ctx context.Context, cfg Config) (oauth2.TokenSource, error) {
	if cfg.Bearer != "" {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Bearer}), nil
	}
	if cfg.OAuthURL == "" {
		return nil, nil
	}

	oauthHTTP := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure}, //nolint:gosec
		},
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, oauthHTTP)

	oidcBase := strings.TrimRight(cfg.OAuthURL, "/") + "/realms/" + cfg.Realm + "/protocol/openid-connect"
	conf := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: oidcBase + "/auth", TokenURL: oidcBase + "/token"},
		Scopes:       []string{"openid"},
		RedirectURL:  fmt.Sprintf("http://localhost:%d/Callback", cfg.CallbackPort),
	}

	var tok *oauth2.Token
	var err error
	if cfg.Username != "" {
		tok, err = conf.PasswordCredentialsToken(ctx, cfg.Username, cfg.Password)
	} else {
		tok, err = browserAuthCode(ctx, conf)
	}
	if err != nil {
		return nil, fmt.Errorf("oidc login: %w", err)
	}
	return conf.TokenSource(ctx, tok), nil
}

// Runs the auth-code flow with PKCE
func browserAuthCode(ctx context.Context, conf *oauth2.Config) (*oauth2.Token, error) {
	redirect, err := url.Parse(conf.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect url %q: %w", conf.RedirectURL, err)
	}

	verifier := oauth2.GenerateVerifier()
	state := uuid.NewString()
	authURL := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	path := redirect.Path
	if path == "" {
		path = "/"
	}
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "login failed: "+e, http.StatusBadRequest)
			errCh <- fmt.Errorf("oidc error: %s %s", e, q.Get("error_description"))
			return
		}
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("oidc state mismatch")
			return
		}

		// Write the browser auth ok page
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h3>Authentication received.</h3>You can close this tab and return to the terminal.</body></html>"))
		codeCh <- q.Get("code")
	})

	ln, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return nil, fmt.Errorf("listen on %s for oauth callback: %w", redirect.Host, err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	openCmd, openArgs := "xdg-open", []string(nil)
	switch runtime.GOOS {
	case "darwin":
		openCmd = "open"
	case "windows":
		openCmd, openArgs = "rundll32", []string{"url.dll,FileProtocolHandler"}
	}
	_ = exec.Command(openCmd, append(openArgs, authURL)...).Start()

	select {
	case code := <-codeCh:
		tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("exchanging auth code: %w", err)
		}
		return tok, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("timed out waiting for browser login")
	}
}
