---
name: selfhost
description: freespace8 自托管 fork 的维护流程：同步上游、selfhost 分支、构建镜像、部署到本机 Docker。用于"同步上游 / 同步代码"、"打包 / 构建镜像"、"部署 / 升级本地 vikunja"、"回滚"，以及新建 MCP token 后要更新各个 AI 客户端配置时。
---

# selfhost：fork 的同步、构建与部署

## 仓库与分支

- `origin` = `github.com/freespace8/vikunja`（fork），`upstream` = `github.com/go-vikunja/vikunja`。
- `main`：上游的镜像，只快进到 `upstream/main`，从不提交自己的代码。
- `selfhost`：`main` + 我们的改动。部署只从这里构建；新功能在 `.worktrees/<branch>` 里开发完后合并进 `selfhost`。本目录平时检出的就是 `selfhost`。
- 我们的改动以 `git log --oneline main..selfhost` 为准。目前的核心改动是 MCP 上传附件：MCP 工具 `task_attachments_upload_link` 生成一次性上传链接，再 `curl -F files=@...` 到 `POST /api/v2/mcp/uploads/{ticket}`。
- 每次部署都用 tag `deployed/YYYYMMDD-<sha>` 记下部署的是哪个提交，它是回滚和迁移比对的基准。首次部署（基于 v2.7.0）的 tag 是 `v2.7.0-mcp-upload`。

## 已知坑

- **提交被 secret-box 钩子拦截**（"could not check what this commit includes"）：钩子要能解析命令。用 `git -C <绝对路径> commit -m ...`，路径写字面量；不要用 shell 变量、`cd ... &&` 链或 heredoc；`add` 单独一条命令。
- **测试与 lint**：遵循仓库 `AGENTS.md`（只用 mage）。本机没装 `mage` 时用 `go run github.com/magefile/mage <target>`。`mage lint:fix` 若报 golangci-lint 不支持当前 Go 版本，用当前 Go 重新编译 golangci-lint v2 再跑 `golangci-lint run --fix`。
- **Docker Hub TLS 握手超时**：根因是 Surge 把 `docker.io` 走了直连。`~/Library/Application Support/Surge/Profiles/Default.conf` 里需要有 `DOMAIN-SUFFIX,docker.com` 和 `DOMAIN-SUFFIX,docker.io` 指向 "🚀 节点选择"。Surge 日志在 `~/Library/Logs/Surge*`，搜 `docker.io ... via DIRECT`。
- **镜像只在本机**：`vikunja-freespace8:*` 不在任何 registry。别 `docker image prune -a`，至少保留当前和上一个部署的镜像。

## 同步上游

1. `git fetch upstream --tags && git fetch origin`。
2. 确认 `main` 没有自己的提交：`git rev-list --count upstream/main..origin/main` 必须是 0；不是 0 就停下来问用户。
3. 快进 fork 的 main：`git push origin upstream/main:main`，本地 `git branch -f main upstream/main`（main 未检出时）。
4. 在 `selfhost` 上 `git merge main`。用合并，不用 rebase，`selfhost` 永远不强推。冲突按上游的新写法改我们的代码，并核对上游有没有改动我们依赖的东西（token 权限命名 `pkg/models/api_routes.go`、MCP 白名单 `pkg/modules/mcp/exposure.go`、上传 handler、`pkg/routes/routes.go`）。
5. 前端客户端若受影响：`mage generate:frontendClient` 后单独提交，`mage check:frontendClient` 通过。
6. 测试：先 `mage test:filter` 跑 `pkg/modules/mcp`、`pkg/routes/...`、`pkg/models`、`pkg/webtests` 里与 MCP、附件相关的用例，再跑完整 `mage test:web`，输出存到文件里读。
7. 推送：`git push origin selfhost`。

完成标准：`origin/main` == `upstream/main`，`selfhost` 包含 `main`，测试全绿，已推送。

## 构建镜像

