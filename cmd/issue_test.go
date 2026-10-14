package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func resetIssueCLIState(t *testing.T) {
	t.Helper()
	reset := func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		for _, command := range []*cobra.Command{
			rootCmd, issueCmd, issueListCmd, issueCreateCmd, issueViewCmd, issueEditCmd,
			issueCloseCmd, issueReopenCmd, issueCommentCmd, issueStatusCmd,
		} {
			command.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
		}
		rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) { flag.Changed = false })
		configFlag, useFlag, repoFlag = "", "", ""
		debugFlag = false
		issueListState, issueListAssignee, issueListAuthor, issueListMention, issueListSearch = "open", "", "", "", ""
		issueListLabels, issueCreateLabels, issueCreateAssignees = nil, nil, nil
		issueListLimit, issueListJSON, issueCreateTitle, issueCreateBody, issueCreateBodyFile, issueCreateMilestone = 30, "", "", "", "", ""
		issueListWeb, issueViewComments, issueViewWeb = false, false, false
		issueViewJSON = ""
		issueEditTitle, issueEditBody, issueEditBodyFile, issueEditMilestone = "", "", "", ""
		issueEditAddLabels, issueEditRemoveLabels, issueEditAddAssignees, issueEditRemoveAssignees = nil, nil, nil, nil
		issueCommentBody, issueHost = "", ""
	}
	reset()
	t.Cleanup(reset)
}
func TestIssueListProjectsSelectedFieldsAndMapsFilters(t *testing.T) {
	resetIssueCLIState(t)
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
	resetIssueCLIState(t)

	configPath := filepath.Join(t.TempDir(), "missing-config.yaml")
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "list", "--project", "roadmap"})
	err := rootCmd.Execute()
	rootCmd.SetArgs(nil)
	if err == nil || err.Error() != "--project is not supported by gh-tea" {
		t.Fatalf("error = %v", err)
	}
}
func TestIssueListFollowsServerPaginationWithFixedPageSize(t *testing.T) {
	resetIssueCLIState(t)
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
	resetIssueCLIState(t)
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
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "edit", "17", "--repo", "alice/project", "--body", "updated", "--remove-assignee", "maintainer", "--add-label", "new", "--remove-label", "old"})
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
	if string(patched["body"]) != `"updated"` {
		t.Fatalf("PATCH body = %s", patched["body"])
	}
	if len(addedLabelIDs) != 1 || addedLabelIDs[0] != 2 || deletedLabelPath != "/api/v1/repos/alice/project/issues/17/labels/1" {
		t.Fatalf("label operations: add=%v delete=%q", addedLabelIDs, deletedLabelPath)
	}
}
func TestIssueCreateEditAndViewPreserveConsumerVisibleState(t *testing.T) {
	resetIssueCLIState(t)
	var createBody map[string]json.RawMessage
	var currentTitle, currentBody string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/labels":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/labels?page=2&limit=50>; rel=\"next\"")
				labels := make([]map[string]any, 50)
				for i := range labels {
					labels[i] = map[string]any{"id": i + 1, "name": fmt.Sprintf("unused-%d", i)}
				}
				_ = json.NewEncoder(w).Encode(labels)
			} else {
				fmt.Fprint(w, `[{"id":51,"name":"bug"}]`)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/milestones":
			if r.URL.Query().Get("state") != "all" {
				t.Errorf("milestone query state = %q, want all", r.URL.Query().Get("state"))
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/milestones?page=2&limit=50>; rel=\"next\"")
				milestones := make([]map[string]any, 50)
				for i := range milestones {
					milestones[i] = map[string]any{"id": i + 1, "title": fmt.Sprintf("unused-%d", i)}
				}
				_ = json.NewEncoder(w).Encode(milestones)
			} else {
				fmt.Fprint(w, `[{"id":51,"title":"v1","state":"closed"}]`)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/alice/project/issues":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			_ = json.Unmarshal(createBody["title"], &currentTitle)
			_ = json.Unmarshal(createBody["body"], &currentBody)
			fmt.Fprint(w, `{"number":23,"state":"open"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/23":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 23, "title": currentTitle, "body": currentBody, "state": "open"})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/alice/project/issues/23":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode edit body: %v", err)
			}
			if raw, ok := payload["title"]; ok {
				_ = json.Unmarshal(raw, &currentTitle)
			}
			if raw, ok := payload["body"]; ok {
				_ = json.Unmarshal(raw, &currentBody)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 23, "title": currentTitle, "body": currentBody, "state": "open"})
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
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if string(createBody["title"]) != `"New issue"` || string(createBody["body"]) != `"details from file"` || string(createBody["assignees"]) != `["maintainer"]` || string(createBody["labels"]) != `[51]` || string(createBody["milestone"]) != `51` {
		t.Fatalf("create payload = %s", createBody)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "edit", "23", "--repo", "alice/project", "--title", "Edited", "--body", "edited body"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "23", "--repo", "alice/project", "--json", "title,body"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var view map[string]string
	if err := json.Unmarshal([]byte(stdout.String()), &view); err != nil {
		t.Fatalf("view JSON is invalid: %v: %s", err, stdout.String())
	}
	if view["title"] != "Edited" || view["body"] != "edited body" {
		t.Fatalf("view after edit = %#v", view)
	}
}
func TestIssueViewCommentsCloseReopenCommentAndStatus(t *testing.T) {
	resetIssueCLIState(t)
	var states []string
	var statusFilters []string
	issueState := "open"
	commentBodies := []map[string]any{{"body": "useful", "user": map[string]string{"login": "reviewer"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/17":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 17, "title": "Issue", "state": issueState, "body": "details"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues/17/comments":
			_ = json.NewEncoder(w).Encode(commentBodies)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/alice/project/issues/17":
			var payload struct {
				State string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode state PATCH: %v", err)
			}
			issueState = payload.State
			states = append(states, payload.State)
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 17, "title": "Issue", "state": issueState})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/alice/project/issues/17/comments":
			var payload struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode comment POST: %v", err)
			}
			commentBodies = append(commentBodies, map[string]any{"body": payload.Body, "user": map[string]string{"login": "viewer"}})
			_ = json.NewEncoder(w).Encode(map[string]any{"body": payload.Body})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/user":
			fmt.Fprint(w, `{"login":"viewer"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/alice/project/issues":
			query := r.URL.Query()
			statusFilters = append(statusFilters, query.Encode())
			switch {
			case query.Get("assigned_by") == "viewer":
				fmt.Fprint(w, `[{"number":1,"title":"Assigned match","state":"open"}]`)
			case query.Get("created_by") == "viewer":
				fmt.Fprint(w, `[{"number":2,"title":"Created match","state":"open"}]`)
			case query.Get("mentioned_by") == "viewer":
				fmt.Fprint(w, `[{"number":3,"title":"Mentioned match","state":"open"}]`)
			case query.Get("state") == issueState:
				_ = json.NewEncoder(w).Encode([]map[string]any{{"number": 17, "title": "Issue", "state": issueState}})
			default:
				fmt.Fprint(w, `[]`)
			}
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
	view := run("issue", "view", "17", "--repo", "alice/project", "--comments")
	if !strings.Contains(view, "Issue") || !strings.Contains(view, "reviewer: useful") {
		t.Fatalf("view output = %q", view)
	}
	run("issue", "close", "17", "--repo", "alice/project")
	closed := run("issue", "list", "--repo", "alice/project", "--state", "closed")
	if !strings.Contains(closed, "Issue") {
		t.Fatalf("closed issue was not listed: %q", closed)
	}
	run("issue", "reopen", "17", "--repo", "alice/project")
	run("issue", "comment", "17", "--repo", "alice/project", "--body", "test comment")
	withComment := run("issue", "view", "17", "--repo", "alice/project", "--comments")
	if !strings.Contains(withComment, "viewer: test comment") {
		t.Fatalf("comment was not visible after creation: %q", withComment)
	}
	status := run("issue", "status", "--repo", "alice/project")
	if len(states) != 2 || states[0] != "closed" || states[1] != "open" {
		t.Fatalf("issue states = %v", states)
	}
	for _, title := range []string{"Assigned match", "Created match", "Mentioned match"} {
		if !strings.Contains(status, title) {
			t.Fatalf("status missing %q: %q", title, status)
		}
	}
	if len(statusFilters) != 4 || !strings.Contains(statusFilters[1], "assigned_by=viewer") || !strings.Contains(statusFilters[2], "created_by=viewer") || !strings.Contains(statusFilters[3], "mentioned_by=viewer") {
		t.Fatalf("status filters = %v", statusFilters)
	}
}
func TestIssueViewJSONIncludesPullRequestMetadataAsBoolean(t *testing.T) {
	for _, test := range []struct {
		name        string
		pullRequest string
		expected    bool
	}{
		{name: "issue", expected: false},
		{name: "pull request", pullRequest: `,"pull_request":{}`, expected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetIssueCLIState(t)
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				switch r.URL.Path {
				case "/api/v1/version":
					fmt.Fprint(w, `{"version":"1.22.0"}`)
				case "/api/v1/repos/alice/project/issues/17":
					fmt.Fprintf(w, `{"number":17,"title":"Issue","state":"open","html_url":"https://example/issues/17"%s}`, test.pullRequest)
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
			var stdout strings.Builder
			rootCmd.SetOut(&stdout)
			rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "number,isPullRequest"})
			t.Cleanup(func() {
				rootCmd.SetArgs(nil)
				rootCmd.SetOut(os.Stdout)
			})
			if err := rootCmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
				t.Fatalf("view JSON is invalid: %v: %s", err, stdout.String())
			}
			if got["number"] != float64(17) || got["isPullRequest"] != test.expected {
				t.Fatalf("view JSON = %#v", got)
			}
			if len(requests) != 2 || requests[1] != "/api/v1/repos/alice/project/issues/17" {
				t.Fatalf("requests = %#v", requests)
			}
		})
	}
}

