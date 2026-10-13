package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"

	"github.com/InsulateJustf/gh-tea/pkg/tea"
)

const issueJSONFields = "number,title,state,url,createdAt,updatedAt,closedAt,author.login,labels[].name,assignees[].login,milestone.title,body"

var (
	issueListState, issueListAssignee, issueListAuthor, issueListMention, issueListSearch      string
	issueListLabels                                                                            []string
	issueListLimit                                                                             int
	issueListJSON                                                                              string
	issueListWeb                                                                               bool
	issueCreateTitle, issueCreateBody, issueCreateBodyFile, issueCreateMilestone               string
	issueCreateLabels, issueCreateAssignees                                                    []string
	issueViewComments, issueViewWeb                                                            bool
	issueViewJSON                                                                              string
	issueEditTitle, issueEditBody, issueEditBodyFile, issueEditMilestone                       string
	issueEditAddLabels, issueEditRemoveLabels, issueEditAddAssignees, issueEditRemoveAssignees []string
	issueCommentBody                                                                           string
)

func init() {
	rootCmd.AddCommand(issueCmd)
	issueCmd.AddCommand(issueListCmd, issueCreateCmd, issueViewCmd, issueEditCmd, issueCloseCmd, issueReopenCmd, issueCommentCmd, issueStatusCmd)
	for _, command := range []*cobra.Command{issueListCmd, issueCreateCmd, issueViewCmd, issueEditCmd, issueCloseCmd, issueReopenCmd, issueCommentCmd, issueStatusCmd} {
		command.SetFlagErrorFunc(issueFlagError)
	}

	issueListCmd.Flags().StringVar(&issueListState, "state", "open", "filter by issue state (open, closed, all)")
	issueListCmd.Flags().IntVar(&issueListLimit, "limit", 30, "maximum number of issues to fetch (0 means unlimited)")
	issueListCmd.Flags().StringArrayVar(&issueListLabels, "label", nil, "filter by label (repeatable)")
	issueListCmd.Flags().StringVar(&issueListAssignee, "assignee", "", "filter by assignee")
	issueListCmd.Flags().StringVar(&issueListAuthor, "author", "", "filter by author")
	issueListCmd.Flags().StringVar(&issueListMention, "mention", "", "filter by mentioned user")
	issueListCmd.Flags().StringVarP(&issueListSearch, "search", "S", "", "search issue title and body")
	issueListCmd.Flags().StringVar(&issueListJSON, "json", "", "output selected fields as JSON")
	issueListCmd.Flags().BoolVar(&issueListWeb, "web", false, "open the issue list in a browser")

	issueCreateCmd.Flags().StringVar(&issueCreateTitle, "title", "", "issue title")
	issueCreateCmd.Flags().StringVar(&issueCreateBody, "body", "", "issue body")
	issueCreateCmd.Flags().StringVarP(&issueCreateBodyFile, "body-file", "f", "", "read issue body from a file (use - for stdin)")
	issueCreateCmd.Flags().StringArrayVar(&issueCreateLabels, "label", nil, "add issue label (repeatable)")
	issueCreateCmd.Flags().StringArrayVar(&issueCreateAssignees, "assignee", nil, "assign issue to user (repeatable)")
	issueCreateCmd.Flags().StringVar(&issueCreateMilestone, "milestone", "", "set issue milestone by title")

	issueCreateCmd.Flags().StringVarP(&issueCreateBodyFile, "body-file-short-F", "F", "", "read issue body from a file (use - for stdin)")
	issueCreateCmd.Flags().Lookup("body-file-short-F").Hidden = true
	issueViewCmd.Flags().BoolVar(&issueViewComments, "comments", false, "show issue comments")
	issueViewCmd.Flags().BoolVar(&issueViewWeb, "web", false, "open issue in a browser")
	issueViewCmd.Flags().StringVar(&issueViewJSON, "json", "", "output selected fields as JSON")

	issueEditCmd.Flags().StringVar(&issueEditTitle, "title", "", "set issue title")
	issueEditCmd.Flags().StringVar(&issueEditBody, "body", "", "set issue body")
	issueEditCmd.Flags().StringVarP(&issueEditBodyFile, "body-file", "f", "", "read issue body from a file (use - for stdin)")
	issueEditCmd.Flags().StringVarP(&issueEditBodyFile, "body-file-short-F", "F", "", "read issue body from a file (use - for stdin)")
	issueEditCmd.Flags().Lookup("body-file-short-F").Hidden = true
	issueEditCmd.Flags().StringArrayVar(&issueEditAddLabels, "add-label", nil, "add label (repeatable)")
	issueEditCmd.Flags().StringArrayVar(&issueEditRemoveLabels, "remove-label", nil, "remove label (repeatable)")
	issueEditCmd.Flags().StringArrayVar(&issueEditAddAssignees, "add-assignee", nil, "add assignee (repeatable)")
	issueEditCmd.Flags().StringArrayVar(&issueEditRemoveAssignees, "remove-assignee", nil, "remove assignee (repeatable)")
	issueEditCmd.Flags().StringVar(&issueEditMilestone, "milestone", "", "set issue milestone by title")
	issueCommentCmd.Flags().StringVar(&issueCommentBody, "body", "", "comment body")
}

