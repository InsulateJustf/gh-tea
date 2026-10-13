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
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/InsulateJustf/gh-tea/pkg/config"
)

var (
	cfg        config.Config
	useFlag    string
	configFlag string

	rootCmd = &cobra.Command{
		Use:   "tea",
		Short: "Use gh commands against Gitea instances",
		Long: `gh tea = gh for Gitea.

Command names and flag names follow gh; the semantics are a practical subset
(the gh-compat surface). Run "gh tea auth login" against a Gitea host first,
then use "gh tea issue ..." and "gh tea repo ..." like their gh counterparts.`,
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFlag, "config", "c", "", "config file (default "+config.DefaultPath+")")
	rootCmd.PersistentFlags().StringVar(&useFlag, "use", "", "use the Tea <name> for this invocation")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false, "debug logging")
	cobra.OnInitialize(initConfig, initLog)
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

var debugFlag bool

func initLog() {
	if debugFlag {
		log.SetLevel(log.DebugLevel)
	}
}

func initConfig() {
	path := configPath()
	cfg2, err := config.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	cfg = cfg2
}

// configPath expands the default "~" path or honors --config.
func configPath() string {
	p := configFlag
	if p == "" {
		p = config.DefaultPath
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		p = filepath.Join(home, p[1:])
	}
	return p
}

// saveConfig persists cfg to the active config file.
func saveConfig() {
	if err := config.Save(configPath(), cfg); err != nil {
		log.Fatal(err)
	}
}

// selectTea picks the Tea for this invocation: explicit --use wins, then the
// configured default. A miss is always a hard error, never a silent fallback.
func selectTea() (config.TeaName, config.TeaItem) {
	if useFlag != "" {
		item, ok := cfg.Tea[config.TeaName(useFlag)]
		if !ok {
			log.Fatalf("no such tea name: %s", useFlag)
		}
		return config.TeaName(useFlag), item
	}
	if cfg.Default == "" {
		log.Fatal("no tea configured; run: gh tea new <name> <host>")
	}
	item, ok := cfg.Tea[cfg.Default]
	if !ok {
		log.Fatalf("default tea %q is missing from the config", cfg.Default)
	}
	return cfg.Default, item
}

// markDefault marks the first registered Tea as default if none is set yet.
func markDefault(name config.TeaName) {
	if cfg.Default == "" {
		cfg.Default = name
		fmt.Printf("default tea set to %s\n", name)
	}
}