func TestIssueViewJSONOmitsUnselectedPullRequestMetadata(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		} else if r.URL.Path == "/api/v1/repos/alice/project/issues/17" {
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open","user":{"login":"author"},"pull_request":{}}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "number,title,author.login"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("view JSON is invalid: %v: %s", err, stdout.String())
	}
	if got["number"] != float64(17) || got["title"] != "Issue" || got["author"].(map[string]any)["login"] != "author" {
		t.Fatalf("view JSON = %#v", got)
	}
	if _, exists := got["isPullRequest"]; exists {
		t.Fatalf("unselected isPullRequest was included: %#v", got)
	}
}

func TestIssueListRejectsViewOnlyJSONFields(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		} else {
			http.Error(w, "field validation should run before issue listing", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"isPullRequest", "comments"} {
		rootCmd.SetArgs([]string{"--config", configPath, "issue", "list", "--repo", "alice/project", "--json", field})
		err := rootCmd.Execute()
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(`unknown JSON field %q; available fields:`, field)) ||
			!strings.HasSuffix(err.Error(), "available fields: "+issueListJSONFields) {
			t.Fatalf("list error for %q = %v", field, err)
		}
	}
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
}

func TestIssueViewUnknownJSONFieldListsViewFields(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		} else {
			http.Error(w, "field validation should run before issue view", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "notAField"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown JSON field "notAField"; available fields:`) || !strings.HasSuffix(err.Error(), "available fields: "+issueViewJSONFields) {
		t.Fatalf("view error = %v", err)
	}
}
func TestIssueCreateWithoutTTYRequiresTitle(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	originalStdin := os.Stdin
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = stdin
	t.Cleanup(func() {
		os.Stdin = originalStdin
		stdin.Close()
	})
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "create", "--repo", "alice/project"})
	err = rootCmd.Execute()
	if err == nil || err.Error() != "issue create requires --title or a TTY for prompts" {
		t.Fatalf("error = %v", err)
	}
}
func TestIssueUnknownJSONFieldListsAvailableFields(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		} else {
			http.Error(w, "field validation should run before issue listing", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "list", "--repo", "alice/project", "--json", "notAField"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown JSON field "notAField"; available fields:`) || !strings.HasSuffix(err.Error(), "available fields: "+issueListJSONFields) {
		t.Fatalf("error = %v", err)
	}
}
func TestIssueViewJSONCommentsPreserveSourceDataAndFetchAllPages(t *testing.T) {
	resetIssueCLIState(t)
	var commentRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/repos/alice/project/issues/17":
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open"}`)
		case "/api/v1/repos/alice/project/issues/17/comments":
			commentRequests++
			if r.URL.Query().Get("page") == "1" {
				comments := make([]map[string]any, 50)
				for i := range comments {
					comments[i] = map[string]any{"id": i + 1, "body": fmt.Sprintf("comment-%d", i+1)}
				}
				_ = json.NewEncoder(w).Encode(comments)
				return
			}
			fmt.Fprint(w, `[{"id":9007199254740993,"body":"  **raw**\n","user":{"login":"writer"},"created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T04:05:06+01:00","html_url":"https://example/comments/9007199254740993"}]`)
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
	var stdout, stderr strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "title,comments"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("issue view: %v (stderr: %s)", err, stderr.String())
	}
	var got struct {
		Title    string `json:"title"`
		Comments []struct {
			ID        json.RawMessage `json:"id"`
			Author    *struct{ Login string `json:"login"` } `json:"author"`
			Body      string `json:"body"`
			CreatedAt *string `json:"createdAt"`
			UpdatedAt *string `json:"updatedAt"`
			URL       *string `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("view JSON is invalid: %v: %s", err, stdout.String())
	}
	if got.Title != "Issue" || len(got.Comments) != 51 || commentRequests != 2 {
		t.Fatalf("title/comments/request count = %q/%d/%d", got.Title, len(got.Comments), commentRequests)
	}
	if got.Comments[0].Body != "comment-1" || got.Comments[49].Body != "comment-50" {
		t.Fatalf("comments did not preserve server page order: first=%q fiftieth=%q", got.Comments[0].Body, got.Comments[49].Body)
	}
	last := got.Comments[50]
	if string(last.ID) != "9007199254740993" || last.Author == nil || last.Author.Login != "writer" ||
		last.Body != "  **raw**\n" || last.CreatedAt == nil || *last.CreatedAt != "2026-01-02T03:04:05Z" ||
		last.UpdatedAt == nil || *last.UpdatedAt != "2026-01-03T04:05:06+01:00" ||
		last.URL == nil || *last.URL != "https://example/comments/9007199254740993" {
		t.Fatalf("last comment = %#v", last)
	}
}

func TestIssueViewJSONCommentsFailureDoesNotWritePartialOutput(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/repos/alice/project/issues/17":
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open"}`)
		case "/api/v1/repos/alice/project/issues/17/comments":
			if r.URL.Query().Get("page") == "1" {
				comments := make([]map[string]any, 50)
				for i := range comments {
					comments[i] = map[string]any{"id": i + 1, "body": "partial"}
				}
				_ = json.NewEncoder(w).Encode(comments)
				return
			}
			http.Error(w, "second page unavailable", http.StatusInternalServerError)
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
	var stdout, stderr strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "title,comments"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
	})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "second page unavailable") {
		t.Fatalf("issue view error = %v", err)
	}
	if !strings.Contains(stderr.String(), "second page unavailable") {
		t.Fatalf("failed JSON view did not write a diagnostic to stderr: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed JSON view wrote to stdout: %s", stdout.String())
	}
}
func TestIssueViewJSONCommentsEmptyAndUnrequested(t *testing.T) {
	for _, test := range []struct {
		name             string
		jsonFields       string
		withCommentsFlag bool
		wantComments     bool
	}{
		{name: "empty selected comments", jsonFields: "title,comments", wantComments: true},
		{name: "unselected comments", jsonFields: "title"},
		{name: "comments flag does not change JSON projection", jsonFields: "title", withCommentsFlag: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetIssueCLIState(t)
			commentRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/version":
					fmt.Fprint(w, `{"version":"1.22.0"}`)
				case "/api/v1/repos/alice/project/issues/17":
					fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open"}`)
				case "/api/v1/repos/alice/project/issues/17/comments":
					commentRequests++
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
			var stdout strings.Builder
			rootCmd.SetOut(&stdout)
			args := []string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", test.jsonFields}
			if test.withCommentsFlag {
				args = append(args, "--comments")
			}
			rootCmd.SetArgs(args)
			t.Cleanup(func() {
				rootCmd.SetArgs(nil)
				rootCmd.SetOut(os.Stdout)
			})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("issue view: %v", err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
				t.Fatalf("view JSON is invalid: %v: %s", err, stdout.String())
			}
			raw, selected := got["comments"]
			if selected != test.wantComments {
				t.Fatalf("comments field selected = %v, want %v: %s", selected, test.wantComments, stdout.String())
			}
			if test.wantComments && string(raw) != "[]" {
				t.Fatalf("empty comments = %s, want []", raw)
			}
			wantRequests := 0
			if test.wantComments {
				wantRequests = 1
			}
			if commentRequests != wantRequests {
				t.Fatalf("comment requests = %d, want %d", commentRequests, wantRequests)
			}
		})
	}
}