var (
	issueCmd        = &cobra.Command{Use: "issue", Short: "Work with issues", Args: cobra.NoArgs}
	issueListCmd    = &cobra.Command{Use: "list", Short: "List issues in a repository", Args: cobra.NoArgs, RunE: issueListRun}
	issueCreateCmd  = &cobra.Command{Use: "create", Short: "Create an issue", Args: cobra.NoArgs, RunE: issueCreateRun}
	issueViewCmd    = &cobra.Command{Use: "view <number>", Short: "View an issue", Args: cobra.ExactArgs(1), RunE: issueViewRun}
	issueEditCmd    = &cobra.Command{Use: "edit <number>", Short: "Edit an issue", Args: cobra.ExactArgs(1), RunE: issueEditRun}
	issueCloseCmd   = &cobra.Command{Use: "close <number>", Short: "Close an issue", Args: cobra.ExactArgs(1), RunE: issueStateRun(gitea.StateClosed)}
	issueReopenCmd  = &cobra.Command{Use: "reopen <number>", Short: "Reopen an issue", Args: cobra.ExactArgs(1), RunE: issueStateRun(gitea.StateOpen)}
	issueCommentCmd = &cobra.Command{Use: "comment <number>", Short: "Add a comment to an issue", Args: cobra.ExactArgs(1), RunE: issueCommentRun}
	issueStatusCmd  = &cobra.Command{Use: "status", Short: "Show issues assigned to, created by, or mentioning you", Args: cobra.NoArgs, RunE: issueStatusRun}
)

func issueFlagError(command *cobra.Command, err error) error {
	message := err.Error()
	if strings.HasPrefix(message, "unknown flag: ") {
		flag := strings.TrimPrefix(message, "unknown flag: ")
		return fmt.Errorf("%s is not supported by gh-tea", flag)
	}
	if strings.Contains(message, "unknown shorthand flag:") {
		parts := strings.Fields(message)
		return fmt.Errorf("%s is not supported by gh-tea", parts[len(parts)-1])
	}
	return err
}

func issueClient() (*gitea.Client, string, string, error) {
	item, owner, name, err := tea.Resolve(cfg, useFlag, repoFlag)
	if err != nil {
		return nil, "", "", err
	}
	issueHost = item.Host
	client, err := tea.Client(item)
	return client, owner, name, err
}

