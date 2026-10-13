// Package tea resolves which Gitea host and repository a command operates on
// (SPEC §3) and builds SDK clients for them.
package tea

import (
	"code.gitea.io/sdk/gitea"

	"github.com/InsulateJustf/gh-tea/pkg/config"
)

// Client returns a Gitea SDK client authenticated with the Tea's token.
func Client(item config.TeaItem) (*gitea.Client, error) {
	return gitea.NewClient(item.Host, gitea.SetToken(item.Token))
}
