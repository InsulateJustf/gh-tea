package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func prTestConfig(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("tea:\n  test:\n    name: test\n    host: %s\n    token: synthetic-token\ndefault: test\n", server.URL)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPRListResolvesMeAndFiltersAllLabelsAcrossPages(t *testing.T) {
	resetPRCLIState(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/user":
			fmt.Fprint(w, `{"login":"me"}`)
		case "/api/v1/repos/alice/project/pulls":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", "<"+server.URL+"/api/v1/repos/alice/project/pulls?page=2&limit=50>; rel=\"next\"")
				fmt.Fprint(w, `[{"number":1,"title":"other author","state":"open","draft":false,"user":{"login":"other"},"labels":[{"name":"bug"},{"name":"urgent"}]},{"number":2,"title":"one label","state":"open","user":{"login":"me"},"labels":[{"name":"bug"}]}]`)
			} else {
				fmt.Fprint(w, `[{"number":3,"title":"both labels","state":"open","draft":true,"user":{"login":"me"},"labels":[{"name":"bug"},{"name":"urgent"}]}]`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--state", "all", "--limit", "1", "--author", "@me", "--label", "bug,urgent", "--json", "number,title,isDraft"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(stdout.String()), "[\n  {\n    \"isDraft\": true,\n    \"number\": 3,\n    \"title\": \"both labels\"\n  }\n]"; got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func TestPRListClosedStateExcludesMergedInServerOrder(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		fmt.Fprint(w, `[{"number":8,"title":"merged","state":"closed","merged":true},{"number":9,"title":"closed","state":"closed","merged":false}]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--state", "closed", "--limit", "1", "--json", "number,title,state"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"number": 9`) || strings.Contains(stdout.String(), `"number": 8`) {
		t.Fatalf("closed result = %s", stdout.String())
	}
}

func TestPRListPreservesMaxInt64AndProjectsMissingDataAsNull(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		fmt.Fprint(w, `[{"number":9223372036854775807,"title":"exact","state":"open"}]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--json", "number,isDraft,url,createdAt,updatedAt,author.login,labels[].name,baseRefName,headRefName"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`9223372036854775807`, `"isDraft": null`, `"url": null`, `"createdAt": null`, `"updatedAt": null`, `"author": null`, `"labels": []`, `"baseRefName": null`, `"headRefName": null`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing %q: %s", want, stdout.String())
		}
	}
}

func TestPRListPageFailureDoesNotWritePartialJSON(t *testing.T) {
	resetPRCLIState(t)
	oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
	rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
	t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
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
	if err == nil || !strings.Contains(err.Error(), "fixture failure") {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("partial JSON was written: %s", stdout.String())
	}
}

func TestPRListStopsAfterCollectingLimitMatches(t *testing.T) {
	resetPRCLIState(t)
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		pages++
		if r.URL.Query().Get("page") != "1" {
			http.Error(w, "unexpected extra page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Link", `<https://untrusted.invalid/api/v1/repos/elsewhere/pulls?page=2&token=leak>; rel="next"`)
		fmt.Fprint(w, `[{"number":1,"title":"first match","state":"open"}]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--limit", "1", "--json", "number"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if pages != 1 {
		t.Fatalf("pull request pages fetched = %d, want 1", pages)
	}
	if !strings.Contains(stdout.String(), `"number": 1`) {
		t.Fatalf("JSON = %s, want first match", stdout.String())
	}
}
func TestPRListUsesLinkPageNumberOnConfiguredEndpoint(t *testing.T) {
	resetPRCLIState(t)
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		if r.URL.Path != "/api/v1/repos/alice/project/pulls" {
			t.Errorf("unexpected endpoint: %s", r.URL.String())
			return
		}
		pages = append(pages, r.URL.Query().Get("page"))
		if r.Header.Get("Authorization") != "token synthetic-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", `<https://untrusted.invalid/other?page=3&token=leak>; rel="next"`)
			fmt.Fprint(w, `[{"number":1,"title":"not open","state":"closed"}]`)
		case "3":
			fmt.Fprint(w, `[{"number":3,"title":"match","state":"open"}]`)
		default:
			t.Errorf("unexpected page: %s", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--limit", "1", "--json", "number"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(pages, ",") != "1,3" {
		t.Fatalf("requested pages = %v, want [1 3]", pages)
	}
	if !strings.Contains(stdout.String(), `"number": 3`) {
		t.Fatalf("JSON = %s, want page 3 match", stdout.String())
	}
}

func TestPRListProjectsMissingNestedRefsAsNull(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			fmt.Fprint(w, `{"version":"1.22.0"}`)
			return
		}
		fmt.Fprint(w, `[{"number":1,"title":"missing nested refs","state":"open","base":{},"head":{"ref":null}}]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--json", "baseRefName,headRefName"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"baseRefName": null`, `"headRefName": null`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing %q: %s", want, stdout.String())
		}
	}
}
func TestPRViewProjectsSelectedFieldsWithoutFetchingUnselectedCollections(t *testing.T) {
	resetPRCLIState(t)
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"title":"keep  body","state":"closed","merged":true,"draft":false,"html_url":"https://tea.example/pr/12","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-03T03:04:05Z","closed_at":"2026-01-04T03:04:05Z","merged_at":"2026-01-04T03:04:05Z","body":" original\nbody ","user":{"login":"alice"},"labels":[{"name":"bug"}],"base":{"ref":"main"},"head":{"ref":"fix"},"mergeable":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "number,title,state,isDraft,url,createdAt,updatedAt,closedAt,author.login,labels[].name,baseRefName,headRefName,body,merged,mergedAt,mergeable"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"number": 12`, `"state": "merged"`, `"isDraft": false`, `"body": " original\nbody "`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing %q: %s", want, stdout.String())
		}
	}
	if len(requests) != 1 || requests[0] != "/api/v1/repos/alice/project/pulls/12" {
		t.Fatalf("requests = %v; expected only selected PR", requests)
	}
}

func TestPRViewDoesNotTreatIssueAsPullRequest(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.0"}`)
		case "/api/v1/repos/alice/project/issues/12":
			fmt.Fprint(w, `{"number":12,"title":"ordinary issue","state":"open"}`)
		case "/api/v1/repos/alice/project/pulls/12":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	var stdout strings.Builder
	oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
	rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
	t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "number"})
	err := rootCmd.Execute()
	if err == nil || stdout.Len() != 0 {
		t.Fatalf("error = %v, stdout = %s; expected missing PR failure", err, stdout.String())
	}
}
func TestPRViewCollectsAllPagesOfSelectedCollections(t *testing.T) {
	resetPRCLIState(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"title":"all pages","state":"open"}`)
		case r.URL.Path == "/api/v1/repos/alice/project/pulls/12/files":
			writePRViewPage(w, r, func(i int) string {
				return fmt.Sprintf(`{"filename":"file-%d","status":"modified","additions":1,"deletions":2}`, i)
			})
		case r.URL.Path == "/api/v1/repos/alice/project/issues/12/comments":
			writePRViewPage(w, r, func(i int) string {
				return fmt.Sprintf(`{"id":%d,"body":"comment-%d"}`, i+1, i+1)
			})
		case r.URL.Path == "/api/v1/repos/alice/project/pulls/12/reviews":
			writePRViewPage(w, r, func(i int) string {
				return fmt.Sprintf(`{"id":%d,"body":"review-%d","state":"APPROVED"}`, i+1, i+1)
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "files,comments,reviews"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"path": "file-100"`, `"previousPath": null`, `"body": "comment-101"`, `"body": "review-101"`, `"author": null`, `"createdAt": null`, `"updatedAt": null`, `"url": null`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing last-page value %q", want)
		}
	}
}