func TestIssueViewJSONCommentsMissingOptionalValuesAreNull(t *testing.T) {
	resetIssueCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/repos/alice/project/issues/17":
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open"}`)
		case "/api/v1/repos/alice/project/issues/17/comments":
			fmt.Fprint(w, `[{"id":42,"body":""}]`)
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
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "comments"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(os.Stdout)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Comments []struct {
			ID        int64           `json:"id"`
			Author    json.RawMessage `json:"author"`
			Body      string          `json:"body"`
			CreatedAt json.RawMessage `json:"createdAt"`
			UpdatedAt json.RawMessage `json:"updatedAt"`
			URL       json.RawMessage `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("view JSON is invalid: %v: %s", err, stdout.String())
	}
	if len(got.Comments) != 1 {
		t.Fatalf("comments = %#v", got.Comments)
	}
	comment := got.Comments[0]
	if comment.ID != 42 || string(comment.Author) != "null" || comment.Body != "" ||
		string(comment.CreatedAt) != "null" || string(comment.UpdatedAt) != "null" || string(comment.URL) != "null" {
		t.Fatalf("comment = %#v", comment)
	}
}

func TestIssueViewJSONCommentsFailureReturnsNonzeroWithoutStdout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/repos/alice/project/issues/17":
			fmt.Fprint(w, `{"number":17,"title":"Issue","state":"open"}`)
		case "/api/v1/repos/alice/project/issues/17/comments":
			if r.URL.Query().Get("page") == "1" {
				comments := make([]map[string]any, 50)
				for i := range comments {
					comments[i] = map[string]any{"id": i + 1, "body": "partial"}
				}
				_ = json.NewEncoder(w).Encode(comments)
				return
			}
			http.Error(w, "second page unavailable", http.StatusInternalServerError)
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
	process := exec.Command(os.Args[0], "-test.run=^TestIssueViewJSONCommentsFailureProcessHelper$")
	process.Env = append(os.Environ(), "GH_TEA_ISSUE_TEST_CONFIG="+configPath)
	stdout, err := process.Output()
	if len(stdout) != 0 {
		t.Fatalf("failed CLI process wrote to stdout: %s", stdout)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("CLI process error = %v, want nonzero exit", err)
	}
	if !strings.Contains(string(exitErr.Stderr), "second page unavailable") {
		t.Fatalf("CLI process stderr lacks a diagnostic: %s", exitErr.Stderr)
	}
}

func TestIssueViewJSONCommentsFailureProcessHelper(t *testing.T) {
	configPath := os.Getenv("GH_TEA_ISSUE_TEST_CONFIG")
	if configPath == "" {
		return
	}
	os.Args = []string{"gh-tea-test", "--config", configPath, "issue", "view", "17", "--repo", "alice/project", "--json", "title,comments"}
	Execute()
}
