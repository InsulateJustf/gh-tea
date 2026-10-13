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
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/InsulateJustf/gh-tea/pkg/config"
)

var newClientID string

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().StringVar(&newClientID, "client-id", "", "OAuth2 application Client ID registered on this host (see README)")
}

var newCmd = &cobra.Command{
	Use:   "new <name> <host>",
	Short: "Register a Gitea host under a name",
	Long:  `gh tea new <name> <host>, like git remote add <name> <url>.`,
	Args:  cobra.ExactArgs(2),
	Run:   newRun,
}

func newRun(cmd *cobra.Command, args []string) {
	name, host := args[0], args[1]
	normalized, err := config.NormalizeHost(host)
	if err != nil {
		log.Fatal(err)
	}
	key := config.TeaName(name)
	if _, exists := cfg.Tea[key]; exists {
		log.Fatalf("tea name %q already exists (remove it first with: gh tea remove %s)", name, name)
	}
	cfg.Tea[key] = config.TeaItem{
		Name:     name,
		Host:     normalized,
		ClientID: newClientID,
	}
	markDefault(key)
	saveConfig()
	fmt.Printf("%s -> %s\n", name, normalized)
}