func TestPRViewFollowsShortPagesAndUsesNextPageReviewIDs(t *testing.T) {
	resetPRCLIState(t)
	requests := make([]string, 0)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"state":"open"}`)
		case "/api/v1/repos/alice/project/pulls/12/reviews":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<https://attacker.invalid/api/v1/repos/alice/project/pulls/12/reviews?limit=50&page=2>; rel="next"`)
				fmt.Fprint(w, `[{"id":91}]`)
			} else {
				fmt.Fprint(w, `[{"id":9223372036854775807}]`)
			}
		case "/api/v1/repos/alice/project/pulls/12/reviews/91/comments":
			fmt.Fprint(w, `[{"id":9223372036854775807,"pull_request_review_id":91,"body":"later review inline"}]`)
		case "/api/v1/repos/alice/project/pulls/12/reviews/9223372036854775807/comments":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "reviews,reviewComments"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`9223372036854775807`, `"body": "later review inline"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing %q: %s", want, stdout.String())
		}
	}
	if !containsRequest(requests, "page=2") {
		t.Fatalf("requests = %v; expected next review page", requests)
	}
}

func TestPRViewShortPageNextFailureWritesNoJSON(t *testing.T) {
	resetPRCLIState(t)
	oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
	rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
	t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/alice/project/pulls/12" {
			fmt.Fprint(w, `{"number":12,"state":"open"}`)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<https://attacker.invalid/api/v1/repos/alice/project/pulls/12/files?limit=50&page=2>; rel="next"`)
			fmt.Fprint(w, `[{"filename":"first","status":"modified","additions":1,"deletions":0}]`)
			return
		}
		http.Error(w, "second page failed", http.StatusInternalServerError)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "files"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "second page failed") || stdout.Len() != 0 {
		t.Fatalf("error = %v, stdout = %s; expected atomic failure", err, stdout.String())
	}
}