func issueListRun(cmd *cobra.Command, _ []string) error {
	client, owner, repo, err := issueClient()
	if err != nil {
		return err
	}
	state := gitea.StateType(issueListState)
	if state != gitea.StateOpen && state != gitea.StateClosed && state != gitea.StateAll {
		return fmt.Errorf("invalid state %q: choose open, closed, or all", issueListState)
	}
	if issueListLimit < 0 {
		return fmt.Errorf("limit must be zero or greater")
	}
	if issueListWeb {
		return issueBrowse(owner, repo, 0)
	}
	var jsonFields []string
	if issueListJSON != "" {
		jsonFields = strings.Split(issueListJSON, ",")
		if err := validateIssueJSONFields(jsonFields); err != nil {
			return err
		}
	}
	issues, err := issuePages(client, owner, repo, gitea.ListIssueOption{State: state, Labels: issueListLabels, AssignedBy: issueListAssignee, CreatedBy: issueListAuthor, MentionedBy: issueListMention, KeyWord: issueListSearch}, issueListLimit)
	if err != nil {
		return err
	}
	if issueListJSON != "" {
		return writeIssueJSON(cmd, issues, jsonFields, false)
	}
	for _, item := range issues {
		fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\n", item.Index, item.State, item.Title)
	}
	return nil
}

func issuePages(client *gitea.Client, owner, repo string, base gitea.ListIssueOption, limit int) ([]*gitea.Issue, error) {
	const pageSize = 50
	var issues []*gitea.Issue
	for page := 1; ; {
		opt := base
		opt.Type = gitea.IssueTypeIssue
		opt.ListOptions = gitea.ListOptions{Page: page, PageSize: pageSize}
		items, response, err := client.ListRepoIssues(owner, repo, opt)
		if err != nil {
			return nil, err
		}
		issues = append(issues, items...)
		if limit > 0 && len(issues) >= limit {
			issues = issues[:limit]
			break
		}
		if len(items) == 0 || (response != nil && response.NextPage == 0 && len(items) < pageSize) {
			break
		}
		if response != nil && response.NextPage > 0 {
			page = response.NextPage
		} else {
			page++
		}
	}
	return issues, nil
}

func issueCreateRun(cmd *cobra.Command, _ []string) error {
	client, owner, repo, err := issueClient()
	if err != nil {
		return err
	}
	if issueCreateBodyFile != "" {
		body, err := readIssueBody(issueCreateBodyFile)
		if err != nil {
			return err
		}
		issueCreateBody = body
	}
	needsTitle := !cmd.Flags().Changed("title")
	needsBody := !cmd.Flags().Changed("body") && issueCreateBodyFile == ""
	tty := interactiveInput()
	if needsTitle && !tty {
		return errors.New("issue create requires --title or a TTY for prompts")
	}
	p := prompter.New(os.Stdin, os.Stdout, os.Stderr)
	if needsTitle {
		issueCreateTitle, err = p.Input("Title", "")
		if err != nil {
			return err
		}
	}
	if needsBody && tty {
		issueCreateBody, err = p.Input("Body", "")
		if err != nil {
			return err
		}
	}
	opt := gitea.CreateIssueOption{Title: issueCreateTitle, Body: issueCreateBody, Assignees: issueCreateAssignees}
	if len(issueCreateLabels) > 0 {
		opt.Labels, err = issueLabelIDs(client, owner, repo, issueCreateLabels)
		if err != nil {
			return err
		}
	}
	if issueCreateMilestone != "" {
		opt.Milestone, err = issueMilestoneID(client, owner, repo, issueCreateMilestone)
		if err != nil {
			return err
		}
	}
	created, _, err := client.CreateIssue(owner, repo, opt)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created issue #%d\n", created.Index)
	return nil
}

