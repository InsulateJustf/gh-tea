package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/InsulateJustf/gh-tea/pkg/tea"
)

const prListJSONFields = "number,title,state,isDraft,url,createdAt,updatedAt,author.login,labels[].name,baseRefName,headRefName"

var (
	prListState  string
	prListAuthor string
	prListLabels []string
	prListLimit  int
	prListJSON   string
)

func init() {
	rootCmd.AddCommand(prCmd)
	prCmd.AddCommand(prListCmd)
	prListCmd.SetFlagErrorFunc(issueFlagError)
	prListCmd.Flags().StringVar(&prListState, "state", "open", "filter by pull request state (open, closed, merged, all)")
	prListCmd.Flags().IntVar(&prListLimit, "limit", 30, "maximum number of pull requests to list")
	prListCmd.Flags().StringVar(&prListAuthor, "author", "", "filter by author")
	prListCmd.Flags().StringArrayVar(&prListLabels, "label", nil, "filter by all comma-separated labels")
	prListCmd.Flags().StringVar(&prListJSON, "json", "", "output selected fields as JSON")
}

var (
	prCmd     = &cobra.Command{Use: "pr", Short: "Work with pull requests", Args: cobra.NoArgs}
	prListCmd = &cobra.Command{Use: "list", Short: "List pull requests in a repository", Args: cobra.NoArgs, RunE: prListRun}
)

type prListItem struct {
	Number  int64      `json:"number"`
	Title   string     `json:"title"`
	State   string     `json:"state"`
	Merged  bool       `json:"merged"`
	Draft   *bool      `json:"draft"`
	URL     *string    `json:"html_url"`
	Created *time.Time `json:"created_at"`
	Updated *time.Time `json:"updated_at"`
	Author  *struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Base *struct {
		Ref *string `json:"ref"`
	} `json:"base"`
	Head *struct {
		Ref *string `json:"ref"`
	} `json:"head"`
}