func containsRequest(requests []string, fragment string) bool {
	for _, request := range requests {
		if strings.Contains(request, fragment) {
			return true
		}
	}
	return false
}
func writePRViewPage(w http.ResponseWriter, r *http.Request, item func(int) string) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	start := (page - 1) * 50
	count := 50
	if start >= 100 {
		count = 1
	}
	rows := make([]string, count)
	for i := range rows {
		rows[i] = item(start + i)
	}
	fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))
}

func TestPRViewReviewCommentsAreFetchedOncePerReview(t *testing.T) {
	resetPRCLIState(t)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"title":"inline","state":"open"}`)
		case "/api/v1/repos/alice/project/pulls/12/reviews":
			fmt.Fprint(w, `[{"id":9},{"id":10}]`)
		case "/api/v1/repos/alice/project/pulls/12/reviews/9/comments":
			fmt.Fprint(w, `[{"id":91,"pull_request_review_id":9,"body":"first inline","position":4}]`)
		case "/api/v1/repos/alice/project/pulls/12/reviews/10/comments":
			fmt.Fprint(w, `[{"id":101,"pull_request_review_id":10,"body":"second inline","position":5}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "reviewComments"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first inline", "second inline", `"reviewId": 9`, `"reviewId": 10`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing %q: %s", want, stdout.String())
		}
	}
	if len(requests) != 4 || strings.Contains(requests[2], "?") || strings.Contains(requests[3], "?") {
		t.Fatalf("review-comment requests = %v; want one unpaged request per review", requests)
	}
}

func TestPRViewLaterCollectionPageFailureWritesNoJSON(t *testing.T) {
	resetPRCLIState(t)
	oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
	rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
	t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/alice/project/pulls/12" {
			fmt.Fprint(w, `{"number":12,"title":"partial","state":"open"}`)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			rows := make([]string, 50)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"filename":"file-%d","status":"modified","additions":1,"deletions":0}`, i)
			}
			fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))
			return
		}
		http.Error(w, "second page failed", http.StatusInternalServerError)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "number,files"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "second page failed") || stdout.Len() != 0 {
		t.Fatalf("error = %v, stdout = %s; expected atomic failure", err, stdout.String())
	}
}
func TestPRViewPreservesMaxInt64AndProjectsMissingData(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/alice/project/pulls/9223372036854775807" {
			fmt.Fprint(w, `{"number":9223372036854775807,"title":"missing","state":"open"}`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "9223372036854775807", "--repo", "alice/project", "--json", "number,isDraft,url,createdAt,updatedAt,closedAt,author.login,labels[].name,baseRefName,headRefName,merged,mergedAt,mergeable,files"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`9223372036854775807`, `"isDraft": null`, `"url": null`, `"createdAt": null`, `"updatedAt": null`, `"closedAt": null`, `"author": null`, `"labels": []`, `"baseRefName": null`, `"headRefName": null`, `"merged": false`, `"mergedAt": null`, `"mergeable": null`, `"files": []`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("JSON missing %q: %s", want, stdout.String())
		}
	}
}
func TestPRViewPreservesMaxInt64InDiscussionAndReviewIDs(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/alice/project/pulls/9223372036854775807":
			fmt.Fprint(w, `{"number":9223372036854775807,"state":"open"}`)
		case "/api/v1/repos/alice/project/issues/9223372036854775807/comments":
			fmt.Fprint(w, `[{"id":9223372036854775807}]`)
		case "/api/v1/repos/alice/project/pulls/9223372036854775807/reviews":
			fmt.Fprint(w, `[{"id":9223372036854775807}]`)
		case "/api/v1/repos/alice/project/pulls/9223372036854775807/reviews/9223372036854775807/comments":
			fmt.Fprint(w, `[{"id":9223372036854775807,"pull_request_review_id":9223372036854775807}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "9223372036854775807", "--repo", "alice/project", "--json", "number,comments,reviews,reviewComments"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(stdout.String(), "9223372036854775807") != 5 {
		t.Fatalf("maximum IDs were not preserved: %s", stdout.String())
	}
}

