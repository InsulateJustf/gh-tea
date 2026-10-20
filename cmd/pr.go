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
const prViewJSONFields = "number,title,state,isDraft,url,createdAt,updatedAt,closedAt,author.login,labels[].name,baseRefName,headRefName,body,merged,mergedAt,mergeable,files,comments,reviews,reviewComments"

var (
	prListState  string
	prListAuthor string
	prListLabels []string
	prListLimit  int
	prListJSON   string
	prViewJSON   string
)

func init() {
	rootCmd.AddCommand(prCmd)
	prListCmd.SetFlagErrorFunc(issueFlagError)
	prListCmd.Flags().StringVar(&prListState, "state", "open", "filter by pull request state (open, closed, merged, all)")
	prListCmd.Flags().IntVar(&prListLimit, "limit", 30, "maximum number of pull requests to list")
	prListCmd.Flags().StringVar(&prListAuthor, "author", "", "filter by author")
	prListCmd.Flags().StringArrayVar(&prListLabels, "label", nil, "filter by all comma-separated labels")
	prListCmd.Flags().StringVar(&prListJSON, "json", "", "output selected fields as JSON")
	prViewCmd.SetFlagErrorFunc(issueFlagError)
	prViewCmd.Flags().StringVar(&prViewJSON, "json", "", "output selected fields as JSON")
	prDiffCmd.SetFlagErrorFunc(issueFlagError)
	prCmd.AddCommand(prListCmd, prViewCmd, prDiffCmd)
}

var (
	prCmd     = &cobra.Command{Use: "pr", Short: "Work with pull requests", Args: cobra.NoArgs}
	prListCmd = &cobra.Command{Use: "list", Short: "List pull requests in a repository", Args: cobra.NoArgs, RunE: prListRun}
	prViewCmd = &cobra.Command{Use: "view <number>", Short: "View a pull request", Args: cobra.ExactArgs(1), RunE: prViewRun}
	prDiffCmd = &cobra.Command{Use: "diff <number>", Short: "Print the raw unified diff of a pull request", Args: cobra.ExactArgs(1), RunE: prDiffRun}
)

func prDiffRun(cmd *cobra.Command, args []string) error {
	index, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || index <= 0 {
		return fmt.Errorf("pull request number must be a positive int64")
	}
	teaItem, owner, repo, err := tea.Resolve(cfg, useFlag, repoFlag)
	if err != nil {
		return err
	}
	base, err := url.JoinPath(teaItem.Host, "api/v1/repos", url.PathEscape(owner), url.PathEscape(repo))
	if err != nil {
		return err
	}
	client := &http.Client{}
	pullRequestURL := base + "/pulls/" + strconv.FormatInt(index, 10)
	var pullRequest prViewItem
	if err := prGetJSON(client, teaItem.Token, pullRequestURL, &pullRequest); err != nil {
		return fmt.Errorf("get pull request #%d: %w", index, err)
	}
	if pullRequest.Number <= 0 {
		return fmt.Errorf("get pull request #%d: server returned invalid pull request number", index)
	}

	request, err := http.NewRequest(http.MethodGet, pullRequestURL+".diff?binary=true", nil)
	if err != nil {
		return fmt.Errorf("get pull request diff: %w", err)
	}
	request.Header.Set("Authorization", "token "+teaItem.Token)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("get pull request diff: %w", err)
	}
	diff, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read pull request diff: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("get pull request diff: %s: %s", response.Status, strings.TrimSpace(string(diff)))
	}
	n, err := cmd.OutOrStdout().Write(diff)
	if err != nil {
		return fmt.Errorf("write pull request diff: %w", err)
	}
	if n != len(diff) {
		return fmt.Errorf("write pull request diff: %w", io.ErrShortWrite)
	}
	return nil
}

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

type prViewItem struct {
	prListItem
	Closed    *time.Time `json:"closed_at"`
	MergedAt  *time.Time `json:"merged_at"`
	Body      string     `json:"body"`
	Mergeable *bool      `json:"mergeable"`
}

type prViewFile struct {
	Path         string  `json:"filename"`
	PreviousPath *string `json:"previous_filename"`
	Status       string  `json:"status"`
	Additions    int64   `json:"additions"`
	Deletions    int64   `json:"deletions"`
}

