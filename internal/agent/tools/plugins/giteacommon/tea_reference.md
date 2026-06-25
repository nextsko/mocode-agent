# Tea CLI 完整参考手册

Tea 是 Gitea 的命令行工具，用于管理 Gitea 实例上的各种实体。版本: 0.12.0

## 全局选项

- `--debug, --vvv` - 启用调试模式
- `--help, -h` - 显示帮助
- `--version, -v` - 显示版本

## 通用选项

大多数命令支持以下选项：

- `--repo string, -r string` - 覆盖本地仓库路径或 Gitea 仓库 slug（可选）
- `--remote string, -R string` - 从远程发现 Gitea 登录（可选）
- `--login string, -l string` - 使用不同的 Gitea 登录（可选）
- `--output string, -o string` - 输出格式（simple, table, csv, tsv, yaml, json）
- `--page int, -p int` - 指定页码（默认: 1）
- `--limit int, --lm int` - 每页项目限制（默认: 30）

---

## ENTITIES - 实体管理

### issues - 列出、创建和更新 issues

**用法**: `tea issues [command [command options]] [<issue index>]`

**子命令**:

- `list, ls` - 列出仓库的 issues
- `create, c` - 创建 issue
- `edit, e` - 编辑一个或多个 issues
- `reopen, open` - 将一个或多个 issues 状态改为 'open'
- `close` - 将一个或多个 issues 状态改为 'closed'

**选项**:

- `--comments` - 是否显示评论（交互式运行时如果未提供会提示）
- `--fields string, -f string` - 逗号分隔的字段列表。可用值: index,state,kind,author,author-id,url,title,body,created,updated,deadline,assignees,milestone,labels,comments,owner,repo（默认: "index,title,state,author,milestone,labels,owner,repo"）
- `--state string` - 按状态过滤（all|open|closed）（默认: open）
- `--kind issues, -K issues` - 返回 issues、`pulls` 或 `all`（可用于对 PR 应用高级搜索过滤）（默认: issues）
- `--keyword string, -k string` - 按搜索字符串过滤
- `--labels string, -L string` - 逗号分隔的标签列表以匹配 issues
- `--milestones string, -m string` - 逗号分隔的里程碑列表以匹配 issues
- `--author string, -A string` - 按作者过滤
- `--assignee string, -a string` - 按指派人过滤
- `--mentions string, -M string` - 按提及过滤
- `--owner string, --org string` - 按所有者/组织过滤
- `--from string, -F string` - 按此日期之后的活跃度过滤
- `--until string, -u string` - 按此日期之前的活跃度过滤

**示例**:

```bash
tea issues --state open --limit 10
tea issues create --title "Bug fix" --body "Fix the issue"
tea issues 123 --comments
```

---

### pulls - 管理和检出 pull requests

**用法**: `tea pulls [command [command options]] [<pull index>]`

**子命令**:

- `list, ls` - 列出仓库的 pull requests
- `checkout, co` - 本地检出给定的 PR
- `clean` - 删除已关闭 PR 的本地和远程功能分支
- `create, c` - 创建 pull request
- `close` - 将一个或多个 pull requests 状态改为 'closed'
- `reopen, open` - 将一个或多个 pull requests 状态改为 'open'
- `review` - 交互式审查 pull request
- `approve, lgtm, a` - 批准 pull request
- `reject` - 请求对 pull request 进行更改
- `merge, m` - 合并 pull request

**选项**:

- `--comments` - 是否显示评论（交互式运行时如果未提供会提示）
- `--fields string, -f string` - 逗号分隔的字段列表。可用值: index,state,author,author-id,url,title,body,mergeable,base,base-commit,head,diff,patch,created,updated,deadline,assignees,milestone,labels,comments（默认: "index,title,state,author,milestone,updated,labels"）
- `--state string` - 按状态过滤（all|open|closed）（默认: open）

**示例**:

```bash
tea pulls --state open --limit 10
tea pulls checkout 123
tea pulls merge 123
```

---

### labels - 管理 issue 标签

**用法**: `tea labels [command [command options]]`

**子命令**:

- `list, ls` - 列出标签
- `create, c` - 创建标签
- `update` - 更新标签
- `delete, rm` - 删除标签

**选项**:

- `--save, -s` - 将所有标签保存为文件

**示例**:

```bash
tea labels list
tea labels create --name "bug" --color "ff0000"
```

---

### milestones - 列出和创建里程碑

**用法**: `tea milestones [command [command options]] [<milestone name>]`

**子命令**:

- `list, ls` - 列出仓库的里程碑
- `create, c` - 创建里程碑
- `close` - 将一个或多个里程碑状态改为 'closed'
- `delete, rm` - 删除里程碑
- `reopen, open` - 将一个或多个里程碑状态改为 'open'
- `issues, i` - 管理里程碑的 issue/pull

**选项**:

