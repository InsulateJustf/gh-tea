# ADR 0002: No built-in OAuth application; per-host ClientID

- Status: accepted
- Date: 2026-10-13

## Context

`gh tea auth login` performs an OAuth2 web flow against a Gitea host. The original code hardcoded an OAuth application ClientID for `gitea.com` in the binary. That has two problems:

- **Hardcoded credentials in a public binary**: anyone can extract the ClientID, and the secret-holder (the fork owner) cannot rotate or audit it.
- **Single-host assumption**: users point gh-tea at arbitrary Gitea instances (self-hosted, gitea.com); a ClientID is registered per Gitea instance, so one value cannot serve all hosts.

## Decision

- The binary contains **zero hardcoded credential values** — no OAuth ClientID for any host, including `gitea.com`.
- Each **Tea** (host entry) carries its own `client-id` in `~/.config/gh-tea/config.yaml`. Users register an OAuth2 application once per Gitea instance with redirect URI `http://localhost:1455/callback` (documented in the README), or skip OAuth entirely with a directly supplied token.
- OAuth endpoint path templates (`/login/oauth/authorize`, `/login/oauth/access_token`) remain in the binary; they are Gitea-spec paths, not credentials.

## Consequences

- Credentials stay user-controlled and rotatable; the binary is safe to distribute and fork.
- Cost: a one-time OAuth app registration per host before OAuth login works. `gitea.com` users are treated the same as self-hosted users — no special casing.
- `auth login` must fail with an actionable message when the selected Tea has no `client-id` and no token was supplied.