type prViewComment struct {
	ID     int64 `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"user"`
	Body    string     `json:"body"`
	Created *time.Time `json:"created_at"`
	Updated *time.Time `json:"updated_at"`
	URL     *string    `json:"html_url"`
}

type prViewReview struct {
	ID     int64 `json:"id"`
	Author *struct {
		Login string `json:"login"`
	} `json:"user"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	Submitted *time.Time `json:"submitted_at"`
	CommitOID *string    `json:"commit_id"`
	URL       *string    `json:"html_url"`
	Stale     bool       `json:"stale"`
	Official  bool       `json:"official"`
	Dismissed bool       `json:"dismissed"`
}

type prViewReviewComment struct {
	ID       int64 `json:"id"`
	ReviewID int64 `json:"pull_request_review_id"`
	Author   *struct {
		Login string `json:"login"`
	} `json:"user"`
	Body             string     `json:"body"`
	Created          *time.Time `json:"created_at"`
	Updated          *time.Time `json:"updated_at"`
	URL              *string    `json:"html_url"`
	Path             *string    `json:"path"`
	Position         *uint64    `json:"position"`
	OriginalPosition *uint64    `json:"original_position"`
}

func prViewRun(cmd *cobra.Command, args []string) error {
	index, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || index <= 0 {
		return fmt.Errorf("pull request number must be a positive int64")
	}
	var fields []string
	if prViewJSON == "" {
		fields = nil
	} else {
		fields = strings.Split(prViewJSON, ",")
		if err := validatePRViewJSONFields(fields); err != nil {
			return err
		}
	}
	teaItem, owner, repo, err := tea.Resolve(cfg, useFlag, repoFlag)
	if err != nil {
		return err
	}
	base, err := url.JoinPath(teaItem.Host, "api/v1/repos", url.PathEscape(owner), url.PathEscape(repo))
	if err != nil {
		return err
	}
	client := &http.Client{}
	endpoint := base + "/pulls/" + strconv.FormatInt(index, 10)
	var pr prViewItem
	if err := prGetJSON(client, teaItem.Token, endpoint, &pr); err != nil {
		return fmt.Errorf("get pull request #%d: %w", index, err)
	}
	if pr.Number <= 0 {
		return fmt.Errorf("get pull request #%d: server returned invalid pull request number", index)
	}
	if prViewJSON == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n\n%s\n", pr.Title, prNormalizedState(&prListItem{State: pr.State, Merged: pr.Merged}), pr.Body)
		return nil
	}
	output := projectPRViewFields(&pr, fields)
	if containsPRField(fields, "files") {
		files, err := prViewPages[prViewFile](client, teaItem.Token, base+"/pulls/"+strconv.FormatInt(index, 10)+"/files")
		if err != nil {
			return fmt.Errorf("get pull request files: %w", err)
		}
		rows := make([]map[string]any, 0, len(files))
		for _, file := range files {
			if file.Additions < 0 || file.Deletions < 0 {
				return fmt.Errorf("get pull request files: invalid file counts for %q", file.Path)
			}
			rows = append(rows, map[string]any{"path": file.Path, "previousPath": file.PreviousPath, "status": file.Status, "additions": file.Additions, "deletions": file.Deletions})
		}
		output["files"] = rows
	}
	if containsPRField(fields, "comments") {
		comments, err := prViewPages[prViewComment](client, teaItem.Token, base+"/issues/"+strconv.FormatInt(index, 10)+"/comments")
		if err != nil {
			return fmt.Errorf("get pull request comments: %w", err)
		}
		rows := make([]map[string]any, 0, len(comments))
		for _, comment := range comments {
			if comment.ID <= 0 {
				return fmt.Errorf("get pull request comments: invalid comment id %d", comment.ID)
			}
			rows = append(rows, map[string]any{"id": comment.ID, "author": prViewAuthor(comment.Author), "body": comment.Body, "createdAt": comment.Created, "updatedAt": comment.Updated, "url": comment.URL})
		}
		output["comments"] = rows
	}
	if containsPRField(fields, "reviews") || containsPRField(fields, "reviewComments") {
		reviews, err := prViewPages[prViewReview](client, teaItem.Token, base+"/pulls/"+strconv.FormatInt(index, 10)+"/reviews")
		if err != nil {
			return fmt.Errorf("get pull request reviews: %w", err)
		}
		if containsPRField(fields, "reviews") {
			rows := make([]map[string]any, 0, len(reviews))
			for _, review := range reviews {
				if review.ID <= 0 {
					return fmt.Errorf("get pull request reviews: invalid review id %d", review.ID)
				}
				rows = append(rows, map[string]any{"id": review.ID, "author": prViewAuthor(review.Author), "body": review.Body, "state": review.State, "submittedAt": review.Submitted, "commitOid": review.CommitOID, "url": review.URL, "stale": review.Stale, "official": review.Official, "dismissed": review.Dismissed})
			}
			output["reviews"] = rows
		}
		if containsPRField(fields, "reviewComments") {
			rows := make([]map[string]any, 0)
			for _, review := range reviews {
				if review.ID <= 0 {
					return fmt.Errorf("get pull request review comments: invalid review id %d", review.ID)
				}
				var comments []prViewReviewComment
				if err := prGetJSON(client, teaItem.Token, base+"/pulls/"+strconv.FormatInt(index, 10)+"/reviews/"+strconv.FormatInt(review.ID, 10)+"/comments", &comments); err != nil {
					return fmt.Errorf("get comments for review %d: %w", review.ID, err)
				}
				for _, comment := range comments {
					if comment.ID <= 0 || comment.ReviewID <= 0 {
						return fmt.Errorf("get comments for review %d: invalid comment metadata", review.ID)
					}
					rows = append(rows, map[string]any{"id": comment.ID, "reviewId": comment.ReviewID, "author": prViewAuthor(comment.Author), "body": comment.Body, "createdAt": comment.Created, "updatedAt": comment.Updated, "url": comment.URL, "path": comment.Path, "position": comment.Position, "originalPosition": comment.OriginalPosition})
				}
			}
			output["reviewComments"] = rows
		}
	}
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func validatePRViewJSONFields(fields []string) error {
	for _, field := range fields {
		switch field {
		case "number", "title", "state", "isDraft", "url", "createdAt", "updatedAt", "closedAt", "author.login", "labels[].name", "baseRefName", "headRefName", "body", "merged", "mergedAt", "mergeable", "files", "comments", "reviews", "reviewComments":
		default:
			return fmt.Errorf("unknown JSON field %q; available fields: %s", field, prViewJSONFields)
		}
	}
	return nil
}

func containsPRField(fields []string, target string) bool {
	for _, field := range fields {
		if field == target {
			return true
		}
	}
	return false
}

func prViewAuthor(author *struct {
	Login string `json:"login"`
}) any {
	if author == nil {
		return nil
	}
	return map[string]string{"login": author.Login}
}

func projectPRViewFields(pr *prViewItem, fields []string) map[string]any {
	out := projectPRFields(&pr.prListItem, fields)
	for _, field := range fields {
		switch field {
		case "closedAt":
			out[field] = pr.Closed
		case "body":
			out[field] = pr.Body
		case "merged":
			out[field] = pr.Merged
		case "mergedAt":
			out[field] = pr.MergedAt
		case "mergeable":
			out[field] = pr.Mergeable
		case "files", "comments", "reviews", "reviewComments":
			out[field] = []map[string]any{}
		}
	}
	return out
}

func prViewPages[T any](client *http.Client, token, endpoint string) ([]T, error) {
	const pageSize = 50
	var all []T
	for page := 1; ; {
		requestURL := endpoint + "?limit=" + strconv.Itoa(pageSize) + "&page=" + strconv.Itoa(page)
		request, err := http.NewRequest(http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "token "+token)
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(body)))
		}
		var items []T
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		all = append(all, items...)
		nextPage, hasNext := prNextPage(response.Header.Values("Link"))
		if hasNext {
			if nextPage <= page {
				return nil, fmt.Errorf("invalid next page %d after page %d", nextPage, page)
			}
			page = nextPage
			continue
		}
		if len(items) < pageSize {
			break
		}
		page++
	}
	return all, nil
}
