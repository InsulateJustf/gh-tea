# Issue tracker：私有 fork 的 GitHub Issues

这个 repo 的 issues 和 specs 存放在 **私有 fork 仓库 `InsulateJustf/gh-tea`** 的 GitHub issues 中。所有操作都使用 `gh` CLI 并显式指定 `-R InsulateJustf/gh-tea`（本地 `origin` 指向上游 `hiifong/gh-tea`，不要依赖 remote 推断）。

## 约定

- **Create an issue**: `gh issue create -R InsulateJustf/gh-tea --title "..." --body "..."`。多行 body 使用 heredoc。
- **Read an issue**: `gh issue view <number> -R InsulateJustf/gh-tea --comments`，用 `jq` 过滤 comments，并同时获取 labels。
- **List issues**: `gh issue list -R InsulateJustf/gh-tea --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'`，按需加上 `--label` 和 `--state` filters。
- **Comment on an issue**: `gh issue comment <number> -R InsulateJustf/gh-tea --body "..."`
- **Apply / remove labels**: `gh issue edit <number> -R InsulateJustf/gh-tea --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> -R InsulateJustf/gh-tea --comment "..."`

## Pull requests 作为 triage surface

**PRs as a request surface: no.**

## 当 skill 说 "publish to the issue tracker" 时

在 `InsulateJustf/gh-tea` 创建一个 GitHub issue。

## 当 skill 说 "fetch the relevant ticket" 时

运行 `gh issue view <number> -R InsulateJustf/gh-tea --comments`。

## Wayfinding 操作

供 `/wayfinder` 使用。**map** 是单个 issue，以 **child** issues 作为 tickets。

- **Map**: 单个带 `wayfinder:map` label 的 issue，保存 Notes / Decisions-so-far / Fog body。
- **Child ticket**: 作为 GitHub sub-issue 链接到 map 的 issue（通过 sub-issues endpoint 使用 `gh api repos/InsulateJustf/gh-tea/issues/<n>/sub_issues`）。未启用 sub-issues 时，把 child 加入 map body 中的 task list，并在 child body 顶部写 `Part of #<map>`。Labels：`wayfinder:<type>`（`research`/`prototype`/`grilling`/`task`）。
- **Blocking**: GitHub 的 **native issue dependencies**。用 `gh api --method POST repos/InsulateJustf/gh-tea/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>` 添加 edge，其中 `<blocker-db-id>` 是 blocker 的数字 **database id**（`gh api repos/InsulateJustf/gh-tea/issues/<n> --jq .id`，_不是_ `#number` 或 `node_id`）。依赖不可用时，回退到 child body 顶部的 `Blocked by: #<n>, #<n>` 行。
- **Frontier query**: 列出 map 的 open children，丢弃任何带有 open blocker（`issue_dependencies_summary.blocked_by > 0`）或 assignee 的；按 map 顺序第一个胜出。
- **Claim**: `gh issue edit <n> -R InsulateJustf/gh-tea --add-assignee @me`。
- **Resolve**: `gh issue comment <n> -R InsulateJustf/gh-tea --body "<answer>"`，然后 close，再向 map 的 Decisions-so-far 追加 context pointer。
