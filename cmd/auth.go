/*
Copyright © 2025 hiifong <f@ilo.nz>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/charmbracelet/log"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/InsulateJustf/gh-tea/pkg/config"
	"github.com/InsulateJustf/gh-tea/pkg/tea"
)

var (
	loginToken  string
	loginClient string
)

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(loginCmd, logoutCmd, refreshCmd, statusCmd, tokenCmd)

	loginCmd.Flags().StringVar(&loginToken, "token", "", "use a personal access token directly instead of OAuth")
	loginCmd.Flags().StringVar(&loginClient, "client-id", "", "OAuth2 application Client ID for this host (saved to the Tea)")
}

var (
	authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Authenticate gh-tea with Gitea hosts",
	}

	loginCmd = &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a Gitea host",
		Run:   loginRun,
	}

	logoutCmd = &cobra.Command{
		Use:   "logout",
		Short: "Remove authentication for a Gitea account",
		Run:   logoutRun,
	}

	refreshCmd = &cobra.Command{
		Use:   "refresh",
		Short: "Verify the stored credential for the selected Tea",
		Run:   refreshRun,
	}

	statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show login state for every configured Tea",
		Run:   statusRun,
	}

	tokenCmd = &cobra.Command{
		Use:   "token",
		Short: "Print the access token of the selected Tea",
		Run:   tokenRun,
	}
)

func loginRun(cmd *cobra.Command, args []string) {
	key, item := selectTea()

	if loginClient != "" {
		item.ClientID = loginClient
	}

	var accessToken string
	switch {
	case loginToken != "":
		accessToken = loginToken
	case item.ClientID != "":
		oconf := oauth2.Config{
			ClientID: item.ClientID,
			Endpoint: oauth2.Endpoint{
				AuthURL:  fmt.Sprintf(config.OAuthAuthURLTmpl, item.Host),
				TokenURL: fmt.Sprintf(config.OAuthTokenURLTmpl, item.Host),
			},
		}
		tok, err := getToken(cmd.Context(), oconf)
		if err != nil {
			log.Fatal(err)
		}
		accessToken = tok.AccessToken
	default:
		log.Fatalf(`no client-id configured for tea %q and no --token given.
Register an OAuth2 application on the host (redirect URI %s), then:
  gh tea new %s <host> --client-id <id>
or authenticate with a token:
  gh tea auth login --use %s --token <token>`, key, config.OAuthCallbackURL, key, key)
	}

	client, err := tea.Client(config.TeaItem{Host: item.Host, Token: accessToken})
	if err != nil {
		log.Fatal(err)
	}
	info, _, err := client.GetMyUserInfo()
	if err != nil {
		log.Fatalf("login failed (host %s): %v", item.Host, err)
	}

	item.Token = accessToken
	cfg.Tea[key] = item
	saveConfig()
	fmt.Printf("Successfully logged in: %s (%s)\n", info.UserName, item.Host)
}

func logoutRun(cmd *cobra.Command, args []string) {
	key, item := selectTea()
	if item.Token == "" {
		fmt.Printf("tea %s has no stored token\n", key)
		return
	}
	item.Token = ""
	cfg.Tea[key] = item
	saveConfig()
	fmt.Printf("Logged out of %s (host and client-id kept)\n", key)
}

func refreshRun(cmd *cobra.Command, args []string) {
	key, item := selectTea()
	if item.Token == "" {
		log.Fatalf("tea %s is not logged in; run: gh tea auth login --use %s", key, key)
	}
	info, err := currentUser(item)
	if err != nil {
		log.Fatalf("credential for %s is invalid: %v", key, err)
	}
	fmt.Printf("tea %s: logged in as %s, token valid\n", key, info.UserName)
}

func statusRun(cmd *cobra.Command, args []string) {
	for _, name := range cfg.SortedNames() {
		item := cfg.Tea[name]
		state := "-- not logged in"
		if item.Token != "" {
			if info, err := currentUser(item); err == nil {
				state = "✓ " + info.UserName
			} else {
				state = "✗ invalid token"
			}
		}
		marker := ""
		if name == cfg.Default {
			marker = " (default)"
		}
		fmt.Printf("%s\t%s\t%s%s\n", name, item.Host, state, marker)
	}
}

func tokenRun(cmd *cobra.Command, args []string) {
	key, item := selectTea()
	if item.Token == "" {
		log.Fatalf("tea %s is not logged in; run: gh tea auth login --use %s", key, key)
	}
	fmt.Println(item.Token)
}

func currentUser(item config.TeaItem) (*gitea.User, error) {
	client, err := tea.Client(item)
	if err != nil {
		return nil, err
	}
	info, _, err := client.GetMyUserInfo()
	return info, err
}

const successTemplate = `<!DOCTYPE html>
<html>
<head><title>gh-tea</title></head>
<body>
<p>Authentication complete. You can close this tab and return to the terminal.</p>
</body>
</html>`

// getToken runs the OAuth2 web flow against the fixed callback
// http://localhost:1455/callback (ADR 0002). The port is fixed, not
// auto-assigned, because Gitea matches redirect URIs exactly and the
// application must be registered with this URI beforehand.
func getToken(ctx context.Context, conf oauth2.Config) (*oauth2.Token, error) {
	callbackURL, err := url.Parse(config.OAuthCallbackURL)
	if err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", callbackURL.Host)
	if err != nil {
		return nil, fmt.Errorf("callback port %s unavailable (is another gh-tea login running?): %w", callbackURL.Host, err)
	}
	defer l.Close()

	queries := make(chan url.Values, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case queries <- r.URL.Query():
		default: // duplicate callback (refresh/prefetch); drop it
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, successTemplate)
	})}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(l) }()
	defer srv.Close()

	conf.RedirectURL = config.OAuthCallbackURL
	state := oauth2.GenerateVerifier()
	verifier := oauth2.GenerateVerifier()
	authCodeURL := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	fmt.Fprintf(os.Stderr, "Please complete authentication in your browser:\n%s\n\n", authCodeURL)
	if b := browser.New("", os.Stdout, os.Stderr); b.Browse(authCodeURL) != nil {
		fmt.Fprintf(os.Stderr, "(could not open a browser automatically; open the URL above manually)\n")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	select {
	case q := <-queries:
		if e := q.Get("error"); e != "" {
			return nil, fmt.Errorf("authorization failed: %s", e)
		}
		if q.Get("state") != state {
			return nil, errors.New("state mismatch")
		}
		return conf.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(verifier))
	case err := <-srvErr:
		return nil, fmt.Errorf("callback server stopped: %w", err)
	case <-ctx.Done():
		return nil, errors.New("login timed out after 5 minutes")
	}
}
