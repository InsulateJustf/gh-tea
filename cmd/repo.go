package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/spf13/cobra"

	"github.com/InsulateJustf/gh-tea/pkg/config"
	"github.com/InsulateJustf/gh-tea/pkg/tea"
)

const repoJSONFields = "name,nameWithOwner,description,visibility,isPrivate,url,updatedAt,defaultBranchRef.name"

var (
	repoListJSON                          string
	repoListLimit                         int
	repoViewJSON                          string
	repoViewWeb                           bool
	repoCreatePrivate, repoCreateClone    bool
	repoCreateDescription, repoCreateOrg  string
	repoDeleteYes                         bool
	repoEditDescription, repoEditHomepage string
	repoEditPrivate, repoEditArchived     bool
	repoForkOrg                           string
	repoForkClone                         bool
)

var (
	repoBrowserOpen = func(pageURL string) error {
		return browser.New("", os.Stdout, os.Stderr).Browse(pageURL)
	}
	repoGitClone = func(cloneURL, dir string) error {
		args := []string{"clone", cloneURL}
		if dir != "" {
			args = append(args, dir)
		}
		command := exec.Command("git", args...)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		return command.Run()
	}
)

var (
	repoCmd       = &cobra.Command{Use: "repo", Short: "Work with repositories", Args: cobra.NoArgs}
	repoListCmd   = &cobra.Command{Use: "list [owner]", Short: "List repositories", Args: cobra.MaximumNArgs(1), RunE: repoListRun}
	repoViewCmd   = &cobra.Command{Use: "view [owner/name]", Short: "View a repository", Args: cobra.MaximumNArgs(1), RunE: repoViewRun}
	repoCreateCmd = &cobra.Command{Use: "create [name]", Short: "Create a repository", Args: cobra.MaximumNArgs(1), RunE: repoCreateRun}
	repoDeleteCmd = &cobra.Command{Use: "delete <owner/name>", Short: "Delete a repository", Args: cobra.ExactArgs(1), RunE: repoDeleteRun}
	repoEditCmd   = &cobra.Command{Use: "edit <owner/name>", Short: "Edit a repository", Args: cobra.ExactArgs(1), RunE: repoEditRun}
	repoForkCmd   = &cobra.Command{Use: "fork <owner/name>", Short: "Fork a repository", Args: cobra.ExactArgs(1), RunE: repoForkRun}
	repoRenameCmd = &cobra.Command{Use: "rename <new-name>", Short: "Rename the current repository", Args: cobra.ExactArgs(1), RunE: repoRenameRun}
	repoCloneCmd  = &cobra.Command{Use: "clone <owner/name> [directory]", Short: "Clone a repository", Args: cobra.RangeArgs(1, 2), RunE: repoCloneRun}
	repoBrowseCmd = &cobra.Command{Use: "browse", Short: "Open the current repository in a browser", Args: cobra.NoArgs, RunE: repoBrowseRun}
)

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoListCmd, repoViewCmd, repoCreateCmd, repoDeleteCmd, repoEditCmd, repoForkCmd, repoRenameCmd, repoCloneCmd, repoBrowseCmd)
	commands := []*cobra.Command{repoListCmd, repoViewCmd, repoCreateCmd, repoDeleteCmd, repoEditCmd, repoForkCmd, repoRenameCmd, repoCloneCmd, repoBrowseCmd}
	for _, command := range commands {
		command.SetFlagErrorFunc(issueFlagError)
	}
	repoListCmd.Flags().IntVar(&repoListLimit, "limit", 30, "maximum number of repositories to fetch (0 means unlimited)")
	repoListCmd.Flags().StringVar(&repoListJSON, "json", "", "output selected fields as JSON")
	repoViewCmd.Flags().BoolVar(&repoViewWeb, "web", false, "open repository in a browser")
	repoViewCmd.Flags().StringVar(&repoViewJSON, "json", "", "output selected fields as JSON")
	repoCreateCmd.Flags().BoolVar(&repoCreatePrivate, "private", false, "create a private repository")
	repoCreateCmd.Flags().StringVar(&repoCreateDescription, "description", "", "repository description")
	repoCreateCmd.Flags().BoolVar(&repoCreateClone, "clone", false, "clone the new repository")
	repoCreateCmd.Flags().StringVar(&repoCreateOrg, "org", "", "organization to create the repository in")
	repoDeleteCmd.Flags().BoolVar(&repoDeleteYes, "yes", false, "skip confirmation")
	repoEditCmd.Flags().StringVar(&repoEditDescription, "description", "", "set repository description")
	repoEditCmd.Flags().StringVar(&repoEditHomepage, "homepage", "", "set repository homepage")
	repoEditCmd.Flags().BoolVar(&repoEditPrivate, "private", false, "make repository private")
	repoEditCmd.Flags().BoolVar(&repoEditArchived, "archived", false, "archive repository")
	repoForkCmd.Flags().StringVar(&repoForkOrg, "org", "", "organization to fork into")
	repoForkCmd.Flags().BoolVar(&repoForkClone, "clone", false, "clone the fork")
}

