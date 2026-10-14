# gh-tea

在终端里用熟悉的 `gh` 风格命令管理 **Gitea** 上的 issue 和仓库。

这是 [GitHub CLI (`gh`)](https://cli.github.com/) 的扩展：把 `gh issue list` 换成 `gh tea issue list`，目标就变成你配置的 Gitea 实例。它支持多个实例，但只实现 `gh` 的实用子集，**不是完整的 GitHub CLI 兼容层**。

## 安装

需要：

- [GitHub CLI](https://github.com/cli/cli#installation) 2.0.0 或更新版本（支持扩展）。
- Git：用于获取源码、识别当前仓库，以及执行克隆。
- Go 1.26.0 或更新版本：从源码构建时需要。
- 一个可以访问的 Gitea 实例，以及该实例的账号和相应操作权限。

### 从源码安装

```sh
git clone https://github.com/InsulateJustf/gh-tea.git
cd gh-tea
go build -o gh-tea .
gh extension install .
gh tea --help
```

不作为扩展安装也可以运行：`./gh-tea --help`。下文的 `gh tea` 均可替换为 `./gh-tea`。

源码安装的更新方式是在这个源码目录执行 `git pull --ff-only`，然后重新运行 `go build -o gh-tea .`。

### 发布版安装

仓库发布了适合你平台的预编译 release 后，可以直接安装，无需本地 Go：

```sh
gh extension install InsulateJustf/gh-tea
gh extension upgrade gh-tea
```

可用版本见 [Releases](https://github.com/InsulateJustf/gh-tea/releases)。没有预编译 release 时，使用上面的源码安装方式。

## 五分钟开始使用

以下使用 `work` 作为实例别名，`https://gitea.example.com` 作为实例地址，`alice/demo` 作为仓库。请替换成你的实际值。

### 1. 注册实例

```sh
gh tea new work https://gitea.example.com
```

第一个注册的实例自动成为默认实例。这里的别名叫 **Tea**，只是本地配置名称，不是 Gitea 用户名。

### 2. 登录

最直接的方式是使用 Gitea 个人访问令牌。在 Gitea 的 **Settings → Applications** 中创建具有所需权限的令牌，再运行：

```sh
gh tea auth login --use work --token '<你的访问令牌>'
gh tea auth status
```

登录会向 Gitea 验证令牌，成功后保存到本地配置。不要把真实令牌复制到共享脚本或日志中；命令行参数还可能进入 shell 历史或进程列表。

也可以使用浏览器 OAuth 登录，见 [OAuth 登录](#oauth-登录)。`gh auth login` 的 GitHub 登录信息不会替代这里的 Gitea 登录。

### 3. 查看并创建 issue

不在 Gitea 仓库目录中时，明确指定实例和仓库：

```sh
gh tea issue list --use work --repo alice/demo
gh tea issue create --use work --repo alice/demo \
  --title '修复安装说明' --body '请补充首次登录步骤。'
```

在该 Gitea 仓库的本地克隆目录中，通常可以省略选择参数：

```sh
cd demo
gh tea issue list
gh tea repo view
```

`gh-tea` 根据 Git remote 识别已配置的 Gitea 实例和仓库；不会把 GitHub remote 自动映射成 Gitea 仓库。

## 日常操作

### Issue

```sh
# 列表默认只看 open，最多 30 条；--limit 0 表示不限数量
gh tea issue list --state all --limit 100
gh tea issue list --label bug --label urgent --assignee alice
gh tea issue list --author alice --search '登录'

# 在终端中交互式输入标题和正文
gh tea issue create

# 从文件或标准输入读取正文；标签、负责人可重复传入
gh tea issue create --title '登录失败' --body-file report.md --label bug
printf '%s\n' '重现步骤：……' | gh tea issue create --title '登录失败' -f -

# 查看、评论、编辑和改变状态
gh tea issue view 12 --comments
gh tea issue view 12 --web
gh tea issue comment 12 --body '已确认，可以重现。'
gh tea issue edit 12 --title 'OAuth 登录失败' --add-label bug --add-assignee alice
gh tea issue edit 12 --remove-label bug --remove-assignee alice
gh tea issue close 12
gh tea issue reopen 12

# 当前仓库中，与登录用户有关的开放 issue：Assigned / Created / Mentioned
gh tea issue status
```

`issue create` 支持 `--milestone <标题>`；`issue edit` 也支持 `--body`、`--body-file` 和 `--milestone`。标签和里程碑按当前仓库中已有的名称查找，不会自动创建。

### 仓库

```sh
# 不提供 owner 时，列出登录用户可访问的仓库
gh tea repo list
gh tea repo list alice --limit 100
gh tea repo view alice/demo
gh tea repo view alice/demo --web

# 创建个人仓库或组织仓库，并可选克隆到本地
gh tea repo create demo --private --description '我的演示项目'
gh tea repo create demo --org my-team --private --description '团队项目' --clone

# 编辑；布尔属性可用 =false 取消
gh tea repo edit alice/demo --description '新的描述' --homepage https://example.com
gh tea repo edit alice/demo --private=false
gh tea repo edit alice/demo --archived=true
gh tea repo edit alice/demo --archived=false

# 在当前选中的 Gitea 实例内部 fork；不支持从 GitHub 跨实例迁移
gh tea repo fork alice/demo --clone

# 克隆、重命名和打开页面
gh tea repo clone alice/demo ./demo
gh tea repo rename demo-next --repo alice/demo
gh tea repo browse --repo alice/demo-next
```

`repo create` 在终端中会询问未提供的名称、可见性和描述。`repo fork` 支持 `--org <组织>`。重命名成功后，后续显式仓库参数应使用新名称；本地 Git remote 不会自动更新。

**删除仓库不可恢复，请先确认实例和仓库。** 交互运行会要求确认；自动化必须显式传入 `--yes`：

```sh
gh tea repo delete alice/demo-next --use work
# 确认要跳过提示时才使用：
gh tea repo delete alice/demo-next --use work --yes
```

克隆由 Git 执行，使用 Gitea 返回的 HTTP(S) 克隆地址。**Git 的认证需要单独配置**；`gh tea auth login` 不会把 API 令牌自动注入 Git。`repo create --clone` 和 `repo fork --clone` 同样如此。

## 多实例与仓库选择

```sh
gh tea new personal https://gitea.example.org
gh tea list
gh tea default work          # 设置默认实例
gh tea default               # 查看默认实例
gh tea auth login --use personal --token '<另一个实例的令牌>'
gh tea issue list --use personal --repo alice/demo
gh tea remove personal       # 删除本地实例配置，不删除服务器上的仓库
```

实例选择顺序：

1. `--use <别名>`。
2. 当前目录 Git remote 匹配的已配置 Gitea 实例。
3. 配置中的默认实例。

仓库选择使用 `--repo/-R <owner/name>`，否则从选中实例对应的 Git remote 推断。默认实例只决定服务器，**不会提供默认仓库**。

- 多个实例匹配 remote：需要 `--use` 消除歧义。
- 同一实例匹配多个不同仓库：需要 `--repo` 消除歧义。
- 不在 Gitea 克隆目录中，或只有未匹配的 remote：需要 `--repo`；尚未设置默认实例时还需要 `--use`。
- HTTPS、SSH URL 和 `git@host:owner/repo.git` 形式的 remote 均可识别，包括带方括号的 IPv6 SSH 地址。
- `auth` 和 `repo list/create` 等只需要实例的命令，不要求当前目录有仓库。

## OAuth 登录

没有内置的 OAuth 应用或 Client ID；每个 Gitea 实例都需要先注册应用。

1. 在 Gitea 打开 **Settings → Applications → Manage OAuth2 applications**。实例管理员也可在站点管理中注册应用。
2. 创建 OAuth2 应用，回调地址必须为：

   ```text
   http://localhost:1455/callback
   ```

3. 用生成的 Client ID 登录已注册的实例：

   ```sh
   gh tea auth login --use work --client-id '<Client ID>'
   ```

Client ID 会保存，下次只需 `gh tea auth login --use work`。也可以在首次 `gh tea new` 时通过 `--client-id` 保存它。

登录会打开浏览器，使用 PKCE，并在五分钟后超时。回调端口固定为 `1455`，不能自动换端口；被占用时需要先释放端口。浏览器未自动打开时，手动打开终端打印的授权 URL。

| 命令 | 行为 |
| --- | --- |
| `auth status` | 检查所有已配置实例的登录状态 |
| `auth refresh --use work` | 验证已保存的令牌；不是刷新或签发新令牌 |
| `auth logout --use work` | 删除本地令牌，保留实例地址和 Client ID；不撤销服务端令牌 |
| `auth token --use work` | 输出原始令牌，仅在确实需要的脚本中使用 |

## 配置与安全

默认配置位置：`~/.config/gh-tea/config.yaml`。所有命令都可用 `--config/-c <文件>` 指定其他配置：

```sh
gh tea --config ./local-config.yaml new work https://gitea.example.com
```

通常用命令管理即可，不需要手写 YAML。配置格式示例：

```yaml
tea:
  work:
    name: work
    host: https://gitea.example.com
    client-id: <可选的 OAuth Client ID>
    token: <登录后保存的访问令牌>
default: work
```

令牌以明文存储在本地 YAML 中，不使用系统钥匙串。配置采用临时文件加重命名的原子写入，新文件权限为仅当前用户读写。不要将配置提交到 Git 或分享给他人。`list` 和 `auth status` 不输出令牌；`auth token` 是明确的例外。配置解析失败会报错，不会静默清空配置。

## JSON 与自动化

```sh
gh tea issue list --use work --repo alice/demo --state all --json number,title,state
gh tea issue view 12 --use work --repo alice/demo --json number,title,body
gh tea repo list --use work --json nameWithOwner,visibility,url
gh tea repo view alice/demo --use work --json nameWithOwner,defaultBranchRef.name
```

`--json` 使用逗号分隔的字段名。`list` 输出数组，`view` 输出单个对象；未知字段会报错并列出可用字段。

| 对象 | 可选字段 |
| --- | --- |
| Issue | `number`, `title`, `state`, `url`, `createdAt`, `updatedAt`, `closedAt`, `author.login`, `labels[].name`, `assignees[].login`, `milestone.title`, `body` |
| 仓库 | `name`, `nameWithOwner`, `description`, `visibility`, `isPrivate`, `url`, `updatedAt`, `defaultBranchRef.name` |

仓库 JSON 不支持 `pushedAt`：Gitea 没有提供可靠的仓库最后推送时间，CLI 请求该字段时会报错。该时间不可用不代表仓库从未推送。CLI 不会以可见活动流或提交时间代替推送时间，也不会为仓库 JSON 查询活动接口。

非交互运行时：

- `issue create` 必须提供 `--title`；未提供正文参数或文件时正文为空。
- `issue comment` 必须提供 `--body`。
- `repo create` 必须提供名称；默认创建 public 仓库，创建私有仓库请加 `--private`。
- `repo delete` 必须提供 `--yes`。

## 支持范围与排错

支持 `auth`、实例管理、八个 `issue` 子命令和九个 `repo` 子命令。PR、release、迁移、镜像、gist 和通用 API 透传暂不支持。`--project`、`--template/-t`、`--jq` 等未实现的 flag 会报错，不会悄悄忽略。完整参数以 `gh tea <命令> --help` 为准。

全局参数：`--use`、`--repo/-R`、`--config/-c`、`--debug/-d`。

| 现象 | 处理方式 |
| --- | --- |
| `no tea configured` | 先用 `gh tea new <别名> <实例 URL>` 注册实例 |
| `no such tea name` | 用 `gh tea list` 检查别名；`--use` 接收别名，不是 URL |
| `specify --repo or run inside a Gitea repo clone` | 显式传入 `--repo owner/name`，或进入匹配的 Gitea 克隆目录 |
| `multiple teas/repositories match` | 分别用 `--use` / `--repo` 指定目标 |
| `run: gh tea auth login --use ...` 或令牌无效 | 对选中实例重新登录，检查令牌权限与服务端状态 |
| API 可用但克隆失败 | 检查 Git 自己的凭据和仓库克隆地址 |
| OAuth 回调端口不可用 | 释放本机 `1455` 端口后重新登录 |

## 开发与反馈

```sh
go build ./...
go vet ./...
go test ./...
```

问题反馈请提交到 [GitHub Issues](https://github.com/InsulateJustf/gh-tea/issues)，附上命令、错误信息和 Gitea 版本，先删除令牌等敏感信息。

许可证：[MIT](LICENSE)。
