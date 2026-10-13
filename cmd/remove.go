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
	rootCmd.AddCommand(removeCmd)
}

var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a host from the list",
	Args:  cobra.ExactArgs(1),
	Run:   removeRun,
}

func removeRun(cmd *cobra.Command, args []string) {
	key := config.TeaName(args[0])
	if _, ok := cfg.Tea[key]; !ok {
		log.Fatalf("no such tea name: %s", args[0])
	}
	delete(cfg.Tea, key)
	if cfg.Default == key {
		cfg.Default = ""
		fmt.Println("default tea cleared; set one with: gh tea default <name>")
	}
	saveConfig()
	fmt.Printf("removed %s\n", args[0])
}
