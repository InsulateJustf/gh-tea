# gh-tea

`gh tea` = gh for Gitea. A [`gh`](https://cli.github.com) CLI extension that lets you run gh-style commands against [Gitea](https://gitea.io) instances: `gh issue list` becomes `gh tea issue list`, `gh repo view` becomes `gh tea repo view`. Command names and flag names follow gh; the semantics are a practical subset (the gh-compat surface).

## Installation

1. Install the `gh` CLI — see the [installation instructions](https://github.com/cli/cli#installation) (minimum version 2.0.0, which supports extensions).

2. Install this extension:

   ```sh
   gh extension install InsulateJustf/gh-tea
   ```

   Or install manually from a clone:

   ```sh
   git clone https://github.com/InsulateJustf/gh-tea
   cd gh-tea
   go build -o gh-tea .
   gh extension install .
   ```

3. Upgrade later with:

   ```sh
   gh extension upgrade gh-tea
   ```

## Configuring

Configuration lives at `~/.config/gh-tea/config.yaml` (override with `--config`). Each Gitea host is a named **Tea** entry:

```yaml
tea:
  myhost:
    name: myhost
    host: http://10.39.40.30:8085
    client-id: <oauth app client id>   # optional, for OAuth login
    token: <access token>              # written by gh tea auth login
default: myhost
```

Manage entries with the host commands below. Tea selection for commands that use a host is explicit `--use <name>` first, then a configured Gitea host matching a Git remote in the current directory, then the configured `default`. If multiple configured Gitea hosts match the current directory's remotes, selection is ambiguous and you must pass `--use`. Remote matching uses configured Gitea hosts only; GitHub remotes are not mapped to a Gitea host. SSH remotes using bracketed IPv6 scp-style addresses (for example, `git@[2001:db8::1]:owner/repo.git`) are supported. The entry named in `default` is used when no explicit or matching host is available.

For authenticated repository operations, `--repo/-R <owner/name>` overrides repository inference. Repository inference uses only matching configured Gitea remotes; it does not map GitHub remotes. These operations require a repository and an access token for the selected Tea. Host-only authentication commands do not require a repository or token to select a Tea. Tokens are never printed by `list` or `status`.

## Commands

| Command | Description |
|---|---|
| `gh tea auth login` | Authenticate with a Gitea host (OAuth web flow, or `--token`) |
| `gh tea auth logout` | Remove authentication for a Gitea account |
| `gh tea auth refresh` | Verify the stored credential of the selected Tea |
| `gh tea auth status` | Show login state for every configured host |
| `gh tea auth token` | Print the access token of the selected Tea (for scripts) |
| `gh tea new <name> <host> [--client-id <id>]` | Register a Gitea host under a name |
| `gh tea list` | List registered hosts (like `git remote -v`) |
| `gh tea default [<name>]` | Show or set the default host |
| `gh tea remove <name>` | Remove a host from the list |
| `gh tea issue list` | List repository issues; filter with `--state`, repeatable `--label`, `--assignee`, `--author`, `--mention`, `--search/-S`, and `--limit` |
| `gh tea issue create` | Create an issue with `--title`, `--body`, `--body-file/-f`, `--label`, `--assignee`, or `--milestone`; prompts for missing title/body in a terminal |
| `gh tea issue view <number>` | View an issue; `--comments`, `--web`, and `--json <fields>` are supported |
| `gh tea issue edit <number>` | Edit issue title/body/milestone, add or remove labels, and add or remove assignees |
| `gh tea issue close <number>` | Close an issue |
| `gh tea issue reopen <number>` | Reopen an issue |
| `gh tea issue comment <number>` | Add a comment with `--body`, or prompt in a terminal |
| `gh tea issue status` | Show open issues assigned to, created by, or mentioning the authenticated user |
| `gh tea repo list [owner]` | List repositories; supports `--limit` and `--json` |
| `gh tea repo view [owner/name]` | View a repository; supports `--web` and `--json` |
| `gh tea repo create [name]` | Create a repository with `--private`, `--description`, `--org`, and optional `--clone`; prompts for name, visibility, and description when omitted interactively |
| `gh tea repo delete <owner/name>` | Delete after confirmation, or pass `--yes` |
| `gh tea repo edit <owner/name>` | Set `--description`, `--homepage`, `--private`, and/or `--archived` |
| `gh tea repo fork <owner/name>` | Fork within the selected Gitea instance; supports `--org` and `--clone` |
| `gh tea repo rename <new-name>` | Rename the current repository selected by `--repo` or the current Gitea remote |
| `gh tea repo clone <owner/name> [directory]` | Clone a repository with Git |
| `gh tea repo browse` | Open the current repository page in a browser |

Issue JSON fields: `number,title,state,url,createdAt,updatedAt,closedAt,author.login,labels[].name,assignees[].login,milestone.title,body`. Repository JSON fields: `name,nameWithOwner,description,visibility,isPrivate,url,updatedAt,pushedAt,defaultBranchRef.name`. Unsupported flags, including `--project` and `--template/-t`, are rejected rather than ignored. `--jq` and template rendering are not implemented.
`issue list --json` and `repo list --json` emit arrays; `issue view --json` and `repo view --json` emit one object. In a non-interactive run, `issue create` requires `--title`; if no body flag/file is supplied, the body is empty. `repo create` requires a name argument, and `repo delete` requires `--yes`.

Global flags: `--config/-c <file>` (config file), `--use <name>` (choose a Tea for this invocation), `--repo/-R <owner/name>` (override repository inference for repository operations), `--debug/-d` (debug logging).

## OAuth app registration

gh-tea ships with **no built-in OAuth application** (see [ADR 0002](docs/adr/0002-per-host-client-id.md)). For OAuth login against a Gitea host, register an app once:

1. On the Gitea instance, go to **Settings → Applications → Manage OAuth2 applications** (site admins: **Site Administration → Applications** for instance-wide apps).
2. Create a new OAuth2 application with redirect URI:

   ```
   http://localhost:1455/callback
   ```

   gh-tea completes the login flow on `localhost:1455`; if that port is occupied, free it before logging in.
3. Use the generated **Client ID** when authenticating with that host (or enter a token directly).
