# ADR 0001: Independent, self-held repository

- Status: accepted
- Date: 2026-10-13

## Context

`gh-tea` originated as `hiifong/gh-tea`: an early prototype (multi-host config
CRUD, a hardcoded-ClientID OAuth login, a placeholder TUI) with none of the
gh-compat surface implemented. The gh-compat v1 goal (`gh tea` = gh for Gitea,
tracked in this repository's milestone `gh-compat`) redefines the project's
scope: auth + issue + repo command surfaces, roughly 80% new code.

Two hosting options were considered: continue in a private fork of the
upstream repository, or start a fresh repository. A code review of the old
codebase concluded it was a prototype worth mining (config schema, host CRUD,
an OAuth callback skeleton) but not a foundation worth preserving as-is.

## Decision

This repository is **independent and self-held**:

- `InsulateJustf/gh-tea` is a **new repository with fresh history**, not a
  fork. There is no upstream remote and no merge relationship with
  `hiifong/gh-tea`.
- It is kept private, with its own issue tracker, triage labels, and the
  `gh-compat` milestone.
- Ported code (config/host-management layer, OAuth callback skeleton) carries
  the upstream MIT copyright headers, and the LICENSE credits both copyright
  holders.

## Consequences

- No upstream review overhead or scope negotiation; breaking changes (e.g. the
  new config schema with `default` as a field) are free.
- Losing upstream history means losing `git blame`/provenance for ported code;
  acceptable, since ported portions were re-reviewed on migration.
- If the project is ever published, the LICENSE already satisfies MIT
  attribution for the derived portions.
