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

Manage entries with the host commands below. The entry named in `default` is used when a command does not select one; override per invocation with `--use <name>`. Tokens are never printed by `list` or `status`.

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

Global flags: `--config/-c <file>` (config file), `--use <name>` (choose a Tea for this invocation), `--debug/-d` (debug logging).

### Roadmap (Tier 1 / Tier 2, not yet implemented)

The target gh-compat surface, per [ADR 0002](docs/adr/0002-per-host-client-id.md):

- **Tier 1 — issue**: `gh tea issue list/create/view/edit/close/reopen/comment/status`
- **Tier 2 — repo**: `gh tea repo list/view/create/delete/edit/fork/rename/clone/browse`

## OAuth app registration

gh-tea ships with **no built-in OAuth application** (see [ADR 0002](docs/adr/0002-per-host-client-id.md)). For OAuth login against a Gitea host, register an app once:

1. On the Gitea instance, go to **Settings → Applications → Manage OAuth2 applications** (site admins: **Site Administration → Applications** for instance-wide apps).
2. Create a new OAuth2 application with redirect URI:

   ```
   http://localhost:1455/callback
   ```

   gh-tea completes the login flow on `localhost:1455`; if that port is occupied, free it before logging in.
3. Use the generated **Client ID** when authenticating with that host (or enter a token directly).