**版本号必须和官方一致**，这样能从 `/api/v1/info` 和界面看出官方出了新版本。不传 `RELEASE_VERSION` 时，mage 会对 HEAD 执行 `git describe`，可能取到我们自己的 tag 或提交，所以必须显式传入。版本号取 `selfhost` 与上游的共同祖先提交，格式和官方构建相同（`-g` 换成 `-`）：

```bash
V=$(git describe --tags --abbrev=10 --match 'v[0-9]*' $(git merge-base HEAD upstream/main)); V=${V/-g/-}
docker build --build-arg RELEASE_VERSION=$V -t vikunja-freespace8:$V-selfhost.$(git rev-parse --short=9 HEAD) .
```

例：合并了上游 v2.8.0 正式版 → `v2.8.0`；跟着上游 main → `v2.7.0-235-2eb21632d3`。镜像标签后缀 `-selfhost.<sha>` 用来区分我们的提交，程序显示的版本号仍是纯官方的。在干净的 `selfhost` 检出上（`git status` 为空）构建。完整构建（前端加 xgo）需要几分钟，放后台跑，看日志末尾的 `naming to ... done`。

## 部署到本机

部署目录 `/Users/taylor/Documents/docker/vikunja`：`docker-compose.yml` 的 `image:` 决定版本，数据库是本机 `postgres` 容器里的 `vikunja` 库（账号 root），附件在 `./files`。

1. **查迁移**：`git diff --name-only <当前部署的 tag>..selfhost -- pkg/migration`。有新迁移时，启动会改正式库且无法自动回退。先列给用户看，用户确认后再继续。
2. **备份**：`docker exec postgres pg_dump -U root -d vikunja -Fc > backups/vikunja-db-<时间>.dump`，用 `docker exec -i postgres pg_restore -l < 该文件` 确认能读；`docker-compose.yml` 复制一份到 `backups/`。
3. **切换**：把 `image:` 改成新标签，并把上一个标签写进注释备查，然后 `docker compose -f /Users/taylor/Documents/docker/vikunja/docker-compose.yml up -d`。
4. **验证**：
   - `curl -s http://127.0.0.1:3456/api/v1/info` 的 `version` 是新版本。
   - `docker logs vikunja` 里有 `Ran all migrations successfully`，没有 error。
   - 用当前 MCP token 调 `/api/v2/mcp`：`tools/list` 里有 `task_attachments_upload_link`，生成链接后 `curl -F` 上传返回 201。
5. **记录**：在部署的提交上打 tag `deployed/YYYYMMDD-<sha>` 并推送。名字不以 `v` 开头，避免被 `git describe` 当成官方版本。

完成标准：新版本在跑，上传实测 201，备份和 tag 都在。

**回滚**：把 `image:` 改回上一个标签，再 `up -d`。如果这次部署跑过新迁移，旧版本可能不兼容新表结构，需要用备份恢复数据库（`pg_restore --clean`）。这一步会覆盖数据，必须先问用户。

## 换 MCP token

上传需要附件 Create 权限。在 Vikunja「设置 → MCP」新建 token 时选 FULL ACCESS（Typed 预设没有附件读取权限）。token 写在这四处，全部要换：

- Claude Code：`~/.claude.json` 顶层 `mcpServers.vikunja`（user scope）。用 `claude mcp remove -s user vikunja` 再 `claude mcp add-json -s user vikunja '<json>'`，不要手改这个文件。
- Codex：`~/.codex/config.toml` 的 `[mcp_servers.vikunja]` → `http_headers`。
- pi：`~/.pi/agent/mcp.json`；omp：`~/.omp/agent/mcp.json`（`headers.Authorization`）。

换之前先备份，换完用 `claude mcp get vikunja`、`codex mcp list`、`pi mcp list`、`omp mcp list` 确认都已连上。token 只经由文件或脚本传递，不要打印到输出里。旧 token 确认没人用后，到 Vikunja 里删掉。