func TestPRViewLaterReviewInlineFailureWritesNoJSON(t *testing.T) {
	resetPRCLIState(t)
	oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
	rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
	t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"state":"open"}`)
		case "/api/v1/repos/alice/project/pulls/12/reviews":
			fmt.Fprint(w, `[{"id":1},{"id":2}]`)
		case "/api/v1/repos/alice/project/pulls/12/reviews/1/comments":
			fmt.Fprint(w, `[{"id":10,"pull_request_review_id":1,"body":"first inline"}]`)
		case "/api/v1/repos/alice/project/pulls/12/reviews/2/comments":
			http.Error(w, "later inline failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "reviewComments"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "later inline failed") || stdout.Len() != 0 {
		t.Fatalf("error = %v, stdout = %s; expected atomic failure", err, stdout.String())
	}
}

func TestPRViewRejectsUnknownFieldsBeforeRequest(t *testing.T) {
	resetPRCLIState(t)
	rootCmd.SetArgs([]string{"pr", "view", "1", "--repo", "alice/project", "--json", "files,unexpected"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown JSON field "unexpected"`) {
		t.Fatalf("error = %v", err)
	}
}
func TestPRDiffWritesOriginalBinaryUnifiedDiffBytes(t *testing.T) {
	resetPRCLIState(t)
	diff := []byte("diff --git \"a/路径 with \\\"quotes\\\".bin\" \"b/路径 with \\\"quotes\\\".bin\"\nindex 0000000..1111111\nGIT binary patch\nliteral 3\nKcmZQzU|?Vg000000000\n")
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "token synthetic-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"state":"open"}`)
		case "/api/v1/repos/alice/project/pulls/12.diff":
			if r.URL.Query().Get("binary") != "true" {
				t.Errorf("binary query = %q, want true", r.URL.RawQuery)
			}
			w.Write(diff)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "diff", "12", "--repo", "alice/project"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := []byte(stdout.String()); string(got) != string(diff) {
		t.Fatalf("diff bytes = %q, want exact response %q", got, diff)
	}
	if !containsRequest(requests, "/api/v1/repos/alice/project/pulls/12.diff?binary=true") {
		t.Fatalf("requests = %v; expected binary diff endpoint", requests)
	}
}

func TestPRDiffBuiltCLIUsesTeaAndPreservesFixtureBytes(t *testing.T) {
	diff := []byte("diff --git \"a/文件 name \\\"quoted\\\".dat\" \"b/文件 name \\\"quoted\\\".dat\"\nindex 0000000..1111111\nGIT binary patch\nliteral 4\nKcmZQzU|?Vg000000000\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token synthetic-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"state":"open"}`)
		case "/api/v1/repos/alice/project/pulls/12.diff":
			if r.URL.Query().Get("binary") != "true" {
				t.Errorf("binary query = %q", r.URL.RawQuery)
			}
			w.Write(diff)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	binary := filepath.Join(t.TempDir(), "gh-tea")
	build := exec.Command("go", "build", "-o", binary, "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	process := exec.Command(binary, "--config", prTestConfig(t, server), "--use", "test", "pr", "diff", "12", "--repo", "alice/project")
	stdout, err := process.Output()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			t.Fatalf("CLI error = %v; stderr = %q", err, exitErr.Stderr)
		}
		t.Fatalf("CLI error = %v", err)
	}
	if string(stdout) != string(diff) {
		t.Fatalf("CLI diff = %q, want exact fixture bytes %q", stdout, diff)
	}
}

