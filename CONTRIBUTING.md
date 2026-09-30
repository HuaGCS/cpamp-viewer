# 参与开发

欢迎通过分支和 Pull Request 一起开发。部署和环境变量见 [README](README.md)，模块关系见 [架构说明](docs/ARCHITECTURE.md)。本文命令使用 Bash，适用于 Linux、macOS 或 WSL；在 Windows 上也可以使用 Docker Desktop 和 WSL。

## 先找对目录

| 目录 | 用途 |
| --- | --- |
| `server/` | Go 服务：认证、上游读取、公开 DTO、匿名标识、缓存和静态资源服务 |
| `web-cpamp/` | **当前生产前端**：React、TypeScript、Vite，以及保留的 CPAMP 共享组件 |
| `web-cpamp/src/viewer/` | Viewer 页面、路由、数据 Hook、API 客户端和适配层，新增 Viewer 功能通常从这里开始 |
| `tests/mock-cpamp/` | 使用合成数据的本地 CPAMP 替身 |
| `scripts/build-release.sh` | 完整检查、镜像构建、Windows 二进制和离线包生成 |
| `docs/UPSTREAM_BASELINE.md` | 上游基线、选择性移植功能和兼容边界 |

旧 `web/` 若存在于历史工作目录中，不参与当前 Docker 或发布构建。不要在那里修改界面。`server/webdist/` 是前端构建产物，不是源代码。

## 克隆与工具链

```bash
git clone https://github.com/jinshenganyuci/cpamp-viewer.git
cd cpamp-viewer
git switch -c feat/my-change
```

本机开发准备 Node.js 24、随 Node 安装的 npm、Go 1.26 和 Git。版本可用以下命令核对：

```bash
node --version
npm --version
go version
```

`server/go.mod` 声明的最低 Go 版本为 1.24；项目发布实际固定使用 Go 1.26。需要复现发布环境时，直接拉取与 [Dockerfile](Dockerfile) 相同的工具链镜像，不必在宿主机安装 Node 或 Go：

```bash
docker pull node:24-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd
docker pull golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
```

依赖以 `web-cpamp/package-lock.json` 为准，使用 `npm ci`。只有确实修改依赖时才更新 lockfile。Alpine 使用 musl，不要把在 Alpine 中安装的 `node_modules` 拿到 Linux glibc、macOS 或 Windows 上运行；换环境后在对应环境重新 `npm ci`。

## 从干净克隆完成首次构建

先构建前端，再检查 Go。`server/main.go` 使用 `//go:embed webdist`，未生成 `webdist` 时直接运行 Go 会报嵌入目录缺失。

本机工具链方式：

```bash
cd web-cpamp
npm ci --ignore-scripts --no-audit --no-fund
npm run type-check
npm run lint
npm test
VERSION="$(cat ../VERSION)" npm run build
cd ../server
go test -buildvcs=false -mod=readonly ./...
go vet ./...
go build -buildvcs=false -mod=readonly -o ../cpamp-viewer-dev .
cd ..
```

前端输出到 `server/webdist/`，Go 将其嵌入二进制。`VERSION` 显式取自版本文件，避免本地 Git tag 覆盖界面版本。Go 命令应在 `server/` 执行，仓库根目录不是 Go module。临时二进制不要提交。

也可在仓库根目录使用固定容器完成同样的前端检查和构建。下例把容器依赖放在临时文件系统，保留宿主机原有的 `node_modules`：

```bash
docker run --rm --platform linux/amd64 \
  --user "$(id -u):$(id -g)" \
  -e CI=1 -e npm_config_cache=/tmp/npm-cache \
  -e VERSION="$(cat VERSION)" \
  -v "$PWD/web-cpamp:/workspace/web-cpamp" \
  -v "$PWD/server:/workspace/server" \
  --tmpfs /workspace/web-cpamp/node_modules:rw,exec,mode=1777 \
  -w /workspace/web-cpamp \
  node:24-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd \
  sh -ceu 'npm ci --ignore-scripts --no-audit --no-fund; npm run type-check; npm run lint; npm test; npm run build'

docker run --rm --platform linux/amd64 \
  -v "$PWD/server:/src:ro" -w /src \
  golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 \
  sh -ceu 'go test -buildvcs=false -mod=readonly ./...; go vet ./...'
```

