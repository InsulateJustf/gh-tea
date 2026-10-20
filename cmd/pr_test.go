package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func resetPRCLIState(t *testing.T) {
	t.Helper()
	reset := func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		for _, command := range []*cobra.Command{rootCmd, prCmd, prListCmd, prViewCmd} {
			command.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
		}
		rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
		configFlag, useFlag, repoFlag = "", "", ""
		debugFlag = false
		prListState, prListAuthor, prListLabels, prListLimit, prListJSON, prViewJSON = "open", "", nil, 30, "", ""
	}
	reset()
	t.Cleanup(reset)
}

func TestPRListFiltersBeforeLimitAcrossPagesAndProjectsSelectedFields(t *testing.T) {
	resetPRCLIState(t)
	var pages []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.URL.Path != "/api/v1/repos/alice/project/pulls" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "token synthetic-token" {
			t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		pages = append(pages, r.URL.Query().Get("page"))
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/pulls?page=2&limit=50>; rel=\"next\"")
			fmt.Fprint(w, `[{"number":1,"title":"closed","state":"closed","merged":false,"draft":false},{"number":2,"title":"merged","state":"closed","merged":true,"draft":true}]`)
			return
		}
		fmt.Fprint(w, `[{"number":3,"title":"chosen","state":"open","merged":false,"draft":false,"html_url":"https://tea.example/pr/3","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z","user":{"login":"alice"},"labels":[{"name":"bug"},{"name":"urgent"}],"base":{"ref":"main"},"head":{"ref":"fix"}}]`)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--config", configPath, "pr", "list", "--repo", "alice/project", "--state", "open", "--limit", "1", "--author", "alice", "--label", "bug,urgent", "--json", "number,title,state,isDraft,url,createdAt,updatedAt,author.login,labels[].name,baseRefName,headRefName"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pr list: %v (stderr: %s)", err, stderr.String())
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("output is not JSON: %v: %s", err, stdout.String())
	}
	if len(got) != 1 || got[0]["title"] != "chosen" || got[0]["state"] != "open" || got[0]["isDraft"] != false {
		t.Fatalf("projected output = %#v", got)
	}
	if got[0]["author"].(map[string]any)["login"] != "alice" || got[0]["baseRefName"] != "main" || got[0]["headRefName"] != "fix" {
		t.Fatalf("nested fields were not projected: %#v", got[0])
	}
	if len(pages) != 2 {
		t.Fatalf("fetched pages = %v, want both pages before applying limit", pages)
	}
}

func TestPRListRejectsUnknownAndViewOnlyJSONFields(t *testing.T) {
	for _, field := range []string{"files", "comments", "reviews", "reviewComments", "unknown"} {
		t.Run(field, func(t *testing.T) {
			resetPRCLIState(t)
			rootCmd.SetArgs([]string{"pr", "list", "--repo", "alice/project", "--json", field})
			err := rootCmd.Execute()
			if err == nil || !strings.Contains(err.Error(), `unknown JSON field "`+field+`"`) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
