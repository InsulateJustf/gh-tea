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
)

func TestIssueListProjectsSelectedFieldsAndMapsFilters(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.URL.Path != "/api/v1/repos/alice/project/issues" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `[{"number":17,"title":"Fix it","state":"closed","html_url":"https://tea.example/alice/project/issues/17","created_at":"2026-02-03T04:05:06Z","updated_at":"2026-02-04T04:05:06Z","closed_at":null,"user":{"login":"author"},"labels":[{"name":"bug"}],"assignees":[{"login":"maintainer"}],"milestone":{"title":"v1"},"body":"details"}]`)
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
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "list", "--repo", "alice/project", "--state", "closed", "--limit", "7", "--label", "bug", "--label", "urgent", "--assignee", "maintainer", "--author", "author", "--mention", "watcher", "--search", "needle", "--json", "number,title,state,author.login,labels[].name,assignees[].login,milestone.title,body"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("issue list: %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(gotQuery, "state=closed") || !strings.Contains(gotQuery, "limit=50") || !strings.Contains(gotQuery, "labels=bug%2Curgent") || !strings.Contains(gotQuery, "assigned_by=maintainer") || !strings.Contains(gotQuery, "created_by=author") || !strings.Contains(gotQuery, "mentioned_by=watcher") || !strings.Contains(gotQuery, "q=needle") {
		t.Fatalf("API query = %q", gotQuery)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("output is not JSON: %v: %s", err, stdout.String())
	}
	if len(got) != 1 || got[0]["number"] != float64(17) || got[0]["title"] != "Fix it" || got[0]["body"] != "details" {
		t.Fatalf("projected output = %#v", got)
	}
	if got[0]["author"].(map[string]any)["login"] != "author" || got[0]["labels"].([]any)[0].(map[string]any)["name"] != "bug" {
		t.Fatalf("nested fields were not projected using gh field names: %#v", got[0])
	}
	if _, exists := got[0]["updatedAt"]; exists {
		t.Fatalf("unselected field updatedAt was included: %#v", got[0])
	}
}
func TestIssueUnsupportedFlagNamesCommandAndFlag(t *testing.T) {

	configPath := filepath.Join(t.TempDir(), "missing-config.yaml")
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "list", "--project", "roadmap"})
	err := rootCmd.Execute()
	rootCmd.SetArgs(nil)
	if err == nil || err.Error() != "--project is not supported by gh-tea" {
		t.Fatalf("error = %v", err)
	}
}
func TestIssueListFollowsServerPaginationWithFixedPageSize(t *testing.T) {
	var pageSizes []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.URL.Path != "/api/v1/repos/alice/project/issues" {
			http.NotFound(w, r)
			return
		}
		pageSizes = append(pageSizes, r.URL.Query().Get("limit"))
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/issues?page=2&limit=50>; rel=\"next\"")
			fmt.Fprint(w, `[{"number":1,"title":"first","state":"open"},{"number":2,"title":"second","state":"open"}]`)
			return
		}
		fmt.Fprint(w, `[{"number":3,"title":"third","state":"open"}]`)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "list", "--repo", "alice/project", "--limit", "5"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(pageSizes) != 2 || pageSizes[0] != "50" || pageSizes[1] != "50" {
		t.Fatalf("requested page sizes = %v, want fixed 50 for each page", pageSizes)
	}
	if !strings.Contains(stdout.String(), "third") {
		t.Fatalf("second page was not rendered: %s", stdout.String())
	}
}

