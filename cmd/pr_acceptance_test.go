package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func prTestConfig(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil { t.Fatal(err) }
	return path
}

func TestPRListResolvesMeAndFiltersAllLabelsAcrossPages(t *testing.T) {
	resetPRCLIState(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version": fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/user": fmt.Fprint(w, `{"login":"me"}`)
		case "/api/v1/repos/alice/project/pulls":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/pulls?page=2&limit=50>; rel=\"next\"")
				fmt.Fprint(w, `[{"number":1,"title":"other author","state":"open","draft":false,"user":{"login":"other"},"labels":[{"name":"bug"},{"name":"urgent"}]},{"number":2,"title":"one label","state":"open","user":{"login":"me"},"labels":[{"name":"bug"}]}]`)
			} else {
				fmt.Fprint(w, `[{"number":3,"title":"both labels","state":"open","draft":true,"user":{"login":"me"},"labels":[{"name":"bug"},{"name":"urgent"}]}]`)
			}
		default: http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--state", "all", "--limit", "1", "--author", "@me", "--label", "bug,urgent", "--json", "number,title,isDraft"})
	if err := rootCmd.Execute(); err != nil { t.Fatal(err) }
	if got, want := strings.TrimSpace(stdout.String()), "[\n  {\n    \"isDraft\": true,\n    \"number\": 3,\n    \"title\": \"both labels\"\n  }\n]"; got != want { t.Fatalf("JSON = %s, want %s", got, want) }
}

func TestPRListClosedStateExcludesMergedInServerOrder(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" { fmt.Fprint(w, `{"version":"1.22.0"}`); return }
		fmt.Fprint(w, `[{"number":8,"title":"merged","state":"closed","merged":true},{"number":9,"title":"closed","state":"closed","merged":false}]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--state", "closed", "--limit", "1", "--json", "number,title,state"})
	if err := rootCmd.Execute(); err != nil { t.Fatal(err) }
	if !strings.Contains(stdout.String(), `"number": 9`) || strings.Contains(stdout.String(), `"number": 8`) { t.Fatalf("closed result = %s", stdout.String()) }
}

func TestPRListPreservesMaxInt64AndProjectsMissingDataAsNull(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" { fmt.Fprint(w, `{"version":"1.22.0"}`); return }
		fmt.Fprint(w, `[{"number":9223372036854775807,"title":"exact","state":"open"}]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--json", "number,isDraft,url,createdAt,updatedAt,author.login,labels[].name,baseRefName,headRefName"})
	if err := rootCmd.Execute(); err != nil { t.Fatal(err) }
	for _, want := range []string{`9223372036854775807`, `"isDraft": null`, `"url": null`, `"createdAt": null`, `"updatedAt": null`, `"author": null`, `"labels": []`, `"baseRefName": null`, `"headRefName": null`} {
		if !strings.Contains(stdout.String(), want) { t.Errorf("JSON missing %q: %s", want, stdout.String()) }
	}
}

func TestPRListPageFailureDoesNotWritePartialJSON(t *testing.T) {
	resetPRCLIState(t)
	oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
	rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
	t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" { fmt.Fprint(w, `{"version":"1.22.0"}`); return }
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/pulls?page=2&limit=50>; rel=\"next\"")
			fmt.Fprint(w, `[{"number":1,"title":"partial","state":"open"}]`)
			return
		}
		http.Error(w, "fixture failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--json", "number"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "fixture failure") { t.Fatalf("error = %v", err) }
	if stdout.Len() != 0 { t.Fatalf("partial JSON was written: %s", stdout.String()) }
}

var _ *cobra.Command
