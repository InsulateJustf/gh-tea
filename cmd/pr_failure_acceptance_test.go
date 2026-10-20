package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPRBuiltCLIRejectsIncompleteResponses(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gh-tea")
	if output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, resource := range []string{"pull request", "inline comments"} {
		for _, damage := range []string{"garbage", "second JSON value", "interrupted", "complete whitespace"} {
			t.Run(resource+"/"+damage, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body string
					var affected bool
					switch r.URL.Path {
					case "/api/v1/version":
						body = `{"version":"1.22.6"}`
					case "/api/v1/repos/alice/project/pulls/12":
						body = `{"number":12,"state":"open","body":"original body"}`
						affected = resource == "pull request"
					case "/api/v1/repos/alice/project/pulls/12/reviews":
						body = `[{"id":3}]`
					case "/api/v1/repos/alice/project/pulls/12/reviews/3/comments":
						body = `[{"id":7,"pull_request_review_id":3,"body":"original inline"}]`
						affected = resource == "inline comments"
					default:
						http.NotFound(w, r)
						return
					}
					if affected {
						switch damage {
						case "garbage":
							body += " trailing garbage"
						case "second JSON value":
							body += ` {"unexpected":true}`
						case "interrupted":
							w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
						case "complete whitespace":
							body += "\r\n\t "
						}
					}
					fmt.Fprint(w, body)
				}))
				defer server.Close()
				fields := "body"
				if resource == "inline comments" {
					fields = "reviewComments"
				}
				process := exec.Command(binary, "--config", prTestConfig(t, server), "pr", "view", "12", "--repo", "alice/project", "--json", fields)
				stdout, err := process.Output()
				if damage == "complete whitespace" {
					if err != nil {
						t.Fatal(err)
					}
					var result struct {
						Body           string
						ReviewComments []struct {
							ID, ReviewID int64
							Body         string
						}
					}
					if err := json.Unmarshal(stdout, &result); err != nil {
						t.Fatal(err)
					}
					if resource == "pull request" && result.Body != "original body" {
						t.Fatalf("body changed: %q", result.Body)
					}
					if resource == "inline comments" && (len(result.ReviewComments) != 1 || result.ReviewComments[0].ID != 7 || result.ReviewComments[0].ReviewID != 3 || result.ReviewComments[0].Body != "original inline") {
						t.Fatalf("inline comments changed: %#v", result.ReviewComments)
					}
					return
				}
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() == 0 || len(stdout) != 0 || len(exit.Stderr) == 0 {
					t.Fatalf("damaged response: error=%v stdout=%q; expected nonzero, diagnostic stderr, empty stdout", err, stdout)
				}
				if damage == "interrupted" && !strings.Contains(string(exit.Stderr), "unexpected EOF") {
					t.Fatalf("transport error lost: %s", exit.Stderr)
				}
			})
		}
	}
	for _, next := range []string{"oops", "", "0", "-1", "1", "999999999999999999999999"} {
		t.Run("list invalid next page/"+next, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/version":
					fmt.Fprint(w, `{"version":"1.22.6"}`)
				case "/api/v1/repos/alice/project/pulls":
					w.Header().Set("Link", `<https://unused.invalid/pulls?page=`+next+`>; rel="next"`)
					fmt.Fprint(w, `[{"number":12,"title":"first page","state":"open"}]`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			process := exec.CommandContext(ctx, binary, "--config", prTestConfig(t, server), "pr", "list", "--repo", "alice/project", "--json", "number,title")
			stdout, err := process.Output()
			if ctx.Err() != nil {
				t.Fatal("list did not reject a non-progressing next page")
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() == 0 || len(stdout) != 0 || !strings.Contains(string(exit.Stderr), "invalid next page") {
				t.Fatalf("invalid next page: error=%v stdout=%q; expected nonzero, pagination diagnostic, empty stdout", err, stdout)
			}
		})
	}
}