func TestIssueEditPreservesUnchangedLabelsAndCanRemoveLastAssignee(t *testing.T) {
	var patched map[string]json.RawMessage
	var addedLabelIDs []int64
	var deletedLabelPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/17":
			fmt.Fprint(w, `{"number":17,"title":"Keep","state":"open","assignees":[{"login":"maintainer"}]}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/alice/project/issues/17":
			if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
				t.Errorf("decode PATCH body: %v", err)
			}
			fmt.Fprint(w, `{"number":17,"title":"Keep","state":"open","assignees":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/labels":
			fmt.Fprint(w, `[{"id":2,"name":"new"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/alice/project/issues/17/labels":
			var payload struct {
				Labels []int64 `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode label POST: %v", err)
			}
			addedLabelIDs = payload.Labels
			fmt.Fprint(w, `[{"id":1,"name":"old"},{"id":2,"name":"new"}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/17/labels":
			fmt.Fprint(w, `[{"id":1,"name":"old"}]`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/repos/alice/project/issues/17/labels/"):
			deletedLabelPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "edit", "17", "--repo", "alice/project", "--remove-assignee", "maintainer", "--add-label", "new", "--remove-label", "old"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var assignees []string
	if err := json.Unmarshal(patched["assignees"], &assignees); err != nil {
		t.Fatalf("PATCH assignees = %s: %v", patched["assignees"], err)
	}
	if assignees == nil || len(assignees) != 0 {
		t.Fatalf("PATCH assignees = %#v, want explicit empty array", assignees)
	}
	if len(addedLabelIDs) != 1 || addedLabelIDs[0] != 2 || deletedLabelPath != "/api/v1/repos/alice/project/issues/17/labels/1" {
		t.Fatalf("label operations: add=%v delete=%q", addedLabelIDs, deletedLabelPath)
	}
}
func TestIssueCreateMapsBodyFileLabelsAssigneesAndMilestone(t *testing.T) {
	var createBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/labels":
			fmt.Fprint(w, `[{"id":5,"name":"bug"}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/milestones":
			fmt.Fprint(w, `[{"id":9,"title":"v1"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/alice/project/issues":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			fmt.Fprint(w, `{"number":23,"title":"New issue","state":"open"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	bodyPath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bodyPath, []byte("details from file"), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "create", "--repo", "alice/project", "--title", "New issue", "--body-file", bodyPath, "--label", "bug", "--assignee", "maintainer", "--milestone", "v1"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if string(createBody["title"]) != `"New issue"` || string(createBody["body"]) != `"details from file"` || string(createBody["assignees"]) != `["maintainer"]` || string(createBody["labels"]) != `[5]` || string(createBody["milestone"]) != `9` {
		t.Fatalf("create payload = %s", createBody)
	}
}
func TestIssueViewCommentsCloseReopenCommentAndStatus(t *testing.T) {
	var states []string
	var commentBody string
	var statusFilters []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/17":
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open","body":"details"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/17/comments":
			fmt.Fprint(w, `[{"body":"useful","user":{"login":"reviewer"}}]`)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/alice/project/issues/17":
			var payload struct {
				State string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode state PATCH: %v", err)
			}
			states = append(states, payload.State)
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/alice/project/issues/17/comments":
			var payload struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode comment POST: %v", err)
			}
			commentBody = payload.Body
			fmt.Fprint(w, `{"body":"saved"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/user":
			fmt.Fprint(w, `{"login":"viewer"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues":
			statusFilters = append(statusFilters, r.URL.Query().Encode())
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		var stdout strings.Builder
		rootCmd.SetOut(&stdout)
		rootCmd.SetArgs(append([]string{"--config", configPath}, args...))
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return stdout.String()
	}
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
	})
	view := run("issue", "view", "17", "--repo", "alice/project", "--comments")
	if !strings.Contains(view, "Issue") || !strings.Contains(view, "reviewer: useful") {
		t.Fatalf("view output = %q", view)
	}
	run("issue", "close", "17", "--repo", "alice/project")
	run("issue", "reopen", "17", "--repo", "alice/project")
	run("issue", "comment", "17", "--repo", "alice/project", "--body", "test comment")
	status := run("issue", "status", "--repo", "alice/project")
	if len(states) != 2 || states[0] != "closed" || states[1] != "open" {
		t.Fatalf("issue states = %v", states)
	}
	if commentBody != "test comment" {
		t.Fatalf("comment body = %q", commentBody)
	}
	if !strings.Contains(status, "Assigned to you") || !strings.Contains(status, "Created by you") || !strings.Contains(status, "Mentioning you") {
		t.Fatalf("status output = %q", status)
	}
	if len(statusFilters) != 3 || !strings.Contains(statusFilters[0], "assigned_by=viewer") || !strings.Contains(statusFilters[1], "created_by=viewer") || !strings.Contains(statusFilters[2], "mentioned_by=viewer") {
		t.Fatalf("status filters = %v", statusFilters)
	}
}
