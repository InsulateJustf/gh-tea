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

func init() {
	rootCmd.AddCommand(defaultCmd)
}

var defaultCmd = &cobra.Command{
	Use:   "default [<name>]",
	Short: "Show or set the default host",
	Long:  `gh tea default <name> sets the default; gh tea default shows it.`,
	Args:  cobra.MaximumNArgs(1),
	Run:   defaultRun,
}

func defaultRun(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		if cfg.Default == "" {
			fmt.Println("no default tea; set one with: gh tea default <name>")
			return
		}
		item, ok := cfg.Tea[cfg.Default]
		if !ok {
			log.Fatalf("default tea %q is missing from the config", cfg.Default)
		}
		fmt.Printf("default: %s (%s)\n", cfg.Default, item.Host)
		return
	}
	key := config.TeaName(args[0])
	if _, ok := cfg.Tea[key]; !ok {
		log.Fatalf("no such tea name: %s", args[0])
	}
	cfg.Default = key
	saveConfig()
	fmt.Printf("default tea set to %s\n", key)
}