## 本地联调：Mock、Viewer、Vite

首次构建完成后，打开三个终端。下面只使用代码里公开的合成测试密钥，不需要真实 CPAMP 或 CPA 凭据。

终端一，在仓库根目录运行 Mock。Mock 程序内部固定监听 `:18318`，因此这里通过 Docker 只发布到本机回环地址：

```bash
docker run --rm --name cpamp-viewer-dev-mock \
  -p 127.0.0.1:18318:18318 \
  -v "$PWD/tests/mock-cpamp:/src:ro" -w /src \
  golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 \
  go run main.go
```

终端二，从仓库根目录启动本机 Go 服务：

```bash
cd server
HTTP_ADDR=127.0.0.1:18417 \
CPAMP_BASE_URL=http://127.0.0.1:18318 \
CPAMP_ADMIN_KEY=cpamp-test-admin-key \
VIEWER_PUBLIC_ACCESS=true \
go run .
```

`cpamp-test-admin-key` 仅用于 `tests/mock-cpamp`。此示例不配置会话签名密钥，服务会生成临时随机值；重启后旧分页游标失效，刷新页面即可。持久化部署应按 README 配置独立的签名密钥。

终端三，使用本机 Node.js 24 启动 Vite：

```bash
cd web-cpamp
npm ci --ignore-scripts --no-audit --no-fund
VERSION="$(cat ../VERSION)" npm run dev
```