func TestPRDiffEmptyAndFailuresAreDistinct(t *testing.T) {
	for _, test := range []struct {
		name       string
		diffStatus int
		diffBody   string
		pullStatus int
		interrupt  bool
		wantErr    string
	}{
		{name: "empty diff", diffStatus: http.StatusOK},
		{name: "server failure", diffStatus: http.StatusForbidden, diffBody: "private PR", wantErr: "private PR"},
		{name: "ordinary issue", pullStatus: http.StatusNotFound, diffStatus: http.StatusOK, diffBody: "must not be returned", wantErr: "get pull request"},
		{name: "interrupted diff", interrupt: true, wantErr: "read pull request diff"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetPRCLIState(t)
			oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
			rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
			t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
			var diffRequested bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/repos/alice/project/pulls/12":
					if test.pullStatus != 0 {
						http.Error(w, "not a pull request", test.pullStatus)
					} else {
						fmt.Fprint(w, `{"number":12,"state":"open"}`)
					}
				case "/api/v1/repos/alice/project/pulls/12.diff":
					diffRequested = true
					if test.interrupt {
						w.Header().Set("Content-Length", "100")
						w.WriteHeader(http.StatusOK)
						fmt.Fprint(w, "partial diff")
						w.(http.Flusher).Flush()
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Errorf("hijack diff response: %v", err)
							return
						}
						conn.Close()
						return
					}
					w.WriteHeader(test.diffStatus)
					fmt.Fprint(w, test.diffBody)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			configPath := prTestConfig(t, server)
			var stdout strings.Builder
			rootCmd.SetOut(&stdout)
			rootCmd.SetArgs([]string{"--config", configPath, "pr", "diff", "12", "--repo", "alice/project"})
			err := rootCmd.Execute()
			if test.wantErr == "" {
				if err != nil || stdout.Len() != 0 {
					t.Fatalf("error = %v, stdout = %q; expected successful empty diff", err, stdout.String())
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) || stdout.Len() != 0 {
					t.Fatalf("error = %v, stdout = %q; expected failure with no output", err, stdout.String())
				}
				process := exec.Command(os.Args[0], "-test.run=^TestPRDiffFailureProcessHelper$")
				process.Env = append(os.Environ(), "GH_TEA_PR_DIFF_TEST_CONFIG="+configPath, "GH_TEA_PR_DIFF_TEST_NUMBER=12")
				processOutput, processErr := process.Output()
				if len(processOutput) != 0 {
					t.Fatalf("failed CLI process wrote to stdout: %s", processOutput)
				}
				exitErr, ok := processErr.(*exec.ExitError)
				if !ok || exitErr.ExitCode() == 0 || !strings.Contains(string(exitErr.Stderr), test.wantErr) {
					t.Fatalf("CLI process error = %v, stderr = %q; expected nonzero diagnostic containing %q", processErr, exitErr, test.wantErr)
				}
			}
			if got := diffRequested; got != (test.pullStatus == 0) {
				t.Fatalf("diff requested = %v, want %v", got, test.pullStatus == 0)
			}
		})
	}
}

func TestPRDiffRejectsInvalidNumbers(t *testing.T) {
	for _, number := range []string{"0", "-1", "9223372036854775808", "1.5"} {
		t.Run(number, func(t *testing.T) {
			resetPRCLIState(t)
			args := []string{"pr", "diff", number, "--repo", "alice/project"}
			if strings.HasPrefix(number, "-") {
				args = []string{"--repo", "alice/project", "pr", "diff", "--", number}
			}
			rootCmd.SetArgs(args)
			if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "positive int64") {
				t.Fatalf("error = %v; expected positive int64 validation", err)
			}
		})
	}
}

func TestPRDiffFailureProcessHelper(t *testing.T) {
	configPath := os.Getenv("GH_TEA_PR_DIFF_TEST_CONFIG")
	if configPath == "" {
		return
	}
	number := os.Getenv("GH_TEA_PR_DIFF_TEST_NUMBER")
	os.Args = []string{"gh-tea-test", "--config", configPath, "pr", "diff", number, "--repo", "alice/project"}
	Execute()
}