- `--fields string, -f string` - 逗号分隔的字段列表。可用值: title,state,items_open,items_closed,items,duedate,description,created,updated,closed,id（默认: "title,items,duedate"）
- `--state string` - 按里程碑状态过滤（all|open|closed）（默认: open）

**示例**:

```bash
tea milestones list --state open
tea milestones create --title "v1.0.0" --due "2024-12-31"
```

---

### releases - 管理发布

**用法**: `tea releases [command [command options]]`

**子命令**:

- `list, ls` - 列出发布
- `create, c` - 创建发布
- `delete, rm` - 删除一个或多个发布
- `edit, e` - 编辑一个或多个发布
- `assets, asset, a` - 管理发布资产

**示例**:

```bash
tea releases list
tea releases create --tag "v1.0.0" --name "Release 1.0.0"
```

---

### times - 操作仓库 issues & pulls 的跟踪时间

**用法**: `tea times [command [command options]] [username | #issue]`

**子命令**:

- `list, ls` - 列出 issues & pulls 上的跟踪时间
- `add, a` - 在 issue 上跟踪花费的时间
- `delete, rm` - 删除 issue 上的单个跟踪时间
- `reset` - 重置 issue 上的跟踪时间

**选项**:

- `--from string, -f string` - 仅显示此日期之后跟踪的时间
- `--until string, -u string` - 仅显示此日期之前跟踪的时间
- `--total, -t` - 在末尾打印总持续时间
- `--mine, -m` - 显示您在所有仓库中跟踪的所有时间（覆盖命令参数）
- `--fields string` - 逗号分隔的字段列表。可用值: id,created,repo,issue,user,duration

**示例**:

```bash
tea times list --mine
tea times add 123 --duration "2h"
```

---

### organizations - 列出、创建、删除组织

**用法**: `tea organizations [command [command options]] [<organization>]`

**子命令**:

- `list, ls` - 列出组织
- `create, c` - 创建组织
- `delete, rm` - 删除用户的组织

**示例**:

```bash
tea organizations list
tea organizations create --name "myorg"
```

---

### repos - 显示仓库详情

**用法**: `tea repos [command [command options]] [<repo owner>/<repo name>]`

**子命令**:

- `list, ls` - 列出您有权限访问的仓库
- `search, s` - 在 Gitea 实例上查找任何仓库
- `create, c` - 创建仓库
- `create-from-template, ct` - 基于现有模板创建仓库
- `fork, f` - Fork 现有仓库
- `migrate, m` - 迁移仓库
- `delete, rm` - 删除现有仓库

**选项**:

- `--watched, -w` - 列出您关注的仓库
- `--starred, -s` - 列出您加星的仓库
- `--fields string, -f string` - 逗号分隔的字段列表。可用值: description,forks,id,name,owner,stars,ssh,updated,url,permission,type（默认: "owner,name,type,ssh"）
- `--type string, -T string` - 按类型过滤: fork, mirror, source

**示例**:

```bash
tea repos list --starred
tea repos create --name "myrepo"
tea repos search "myproject"
```

---

### branches - 查看分支

**用法**: `tea branches [command [command options]] [<branch name>]`

**子命令**:

- `list, ls` - 列出仓库的分支
- `protect, P` - 保护分支
- `unprotect, U` - 取消保护分支

**选项**:

- `--comments` - 是否显示评论（交互式运行时如果未提供会提示）
- `--fields string, -f string` - 逗号分隔的字段列表。可用值: name,protected,user-can-merge,user-can-push,protection（默认: "name,protected,user-can-merge,user-can-push"）

**示例**:

```bash
tea branches list
tea branches protect main
```

---

### actions - 管理仓库 actions

**用法**: `tea actions [command [command options]]`

**子命令**:

- `secrets, secret` - 管理仓库 action secrets
- `variables, variable, vars, var` - 管理仓库 action variables
- `runs, run` - 管理工作流运行
- `workflows, workflow` - 管理仓库工作流

**选项**:

- `--repo string` - 要操作的仓库
- `--login string` - 要使用的 gitea 登录实例
- `--output string, -o string` - 输出格式 [table, csv, simple, tsv, yaml, json]

**示例**:

```bash
tea actions secrets list
tea actions runs list
```

---

### webhooks - 管理 webhooks

**用法**: `tea webhooks [command [command options]]`

**子命令**:

- `list, ls` - 列出 webhooks
- `create, c` - 创建 webhook
- `update` - 更新 webhook
- `delete, rm` - 删除 webhook

**示例**:

```bash
tea webhooks list
tea webhooks create --url "https://example.com/hook"
```

---

### comment - 向 issue/pr 添加评论

**用法**: `tea comment [options] <issue / pr index> [<comment body>]`

**选项**:

- `--repo string, -r string` - 覆盖本地仓库路径或 Gitea 仓库 slug（可选）
- `--remote string, -R string` - 从远程发现 Gitea 登录（可选）
- `--login string, -l string` - 使用不同的 Gitea 登录（可选）
- `--output string, -o string` - 输出格式（simple, table, csv, tsv, yaml, json）