func repoClient() (*gitea.Client, error) {
	item, err := repoResolveTea()
	if err != nil {
		return nil, err
	}
	if item.Token == "" {
		return nil, fmt.Errorf("run: gh tea auth login --use %s", item.Name)
	}
	return tea.Client(item)
}

func repoResolveTea() (item config.TeaItem, err error) {
	return tea.ResolveTea(cfg, useFlag)
}

func repoListRun(cmd *cobra.Command, args []string) error {
	if repoListLimit < 0 {
		return fmt.Errorf("limit must be zero or greater")
	}
	var fields []string
	if repoListJSON != "" {
		fields = strings.Split(repoListJSON, ",")
		if err := validateRepoJSONFields(fields); err != nil {
			return err
		}
	}
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner := ""
	if len(args) == 1 {
		owner = args[0]
	}
	items, err := repoPages(client, owner, repoListLimit)
	if err != nil {
		return err
	}
	if repoListJSON != "" {
		return writeRepoJSON(cmd, items, fields, false)
	}
	for _, item := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", item.FullName, item.Description)
	}
	return nil
}

func repoPages(client *gitea.Client, owner string, limit int) ([]*gitea.Repository, error) {
	const pageSize = 50
	var repos []*gitea.Repository
	for page := 1; ; {
		opt := gitea.ListReposOptions{ListOptions: gitea.ListOptions{Page: page, PageSize: pageSize}}
		var items []*gitea.Repository
		var response *gitea.Response
		var err error
		if owner == "" {
			items, response, err = client.ListMyRepos(opt)
		} else {
			items, response, err = client.ListUserRepos(owner, opt)
		}
		if err != nil {
			return nil, err
		}
		repos = append(repos, items...)
		if limit > 0 && len(repos) >= limit {
			repos = repos[:limit]
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
	return repos, nil
}

func repoViewRun(cmd *cobra.Command, args []string) error {
	var fields []string
	if repoViewJSON != "" {
		fields = strings.Split(repoViewJSON, ",")
		if err := validateRepoJSONFields(fields); err != nil {
			return err
		}
	}
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner, name, err := repoCoordinates(args)
	if err != nil {
		return err
	}
	item, _, err := client.GetRepo(owner, name)
	if err != nil {
		return err
	}

	if repoViewWeb {
		return browseRepo(item.HTMLURL)
	}
	if repoViewJSON != "" {
		return writeRepoJSON(cmd, []*gitea.Repository{item}, fields, true)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n%s\n\n%s\n", item.FullName, item.HTMLURL, item.Description)
	return nil
}

func repoCreateRun(cmd *cobra.Command, args []string) error {
	item, err := repoResolveTea()
	if err != nil {
		return err
	}
	if item.Token == "" {
		return fmt.Errorf("run: gh tea auth login --use %s", item.Name)
	}
	client, err := tea.Client(item)
	if err != nil {
		return err
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	description := repoCreateDescription
	private := repoCreatePrivate
	if !interactiveInput() && name == "" {
		return fmt.Errorf("repository name is required in non-interactive mode")
	}
	var p *prompter.Prompter
	if interactiveInput() {
		p = prompter.New(os.Stdin, os.Stdout, os.Stderr)
	}
	if name == "" {
		name, err = p.Input("Repository name", "")
		if err != nil {
			return err
		}
	}
	if p != nil && !cmd.Flags().Lookup("private").Changed {
		choice, promptErr := p.Select("Visibility", "Public", []string{"Public", "Private"})
		if promptErr != nil {
			return promptErr
		}
		private = choice == 1
	}
	if p != nil && !cmd.Flags().Lookup("description").Changed {
		description, err = p.Input("Description", "")
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("repository name cannot be empty")
	}
	opt := gitea.CreateRepoOption{Name: name, Private: private, Description: description}
	var created *gitea.Repository
	if repoCreateOrg != "" {
		created, _, err = client.CreateOrgRepo(repoCreateOrg, opt)
	} else {
		created, _, err = client.CreateRepo(opt)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created repository %s\n", created.FullName)
	if repoCreateClone {
		return cloneRepo(cmd, created.CloneURL, created.Name, "")
	}
	return nil
}

func repoDeleteRun(cmd *cobra.Command, args []string) error {
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner, name, err := splitRepoArg(args[0])
	if err != nil {
		return err
	}
	if !repoDeleteYes {
		if !interactiveInput() {
			return fmt.Errorf("delete requires --yes in non-interactive mode")
		}
		confirmed, err := prompter.New(os.Stdin, os.Stdout, os.Stderr).Confirm(fmt.Sprintf("Delete %s/%s?", owner, name), false)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	if _, err := client.DeleteRepo(owner, name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted repository %s/%s\n", owner, name)
	return nil
}

func repoEditRun(cmd *cobra.Command, args []string) error {
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner, name, err := splitRepoArg(args[0])
	if err != nil {
		return err
	}
	if !cmd.Flags().Lookup("description").Changed && !cmd.Flags().Lookup("homepage").Changed && !cmd.Flags().Lookup("private").Changed && !cmd.Flags().Lookup("archived").Changed {
		return fmt.Errorf("specify at least one repository property to edit")
	}
	opt := gitea.EditRepoOption{}
	if cmd.Flags().Lookup("description").Changed {
		opt.Description = &repoEditDescription
	}
	if cmd.Flags().Lookup("homepage").Changed {
		opt.Website = &repoEditHomepage
	}
	if cmd.Flags().Lookup("private").Changed {
		opt.Private = &repoEditPrivate
	}
	if cmd.Flags().Lookup("archived").Changed {
		opt.Archived = &repoEditArchived
	}
	updated, _, err := client.EditRepo(owner, name, opt)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated repository %s\n", updated.FullName)
	return nil
}

func repoForkRun(cmd *cobra.Command, args []string) error {
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner, name, err := splitRepoArg(args[0])
	if err != nil {
		return err
	}
	opt := gitea.CreateForkOption{}
	if repoForkOrg != "" {
		opt.Organization = &repoForkOrg
	}
	fork, _, err := client.CreateFork(owner, name, opt)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created fork %s\n", fork.FullName)
	if repoForkClone {
		return cloneRepo(cmd, fork.CloneURL, fork.Name, "")
	}
	return nil
}

func repoRenameRun(cmd *cobra.Command, args []string) error {
	client, err := repoClient()
	if err != nil {
		return err
	}
	_, owner, name, err := tea.Resolve(cfg, useFlag, repoFlag)
	if err != nil {
		return err
	}
	updated, _, err := client.EditRepo(owner, name, gitea.EditRepoOption{Name: &args[0]})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Renamed repository to %s\n", updated.FullName)
	return nil
}

func repoCloneRun(cmd *cobra.Command, args []string) error {
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner, name, err := splitRepoArg(args[0])
	if err != nil {
		return err
	}
	item, _, err := client.GetRepo(owner, name)
	if err != nil {
		return err
	}
	dir := ""
	if len(args) == 2 {
		dir = args[1]
	}
	return cloneRepo(cmd, item.CloneURL, item.Name, dir)
}

func cloneRepo(cmd *cobra.Command, cloneURL, name, dir string) error {
	if cloneURL == "" {
		return fmt.Errorf("repository clone URL is empty")
	}
	if err := repoGitClone(cloneURL, dir); err != nil {
		return err
	}
	nextDir := dir
	if nextDir == "" {
		nextDir = name
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nNext steps:\n  cd %s\n  gh tea repo view\n", nextDir)
	return nil
}

func repoBrowseRun(_ *cobra.Command, _ []string) error {
	client, err := repoClient()
	if err != nil {
		return err
	}
	owner, name, err := repoCoordinates(nil)
	if err != nil {
		return err
	}
	item, _, err := client.GetRepo(owner, name)
	if err != nil {
		return err
	}
	return browseRepo(item.HTMLURL)
}

func browseRepo(pageURL string) error { return repoBrowserOpen(pageURL) }

func repoCoordinates(args []string) (string, string, error) {
	if len(args) == 1 {
		return splitRepoArg(args[0])
	}
	_, owner, name, err := tea.Resolve(cfg, useFlag, repoFlag)
	return owner, name, err
}

func splitRepoArg(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository must be specified as <owner>/<name>")
	}
	return parts[0], parts[1], nil
}

func validateRepoJSONFields(fields []string) error {
	for _, field := range fields {
		switch field {
		case "pushedAt":
			return fmt.Errorf("JSON field %q is unsupported: Gitea does not expose a reliable repository push timestamp", field)
		case "name", "nameWithOwner", "description", "visibility", "isPrivate", "url", "updatedAt", "defaultBranchRef.name":
		default:
			return fmt.Errorf("unknown JSON field %q; available fields: %s", field, repoJSONFields)
		}
	}
	return nil
}

func writeRepoJSON(cmd *cobra.Command, repos []*gitea.Repository, fields []string, single bool) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	if single && len(repos) == 1 {
		return encoder.Encode(projectRepoFields(repos[0], fields))
	}
	rows := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		rows = append(rows, projectRepoFields(repo, fields))
	}
	return encoder.Encode(rows)
}

func projectRepoFields(repo *gitea.Repository, fields []string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		switch field {
		case "name":
			result[field] = repo.Name
		case "nameWithOwner":
			result[field] = repo.FullName
		case "description":
			result[field] = repo.Description
		case "visibility":
			if repo.Private {
				result[field] = "PRIVATE"
			} else if repo.Internal {
				result[field] = "INTERNAL"
			} else {
				result[field] = "PUBLIC"
			}
		case "isPrivate":
			result[field] = repo.Private
		case "url":
			result[field] = repo.HTMLURL
		case "updatedAt":
			result[field] = repo.Updated.Format(time.RFC3339)
		case "defaultBranchRef.name":
			if repo.DefaultBranch == "" {
				result["defaultBranchRef"] = nil
			} else {
				result["defaultBranchRef"] = map[string]string{"name": repo.DefaultBranch}
			}
		}
	}
	return result
}