func issueViewRun(cmd *cobra.Command, args []string) error {
	client, owner, repo, err := issueClient()
	if err != nil {
		return err
	}
	index, err := issueNumber(args[0])
	if err != nil {
		return err
	}
	if issueViewWeb {
		return issueBrowse(owner, repo, index)
	}
	var jsonFields []string
	if issueViewJSON != "" {
		jsonFields = strings.Split(issueViewJSON, ",")
		if err := validateIssueJSONFields(jsonFields); err != nil {
			return err
		}
	}
	item, _, err := client.GetIssue(owner, repo, index)
	if err != nil {
		return err
	}
	if issueViewJSON != "" {
		return writeIssueJSON(cmd, []*gitea.Issue{item}, jsonFields, true)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n\n%s\n", item.Title, item.State, item.Body)
	if issueViewComments {
		comments, err := issueCommentPages(client, owner, repo, index)
		if err != nil {
			return err
		}
		for _, comment := range comments {
			author := ""
			if comment.Poster != nil {
				author = comment.Poster.UserName
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s: %s\n", author, comment.Body)
		}
	}
	return nil
}
func issueCommentPages(client *gitea.Client, owner, repo string, index int64) ([]*gitea.Comment, error) {
	const pageSize = 50
	var comments []*gitea.Comment
	for page := 1; ; {
		items, response, err := client.ListIssueComments(owner, repo, index, gitea.ListIssueCommentOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: pageSize},
		})
		if err != nil {
			return nil, err
		}
		comments = append(comments, items...)
		if len(items) == 0 || (response != nil && response.NextPage == 0 && len(items) < pageSize) {
			break
		}
		if response != nil && response.NextPage > 0 {
			page = response.NextPage
		} else {
			page++
		}
	}
	return comments, nil
}

func issueStateRun(state gitea.StateType) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, owner, repo, err := issueClient()
		if err != nil {
			return err
		}
		index, err := issueNumber(args[0])
		if err != nil {
			return err
		}
		_, _, err = client.EditIssue(owner, repo, index, gitea.EditIssueOption{State: &state})
		return err
	}
}

