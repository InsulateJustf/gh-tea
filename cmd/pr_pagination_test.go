package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestPRViewCommentsReturnedAsCompleteCollection(t *testing.T) {
	resetPRCLIState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/version":
			fmt.Fprint(w, `{"version":"1.22.6"}`)
		case "/api/v1/repos/alice/project/pulls/12":
			fmt.Fprint(w, `{"number":12,"state":"open"}`)
		case "/api/v1/repos/alice/project/issues/12/comments":
			if r.URL.Query().Get("page") != "1" {
				http.Error(w, "unpaged comments must not be fetched again", 500)
				return
			}
			w.Header().Set("X-Total-Count", "101")
			comments := make([]map[string]any, 101)
			for i := range comments {
				comments[i] = map[string]any{"id": i + 1, "body": fmt.Sprintf("body-%d\n", i+1)}
			}
			json.NewEncoder(w).Encode(comments)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var stdout strings.Builder
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "comments"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Comments []struct {
			ID   int64
			Body string
		}
	}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Comments) != 101 || result.Comments[100].ID != 101 || result.Comments[100].Body != "body-101\n" {
		t.Fatalf("incomplete comments: %#v", result)
	}
}

func TestPRViewReviewsUseTotalCountAcrossFilteredOrClampedPages(t *testing.T) {
	for _, scenario := range []string{"filtered", "clamped", "later failure"} {
		t.Run(scenario, func(t *testing.T) {
			resetPRCLIState(t)
			oldSilenceUsage, oldSilenceErrors := rootCmd.SilenceUsage, rootCmd.SilenceErrors
			rootCmd.SilenceUsage, rootCmd.SilenceErrors = true, true
			t.Cleanup(func() { rootCmd.SilenceUsage, rootCmd.SilenceErrors = oldSilenceUsage, oldSilenceErrors })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/version":
					fmt.Fprint(w, `{"version":"1.22.6"}`)
				case "/api/v1/repos/alice/project/pulls/12":
					fmt.Fprint(w, `{"number":12,"state":"open"}`)
				case "/api/v1/repos/alice/project/pulls/12/reviews":
					page, _ := strconv.Atoi(r.URL.Query().Get("page"))
					w.Header().Set("X-Total-Count", "3")
					if scenario == "later failure" && page == 2 {
						http.Error(w, "hidden-page failure", 500)
						return
					}
					if scenario == "clamped" && page == 1 {
						fmt.Fprint(w, `[{"id":1,"body":"first"},{"id":2,"body":"second"}]`)
					} else if (scenario == "clamped" && page == 2) || (scenario != "clamped" && page == 3) {
						fmt.Fprint(w, `[{"id":3,"body":"last\n"}]`)
					} else {
						fmt.Fprint(w, `[]`)
					}
				case "/api/v1/repos/alice/project/pulls/12/reviews/3/comments":
					fmt.Fprint(w, `[{"id":7,"pull_request_review_id":3,"body":"last inline\n"}]`)
				default:
					if strings.HasSuffix(r.URL.Path, "/comments") {
						fmt.Fprint(w, `[]`)
					} else {
						http.NotFound(w, r)
					}
				}
			}))
			defer server.Close()
			var stdout strings.Builder
			rootCmd.SetOut(&stdout)
			rootCmd.SetArgs([]string{"--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", "reviews,reviewComments"})
			err := rootCmd.Execute()
			if scenario == "later failure" {
				if err == nil || !strings.Contains(err.Error(), "hidden-page failure") || stdout.Len() != 0 {
					t.Fatalf("error=%v stdout=%s", err, stdout.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Reviews []struct {
					ID   int64
					Body string
				}
				ReviewComments []struct{ Body string }
			}
			if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Reviews) == 0 || result.Reviews[len(result.Reviews)-1].ID != 3 || result.Reviews[len(result.Reviews)-1].Body != "last\n" || len(result.ReviewComments) != 1 || result.ReviewComments[0].Body != "last inline\n" {
				t.Fatalf("truncated discussions: %#v", result)
			}
		})
	}
}