func prListRun(cmd *cobra.Command, _ []string) error {
	if prListState != "open" && prListState != "closed" && prListState != "merged" && prListState != "all" {
		return fmt.Errorf("invalid state %q: choose open, closed, merged, or all", prListState)
	}
	if prListLimit <= 0 {
		return fmt.Errorf("limit must be a positive integer")
	}
	var fields []string
	if prListJSON != "" {
		fields = strings.Split(prListJSON, ",")
		if err := validatePRJSONFields(fields); err != nil {
			return err
		}
	}
	item, owner, repo, err := tea.Resolve(cfg, useFlag, repoFlag)
	if err != nil {
		return err
	}
	client := &http.Client{}
	endpoint, err := url.JoinPath(item.Host, "api/v1/repos", url.PathEscape(owner), url.PathEscape(repo), "pulls")
	if err != nil {
		return err
	}
	author := prListAuthor
	if author == "@me" {
		var viewer struct {
			Login string `json:"login"`
		}
		if err := prGetJSON(client, item.Token, item.Host+"/api/v1/user", &viewer); err != nil {
			return fmt.Errorf("get authenticated user: %w", err)
		}
		author = viewer.Login
	}
	labels := splitPRLabels(prListLabels)
	matched := make([]prListItem, 0, min(prListLimit, 50))
	for page := 1; ; {
		items, links, err := prListPage(client, item.Token, endpoint, page)
		if err != nil {
			return err
		}
		for _, pr := range items {
			state := prNormalizedState(&pr)
			if prListState != "all" && state != prListState {
				continue
			}
			if author != "" && (pr.Author == nil || pr.Author.Login != author) {
				continue
			}
			if !prHasAllLabels(&pr, labels) {
				continue
			}
			matched = append(matched, pr)
			if len(matched) == prListLimit {
				break
			}
		}
		if len(matched) == prListLimit {
			break
		}
		nextPage, hasNext := prNextPage(links)
		if hasNext {
			if nextPage <= 0 {
				break
			}
			page = nextPage
			continue
		}
		if len(items) < 50 {
			break
		}
		page++
	}
	if prListJSON != "" {
		rows := make([]map[string]any, 0, len(matched))
		for i := range matched {
			rows = append(rows, projectPRFields(&matched[i], fields))
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	}
	for _, pr := range matched {
		fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\n", pr.Number, prNormalizedState(&pr), pr.Title)
	}
	return nil
}

func prListPage(client *http.Client, token, endpoint string, page int) ([]prListItem, []string, error) {
	const pageSize = 50
	requestURL := endpoint + "?state=all&limit=" + strconv.Itoa(pageSize) + "&page=" + strconv.Itoa(page)
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Authorization", "token "+token)
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("list pull requests: %w", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		return nil, nil, fmt.Errorf("list pull requests: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("list pull requests: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var items []prListItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, nil, fmt.Errorf("decode pull requests: %w", err)
	}
	return items, response.Header.Values("Link"), nil
}

func prNextPage(headers []string) (int, bool) {
	for _, header := range headers {
		for _, link := range strings.Split(header, ",") {
			open, close := strings.IndexByte(link, '<'), strings.IndexByte(link, '>')
			if open < 0 || close <= open {
				continue
			}
			params := link[close+1:]
			if !strings.Contains(params, `rel="next"`) && !strings.Contains(params, "rel=next") {
				continue
			}
			target, err := url.Parse(link[open+1 : close])
			if err != nil {
				return 0, true
			}
			page, err := strconv.Atoi(target.Query().Get("page"))
			if err != nil {
				return 0, true
			}
			return page, true
		}
	}
	return 0, false
}

func prGetJSON(client *http.Client, token, endpoint string, target any) error {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "token "+token)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func prNormalizedState(pr *prListItem) string {
	if pr.Merged {
		return "merged"
	}
	if pr.State == "open" {
		return "open"
	}
	return "closed"
}

func splitPRLabels(values []string) []string {
	var labels []string
	for _, value := range values {
		for _, label := range strings.Split(value, ",") {
			if label = strings.TrimSpace(label); label != "" {
				labels = append(labels, label)
			}
		}
	}
	return labels
}

func prHasAllLabels(pr *prListItem, required []string) bool {
	for _, wanted := range required {
		found := false
		for _, actual := range pr.Labels {
			if actual.Name == wanted {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validatePRJSONFields(fields []string) error {
	for _, field := range fields {
		switch field {
		case "number", "title", "state", "isDraft", "url", "createdAt", "updatedAt", "author.login", "labels[].name", "baseRefName", "headRefName":
		default:
			return fmt.Errorf("unknown JSON field %q; available fields: %s", field, prListJSONFields)
		}
	}
	return nil
}

func projectPRFields(pr *prListItem, fields []string) map[string]any {
	projected := make(map[string]any, len(fields))
	for _, field := range fields {
		switch field {
		case "number":
			projected[field] = pr.Number
		case "title":
			projected[field] = pr.Title
		case "state":
			projected[field] = prNormalizedState(pr)
		case "isDraft":
			projected[field] = pr.Draft
		case "url":
			projected[field] = pr.URL
		case "createdAt":
			projected[field] = pr.Created
		case "updatedAt":
			projected[field] = pr.Updated
		case "author.login":
			if pr.Author == nil {
				projected["author"] = nil
			} else {
				projected["author"] = map[string]string{"login": pr.Author.Login}
			}
		case "labels[].name":
			labels := make([]map[string]string, 0, len(pr.Labels))
			for _, label := range pr.Labels {
				labels = append(labels, map[string]string{"name": label.Name})
			}
			projected["labels"] = labels
		case "baseRefName":
			if pr.Base == nil {
				projected[field] = nil
			} else {
				projected[field] = pr.Base.Ref
			}
		case "headRefName":
			if pr.Head == nil {
				projected[field] = nil
			} else {
				projected[field] = pr.Head.Ref
			}
		}
	}
	return projected
}