开发页面：[http://127.0.0.1:5173/viewer/](http://127.0.0.1:5173/viewer/)。Vite 仅将 `/viewer/api` 代理到 `http://127.0.0.1:18417`，其余界面资源由 Vite 提供。`changeOrigin:false` 保留浏览器的 Host，与后端的同源检查一致；不要为开发放宽后端 Origin/CSRF 检查。改 Go 端口时同步修改 `vite.config.ts` 中的代理目标。

也可直接打开 [http://127.0.0.1:18417/management.html](http://127.0.0.1:18417/management.html)，查看最近一次构建的嵌入页面。这个地址没有前端热更新；修改前端后重新构建并重启 Go 才会更新。

基础连通性检查：

```bash
curl --fail http://127.0.0.1:18318/health
curl --fail http://127.0.0.1:18417/health
curl --fail http://127.0.0.1:18417/viewer/api/v1/session
```

Mock 用于基础页面和脱敏链路开发，没有实现全部 CPAMP v1.14.1 或 Access Guard 接口。用量状态、未定价模型、完整 Codex 查询等出现能力不可用或快照回退时，先看 Mock 是否提供对应接口。不要把 Mock 页面通过当成真实上游兼容性验收。结束后分别 `Ctrl+C`；Docker Mock 使用 `--rm` 自动移除。

## 修改功能的入口

| 想修改的内容 | 从这里查看 |
| --- | --- |
| 路由、导航 | `web-cpamp/src/viewer/ViewerApp.tsx`、`ViewerLayout.tsx` |
| 页面和布局 | `web-cpamp/src/viewer/pages/` |
| API 调用和类型 | `web-cpamp/src/viewer/api/client.ts`、`api/types.ts`、`viewerApi.ts` |
| 页面数据适配 | `web-cpamp/src/viewer/model/`、`hooks/`，包括另一份页面模型类型 `model/viewerTypes.ts` |
| 新只读接口、筛选与响应投影 | `server/internal/httpapi/` |
| 上游固定请求 | `server/internal/cpamp/client.go`、`server/internal/accessguard/client.go` |
| 配置与认证 | `server/internal/config/`、`server/internal/auth/` |
| 共享 CPAMP 展示组件 | `web-cpamp/src/features/`、`components/`；先确认实际由哪个 Viewer 页面调用 |
| 翻译 | `web-cpamp/src/i18n/locales/`；Viewer 独立文案还可能位于自身 model 模块 |

新增字段时，从上游响应、后端字段白名单、公开 DTO、前端类型、适配器到组件完整走一遍。只更新上游管理页面或上游 Hook，往往不会影响 Viewer，因为 Viewer 有自己的数据通路。

## 保持公开只读边界

- 浏览器仅调用固定的 `/viewer/api/v1/*`。不要添加可由访客传 URL、上游路径、凭据索引或任意 Header 的代理。
- CPAMP Admin Key、CPA Management Key 和认证 Token 留在服务端。不得把它们放进 `VITE_*` 变量、前端 bundle、日志、截图、测试报告或 PR。
- 原始 API Key、64 位哈希、auth index、session/parent-session ID、access-token hash、IP/UA/Trace 和认证正文不进入公开响应；身份关联使用服务端生成的 `view_*`。遮罩开关和 CSS 隐藏不构成授权。
- `POST /analytics` 是受约束的读取接口；HTTP 方法为 POST 不代表允许管理写操作。上游只读查询也可能使用 POST 封装，仍须固定操作并验证输入。
- Access Guard 的全部公开必须显式启用，空名单不扩大为全部。不能为方便演示而改变默认公开范围。
- 额度未知、过期、读取失败和零余额必须区分；完整 Codex/Spark 结果不能与旧窗口错误拼接。正常模型别名路由不应触发“模型不一致”。
- 引用共享组件时保留所需 store 的直接导入。不要重新接回管理端的认证 store、配置 Hook、写按钮或原始 API 客户端。

变更这些边界时，补上能阻止回归的测试，例如非法筛选/游标、敏感字段遗漏、`false` 与缺失值、未知额度、跨查询范围的旧数据，以及写路径拒绝。新增或修改上游能力时也更新 [基线说明](docs/UPSTREAM_BASELINE.md) 与 [来源说明](web-cpamp/UPSTREAM.md)，保留原作者许可证。

## 验证与 Pull Request

日常先运行受影响的测试，再执行上面的类型检查、Lint、全量前端测试、前端构建和 Go 检查。例如：

```bash
cd web-cpamp
npm test -- src/viewer/model/monitoring.test.ts
cd ../server
go test ./internal/httpapi -run 'Test.*Quota'
```

修改并发、缓存或请求取消逻辑时，使用带 C 编译器的 Go 环境额外运行 `go test -race ./...`。界面变更检查桌面、手机、深色模式和减少动态效果设置；涉及 API 的变更检查浏览器只请求 Viewer 接口。Vite 开发服务不能代替生产 bundle 的安全检查。

共同开发采用短分支：

```bash
git status --short
git add CONTRIBUTING.md
git diff --cached
git commit -m "docs: clarify contributor workflow"
git fetch origin
git rebase origin/main
git push -u origin HEAD
gh pr create --base main --title "说明本次改动" --body-file pr-description.md
```

按实际改动选择 `git add` 的文件，PR 描述文件也可以放在仓库外。没有主仓库写权限时，先 Fork，再将分支推到自己的 Fork 发 PR；不需要共享 GitHub Token。PR 写清问题、最终行为、测试结果和兼容边界；界面变化附合成数据截图。评审通过后合并，避免多人直接改 `main`。功能 PR 通常不修改发布版本。

## 完整发布构建

维护者从干净工作区运行：

```bash
./scripts/build-release.sh
```

此脚本需要 Docker、Buildx、Compose v2、Bash 和 GNU 命令行工具，建议 Linux/WSL。它校验版本一致性和 Compose，使用固定 Node/Go 镜像执行构建与测试，检查前端产物没有管理能力，生成 Linux amd64 镜像、Windows amd64 二进制、离线部署包、许可证及 SHA-256 校验文件。

脚本会**重新生成 `release/<VERSION>/` 并覆盖根目录兼容产物**，也会在 Alpine 容器内重新安装 `web-cpamp/node_modules`；随后继续本机前端开发时需重新 `npm ci`。脚本只制作本地产物，不推送 GitHub 或 Docker Hub，也不部署运行中的服务。`go vet`、race、真实 CPAMP 联调和浏览器验收需另外完成。

版本发布由维护者统一处理 `VERSION`、前端 package/lock、Dockerfile 与部署文档的一致性，并验证固定版和 `latest` 指向实际验收的同一镜像。提交源码时排除本地 `.env`、`secrets/`、数据库、`node_modules/`、`server/webdist/`、运行日志和发布二进制。