func issueEditRun(cmd *cobra.Command, args []string) error {
	client, owner, repo, err := issueClient()
	if err != nil {
		return err
	}
	index, err := issueNumber(args[0])
	if err != nil {
		return err
	}
	if issueEditBodyFile != "" {
		issueEditBody, err = readIssueBody(issueEditBodyFile)
		if err != nil {
			return err
		}
	}
	current, _, err := client.GetIssue(owner, repo, index)
	if err != nil {
		return err
	}
	var opt gitea.EditIssueOption
	if cmd.Flags().Changed("title") {
		opt.Title = issueEditTitle
	}
	if cmd.Flags().Changed("body") || issueEditBodyFile != "" {
		opt.Body = &issueEditBody
	}
	if issueEditMilestone != "" {
		id, err := issueMilestoneID(client, owner, repo, issueEditMilestone)
		if err != nil {
			return err
		}
		opt.Milestone = &id
	}
	updateIssue := cmd.Flags().Changed("title") || opt.Body != nil || opt.Milestone != nil ||
		len(issueEditAddAssignees) > 0 || len(issueEditRemoveAssignees) > 0
	if len(issueEditAddAssignees) > 0 || len(issueEditRemoveAssignees) > 0 {
		set := make(map[string]bool, len(current.Assignees)+len(issueEditAddAssignees))
		for _, user := range current.Assignees {
			set[user.UserName] = true
		}
		for _, user := range issueEditRemoveAssignees {
			delete(set, user)
		}
		for _, user := range issueEditAddAssignees {
			set[user] = true
		}
		opt.Assignees = sortedKeys(set)
	}
	if updateIssue {
		if _, _, err = client.EditIssue(owner, repo, index, opt); err != nil {
			return err
		}
	}
	if len(issueEditAddLabels) > 0 {
		ids, err := issueLabelIDs(client, owner, repo, issueEditAddLabels)
		if err != nil {
			return err
		}
		if _, _, err = client.AddIssueLabels(owner, repo, index, gitea.IssueLabelsOption{Labels: ids}); err != nil {
			return err
		}
	}
	if len(issueEditRemoveLabels) > 0 {
		labels, err := issueAttachedLabels(client, owner, repo, index)
		if err != nil {
			return err
		}
		remove := make(map[string]bool, len(issueEditRemoveLabels))
		for _, label := range issueEditRemoveLabels {
			remove[label] = true
		}
		for _, label := range labels {
			if remove[label.Name] {
				if _, err = client.DeleteIssueLabel(owner, repo, index, label.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func issueCommentRun(cmd *cobra.Command, args []string) error {
	client, owner, repo, err := issueClient()
	if err != nil {
		return err
	}
	index, err := issueNumber(args[0])
	if err != nil {
		return err
	}
	body := issueCommentBody
	if !cmd.Flags().Changed("body") {
		if !interactiveInput() {
			return errors.New("issue comment requires --body or a TTY for prompts")
		}
		body, err = prompter.New(os.Stdin, os.Stdout, os.Stderr).Input("Comment", "")
		if err != nil {
			return err
		}
	}
	_, _, err = client.CreateIssueComment(owner, repo, index, gitea.CreateIssueCommentOption{Body: body})
	return err
}

func issueStatusRun(cmd *cobra.Command, _ []string) error {
	client, owner, repo, err := issueClient()
	if err != nil {
		return err
	}
	viewer, _, err := client.GetMyUserInfo()
	if err != nil {
		return err
	}
	for _, section := range []struct {
		title  string
		filter func(string) gitea.ListIssueOption
	}{
		{"Assigned to you", func(user string) gitea.ListIssueOption {
			return gitea.ListIssueOption{State: gitea.StateOpen, AssignedBy: user}
		}},
		{"Created by you", func(user string) gitea.ListIssueOption {
			return gitea.ListIssueOption{State: gitea.StateOpen, CreatedBy: user}
		}},
		{"Mentioning you", func(user string) gitea.ListIssueOption {
			return gitea.ListIssueOption{State: gitea.StateOpen, MentionedBy: user}
		}},
	} {
		items, err := issuePages(client, owner, repo, section.filter(viewer.UserName), 30)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", section.title)
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\t%s\n", item.Index, item.State, item.Title)
		}
	}
	return nil
}

func issueBrowse(owner, repo string, number int64) error {
	base, err := url.Parse(issueHost)
	if err != nil {
		return err
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/issues"
	if number > 0 {
		base.Path += "/" + strconv.FormatInt(number, 10)
	}
	return browser.New("", os.Stdout, os.Stderr).Browse(base.String())
}

var issueHost string

func validateIssueJSONFields(fields []string) error {
	for _, field := range fields {
		switch field {
		case "number", "title", "state", "url", "createdAt", "updatedAt", "closedAt", "author.login", "labels[].name", "assignees[].login", "milestone.title", "body":
		default:
			return fmt.Errorf("unknown JSON field %q; available fields: %s", field, issueJSONFields)
		}
	}
	return nil
}

func writeIssueJSON(cmd *cobra.Command, issues []*gitea.Issue, selected []string, single bool) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	if single && len(issues) == 1 {
		return encoder.Encode(projectIssueFields(issues[0], selected))
	}
	rows := make([]map[string]any, 0, len(issues))
	for _, item := range issues {
		rows = append(rows, projectIssueFields(item, selected))
	}
	return encoder.Encode(rows)
}

func projectIssueFields(item *gitea.Issue, fields []string) map[string]any {
	projected := make(map[string]any, len(fields))
	for _, field := range fields {
		switch field {
		case "number":
			projected[field] = item.Index
		case "title":
			projected[field] = item.Title
		case "state":
			projected[field] = item.State
		case "url":
			projected[field] = item.HTMLURL
		case "createdAt":
			projected[field] = item.Created.Format(time.RFC3339)
		case "updatedAt":
			projected[field] = item.Updated.Format(time.RFC3339)
		case "closedAt":
			if item.Closed == nil {
				projected[field] = nil
			} else {
				projected[field] = item.Closed.Format(time.RFC3339)
			}
		case "author.login":
			if item.Poster == nil {
				projected["author"] = nil
			} else {
				projected["author"] = map[string]string{"login": item.Poster.UserName}
			}
		case "labels[].name":
			labels := make([]map[string]string, 0, len(item.Labels))
			for _, label := range item.Labels {
				labels = append(labels, map[string]string{"name": label.Name})
			}
			projected["labels"] = labels
		case "assignees[].login":
			assignees := make([]map[string]string, 0, len(item.Assignees))
			for _, assignee := range item.Assignees {
				assignees = append(assignees, map[string]string{"login": assignee.UserName})
			}
			projected["assignees"] = assignees
		case "milestone.title":
			if item.Milestone == nil {
				projected["milestone"] = nil
			} else {
				projected["milestone"] = map[string]string{"title": item.Milestone.Title}
			}
		case "body":
			projected[field] = item.Body
		}
	}
	return projected
}

func readIssueBody(path string) (string, error) {
	var reader io.Reader
	if path == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(filepath.Clean(path))
		if err != nil {
			return "", err
		}
		defer file.Close()
		reader = file
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func issueRepoLabels(client *gitea.Client, owner, repo string) ([]*gitea.Label, error) {
	const pageSize = 50
	var labels []*gitea.Label
	for page := 1; ; {
		items, response, err := client.ListRepoLabels(owner, repo, gitea.ListLabelsOptions{ListOptions: gitea.ListOptions{Page: page, PageSize: pageSize}})
		if err != nil {
			return nil, err
		}
		labels = append(labels, items...)
		if len(items) == 0 || (response != nil && response.NextPage == 0 && len(items) < pageSize) {
			break
		}
		if response != nil && response.NextPage > 0 {
			page = response.NextPage
		} else {
			page++
		}
	}
	return labels, nil
}

func issueAttachedLabels(client *gitea.Client, owner, repo string, index int64) ([]*gitea.Label, error) {
	const pageSize = 50
	var labels []*gitea.Label
	for page := 1; ; {
		items, response, err := client.GetIssueLabels(owner, repo, index, gitea.ListLabelsOptions{ListOptions: gitea.ListOptions{Page: page, PageSize: pageSize}})
		if err != nil {
			return nil, err
		}
		labels = append(labels, items...)
		if len(items) == 0 || (response != nil && response.NextPage == 0 && len(items) < pageSize) {
			break
		}
		if response != nil && response.NextPage > 0 {
			page = response.NextPage
		} else {
			page++
		}
	}
	return labels, nil
}

func issueLabelIDs(client *gitea.Client, owner, repo string, names []string) ([]int64, error) {
	labels, err := issueRepoLabels(client, owner, repo)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]int64, len(labels))
	for _, label := range labels {
		byName[label.Name] = label.ID
	}
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("no such label: %s", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func issueMilestoneID(client *gitea.Client, owner, repo, title string) (int64, error) {
	const pageSize = 50
	for page := 1; ; {
		items, response, err := client.ListRepoMilestones(owner, repo, gitea.ListMilestoneOption{
			ListOptions: gitea.ListOptions{Page: page, PageSize: pageSize},
			State:       gitea.StateAll,
		})
		if err != nil {
			return 0, err
		}
		for _, milestone := range items {
			if milestone.Title == title {
				return milestone.ID, nil
			}
		}
		if len(items) == 0 || (response != nil && response.NextPage == 0 && len(items) < pageSize) {
			break
		}
		if response != nil && response.NextPage > 0 {
			page = response.NextPage
		} else {
			page++
		}
	}
	return 0, fmt.Errorf("no such milestone: %s", title)
}

func issueNumber(raw string) (int64, error) {
	number, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || number < 1 {
		return 0, fmt.Errorf("invalid issue number %q", raw)
	}
	return number, nil
}

func interactiveInput() bool {
	return term.IsTerminal(os.Stdin)
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
