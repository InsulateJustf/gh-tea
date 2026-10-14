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

func resetRepoCLIState(t *testing.T) {
	t.Helper()
	reset := func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		for _, command := range []*cobra.Command{rootCmd, repoCmd, repoListCmd, repoViewCmd, repoCreateCmd, repoDeleteCmd, repoEditCmd, repoForkCmd, repoRenameCmd, repoCloneCmd, repoBrowseCmd} {
			command.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
		}
		rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
		configFlag, useFlag, repoFlag = "", "", ""
		repoListLimit, repoListJSON, repoViewJSON, repoCreateDescription, repoCreateOrg = 30, "", "", "", ""
		repoViewWeb, repoCreatePrivate, repoCreateClone = false, false, false
		repoDeleteYes, repoEditDescription, repoEditHomepage, repoEditPrivate, repoEditArchived = false, "", "", false, false
		repoForkOrg, repoForkClone = "", false
	}
	reset()
	t.Cleanup(reset)
}

func TestRepoListPaginatesAndOutputsSelectedFields(t *testing.T) {
	resetRepoCLIState(t)
	var pages []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.URL.Path != "/api/v1/user/repos" {
			http.NotFound(w, r)
			return
		}
		pages = append(pages, r.URL.Query().Get("page"))
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", "<"+server.URL+"/api/v1/user/repos?page=2&limit=50>; rel=\"next\"")
			fmt.Fprint(w, `[{"name":"one","full_name":"alice/one","description":"first","private":true,"html_url":"https://tea.example/alice/one","updated_at":"2026-01-01T00:00:00Z"}]`)
			return
		}
		fmt.Fprint(w, `[{"name":"two","full_name":"alice/two","description":"second","private":false,"html_url":"https://tea.example/alice/two"}]`)
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
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "list", "--json", "name,nameWithOwner,description,visibility,isPrivate,url"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo list: %v (stderr: %s)", err, stderr.String())
	}
	if len(pages) != 2 || pages[0] != "1" || pages[1] != "2" {
		t.Fatalf("requested pages = %v, want [1 2]", pages)
	}
	for _, want := range []string{`"name": "one"`, `"nameWithOwner": "alice/two"`, `"visibility": "PRIVATE"`, `"isPrivate": true`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("JSON output missing %s: %s", want, stdout.String())
		}
	}
}
func TestRepoCreateUsesPrivateOrgEndpointAndDescription(t *testing.T) {
	resetRepoCLIState(t)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/org/team/repos" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode repository create: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"name":"demo","full_name":"team/demo","clone_url":"https://tea.example/team/demo.git"}`)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetOut(&strings.Builder{})
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "create", "demo", "--private", "--description", "demo description", "--org", "team"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	if got["name"] != "demo" || got["description"] != "demo description" || got["private"] != true {
		t.Fatalf("create payload = %#v", got)
	}
}

func TestRepoDeleteRequiresYesWithoutInteractiveInput(t *testing.T) {
	resetRepoCLIState(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "delete", "alice/demo"})
	err := rootCmd.Execute()
	if err == nil || err.Error() != "delete requires --yes in non-interactive mode" {
		t.Fatalf("delete error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("delete made %d API requests without confirmation", requests)
	}
}

func TestRepoForkEditAndRenameUseGiteaEndpoints(t *testing.T) {
	resetRepoCLIState(t)
	var forkBody, editBody, renameBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/source/project/forks":
			_ = json.NewDecoder(r.Body).Decode(&forkBody)
			fmt.Fprint(w, `{"full_name":"group/project","clone_url":"https://tea.example/group/project.git"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/alice/project":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["name"] == "renamed" {
				renameBody = payload
				fmt.Fprint(w, `{"full_name":"alice/renamed"}`)
			} else {
				editBody = payload
				fmt.Fprint(w, `{"full_name":"alice/project"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		rootCmd.SetArgs(append([]string{"--config", configPath}, args...))
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
	}
	rootCmd.SetOut(&strings.Builder{})
	run("repo", "fork", "source/project", "--org", "group")
	run("repo", "edit", "alice/project", "--description", "updated", "--homepage", "https://example.test", "--private", "--archived")
	run("--repo", "alice/project", "repo", "rename", "renamed")
	if forkBody["organization"] != "group" {
		t.Fatalf("fork payload = %#v", forkBody)
	}
	if editBody["description"] != "updated" || editBody["website"] != "https://example.test" || editBody["private"] != true || editBody["archived"] != true {
		t.Fatalf("edit payload = %#v", editBody)
	}
	if renameBody["name"] != "renamed" {
		t.Fatalf("rename payload = %#v", renameBody)
	}
}

func TestRepoCloneAndBrowseUseRepositoryURLs(t *testing.T) {
	resetRepoCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project" {
			fmt.Fprint(w, `{"name":"project","full_name":"alice/project","html_url":"https://tea.example/alice/project","clone_url":"https://tea.example/alice/project.git"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	oldClone, oldBrowser := repoGitClone, repoBrowserOpen
	defer func() { repoGitClone, repoBrowserOpen = oldClone, oldBrowser }()
	var gotCloneURL, gotDir, gotPageURL string
	repoGitClone = func(cloneURL, dir string) error { gotCloneURL, gotDir = cloneURL, dir; return nil }
	repoBrowserOpen = func(pageURL string) error { gotPageURL = pageURL; return nil }
	rootCmd.SetOut(&strings.Builder{})
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "clone", "alice/project", "checkout"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo clone: %v", err)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "--repo", "alice/project", "repo", "browse"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo browse: %v", err)
	}
	if gotCloneURL != "https://tea.example/alice/project.git" || gotDir != "checkout" {
		t.Fatalf("clone args = %q %q", gotCloneURL, gotDir)
	}
	if gotPageURL != server.URL+"/alice/project" {
		t.Fatalf("browse URL = %q", gotPageURL)
	}
}

func TestRepoViewJSONAndDeleteYes(t *testing.T) {
	resetRepoCLIState(t)
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project":
			fmt.Fprint(w, `{"name":"project","full_name":"alice/project","description":"details","private":false,"html_url":"https://tea.example/alice/project","default_branch":"main"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/alice/project":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "view", "alice/project", "--json", "name,nameWithOwner,description,visibility,defaultBranchRef.name"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo view: %v", err)
	}
	for _, want := range []string{`"name": "project"`, `"nameWithOwner": "alice/project"`, `"visibility": "PUBLIC"`, `"defaultBranchRef"`, `"name": "main"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("view JSON missing %s: %s", want, stdout.String())
		}
	}
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "delete", "alice/project", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo delete --yes: %v", err)
	}
	if !deleted {
		t.Fatal("repository was not deleted")
	}
}

func TestRepoCreatePrivateWithoutDescriptionUsesUserEndpoint(t *testing.T) {
	resetRepoCLIState(t)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/user/repos" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"name":"demo","full_name":"alice/demo"}`)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetOut(&strings.Builder{})
	rootCmd.SetArgs([]string{"--config", configPath, "repo", "create", "demo", "--private"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	if got["name"] != "demo" || got["private"] != true {
		t.Fatalf("create payload = %#v", got)
	}
}
