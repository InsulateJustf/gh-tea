# gh-tea

`gh-tea` is a `gh` CLI extension that lets GitHub CLI users operate on Gitea
instances by prefixing commands with `tea` (`gh issue` → `gh tea issue`).

## Language

**Tea**:
A named entry in the user's configuration that points at one Gitea host and
carries its credentials and OAuth app identity.
_Avoid_: account, profile, connection

**Host**:
The base URL of a Gitea instance, e.g. `https://gitea.com`.

**Default**:
The Tea used when a command does not name one.
_Avoid_: active tea, current tea

**Token**:
The stored access credential for a Tea, obtained via OAuth login or entered
directly.

**ClientID**:
The id of the OAuth app registered on a Tea's host that gh-tea uses for the
OAuth login flow. Always per-host, never built into the binary.
_Avoid_: app id (ambiguous with Gitea app installations)

**gh-compat surface**:
The set of gh commands and flags gh-tea reimplements with the same names and
practical semantics, as a subset (never a full clone) of gh.

**Migration**:
Copying a repository from another forge into Gitea, once.
_Avoid_: import

**Mirror**:
A Gitea repository that continuously re-syncs from its source forge.
_Avoid_: sync (that means a one-shot refresh of a Mirror)

**Gist repo**:
The dedicated private Gitea repository that receives migrated GitHub gists.