**示例**:

```bash
tea comment 123 "This looks good to me"
```

---

## HELPERS - 辅助工具

### open - 在浏览器中打开仓库

**用法**: `tea open [options]`

**选项**:

- `--login string, -l string` - 使用不同的 Gitea 登录（可选）
- `--repo string, -r string` - 覆盖本地仓库路径或 Gitea 仓库 slug（可选）
- `--remote string, -R string` - 从远程发现 Gitea 登录（可选）

**示例**:

```bash
tea open
tea open --repo gitea/tea
```

---

### notifications - 显示通知

**用法**: `tea notifications [command [command options]]`

**子命令**:

- `ls, list` - 列出通知
- `read, r` - 将所有过滤的或特定通知标记为已读
- `unread, u` - 将所有过滤的或特定通知标记为未读
- `pin, p` - 将所有过滤的或特定通知标记为固定
- `unpin` - 取消固定所有固定的或特定通知

**选项**:

- `--fields string, -f string` - 逗号分隔的字段列表。可用值: id,status,updated,index,type,state,title,repository（默认: "id,status,index,type,state,title"）
- `--types string, -t string` - 逗号分隔的主题类型列表以过滤。可用值: issue,pull,repository,commit
- `--states string, -s string` - 逗号分隔的通知状态列表以过滤。可用值: pinned,unread,read（默认: "unread,pinned"）
- `--mine, -m` - 显示所有仓库的通知而不是仅当前仓库

**示例**:

```bash
tea notifications list --mine
tea notifications read 123
```

---

### clone - 本地克隆仓库

**用法**: `tea clone [options] <repo-slug> [target dir]`

**选项**:

- `--depth int, -d int` - 获取的提交数，默认为全部（默认: 0）
- `--login string, -l string` - 使用不同的 Gitea 登录（可选）

**说明**: 仓库 slug 可以以不同格式指定:

- `gitea/tea`
- `tea`
- `gitea.com/gitea/tea`
- `git@gitea.com:gitea/tea`
- `https://gitea.com/gitea/tea`
- `ssh://gitea.com:22/gitea/tea`

当 repo-slug 中指定了主机时，它将覆盖用 `--login` 指定的登录。

**示例**:

```bash
tea clone gitea/tea
tea clone gitea/tea my-tea-dir
```

---

## MISCELLANEOUS - 杂项

### whoami - 显示当前登录用户

**用法**: `tea whoami [options]`

**说明**: 用于调试目的，显示当前登录的用户。

**示例**:

```bash
tea whoami
```

---

### admin - 需要 Gitea 实例管理员访问权限的操作

**用法**: `tea admin [command [command options]]`

**子命令**:

- `user, u` - 管理注册用户

**示例**:

```bash
tea admin user list
```

---

## SETUP - 设置

### logins - 登录到 Gitea 服务器

**用法**: `tea logins [command [command options]] [<login name>]`

**子命令**:

- `list, ls` - 列出 Gitea 登录
- `add` - 添加 Gitea 登录
- `edit, e` - 编辑 Gitea 登录
- `delete, rm` - 移除 Gitea 登录
- `default` - 获取或设置默认登录
- `oauth-refresh` - 刷新 OAuth 令牌

**示例**:

```bash
tea logins list
tea logins add --name mylogin --url https://gitea.com
```

---

### logout - 从 Gitea 服务器登出

**用法**: `tea logout [options] <login name>`

**示例**:

```bash
tea logout mylogin
```

---

## 常用场景示例

### 查看和创建 issues

```bash
# 列出开放的 issues
tea issues --state open --limit 20

# 创建新 issue
tea issues create --title "Bug fix" --body "Fix the issue"

# 查看特定 issue
tea issues 123 --comments
```

### 管理 pull requests

```bash
# 列出开放的 PRs
tea pulls --state open --limit 20

# 检出 PR
tea pulls checkout 123

# 合并 PR
tea pulls merge 123
```

### 查看通知

```bash
# 查看所有仓库的通知
tea notifications list --mine

# 标记通知为已读
tea notifications read 123
```

### 仓库操作

```bash
# 列出加星的仓库
tea repos list --starred

# 创建新仓库
tea repos create --name myrepo

# 克隆仓库
tea clone gitea/tea
```

### 分支管理

```bash
# 列出分支
tea branches list

# 保护主分支
tea branches protect main
```

---

## 注意事项

1. **上下文感知**: tea 会尝试使用 `$PWD` 中提供的仓库上下文（如果可用）
2. **最佳工作流**: tea 在 upstream/fork 工作流中效果最好，当本地 main 分支跟踪 upstream 仓库时
3. **Git 状态**: tea 假定本地 git 状态在执行 tea 操作之前已发布到远程
4. **配置**: 配置持久化在 `$XDG_CONFIG_HOME/tea`
5. **认证**: 大多数命令需要先使用 `tea logins add` 登录
