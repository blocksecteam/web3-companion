# Web3 Companion 开发者指南

[English](DEVELOPER_GUIDE.md)

本指南面向希望构建、测试、扩展和发布 Web3 Companion 的开发者。

## 项目结构

Web3 Companion 是 Docker-first 的自托管发行版。仓库包含：

| 路径 | 用途 |
| --- | --- |
| `cmd/` | Go server 和 CLI 入口 |
| `internal/` | Go server 包 |
| `frontend/` | React 浏览器应用 |
| `companion/` | TypeScript companion runtime 和提示词 |
| `skills/` | companion agent 运行时加载的技能 |
| `.skills/` | 开发者扩展技能 |
| `tests/` | Go 单测和集成测试 |
| `docker/` | Docker 运行文件 |
| `scripts/` | 本地测试、发布和构建辅助脚本 |

SQLite 是唯一支持的数据库引擎。

## 本地开发环境

需要安装：

- Go 1.26+
- Bun
- Docker

启动服务：

```bash
cd docker
./start.sh
```

`start.sh` 会构建镜像、准备数据目录并启动 Docker Compose。它不需要 `docker/.env` 或 `docker/config.toml`。

正常源码工作流不要要求手动准备 `.env` 或 `config.toml`。`./start.sh` 是支持的本地入口。

需要覆盖部署参数时，可以在运行 `./start.sh` 或 `docker compose` 前通过 shell 环境变量传入。

打开：

```text
http://localhost:8081
```

## 配置边界

运行配置从 `docker/` 开始。

- 根目录 `.env` 和根目录 `config.toml` 不是运行入口。
- 正常启动不需要 `docker/.env` 或 `docker/config.toml`。
- 发布镜像和源码构建镜像都会把 `docker/config.docker.toml` 烘焙成 `/opt/web3/config.toml`。
- 部署覆盖可以通过环境变量传入。
- 登录后的 Settings 页面可以更新运行时 LLM 和 RPC 设置。

绝不要提交：

- 带真实值的 `.env`
- 私钥
- mnemonic 或 seed phrase
- 钱包导出
- `WEB3_SSM_KEY`
- `WEB3_JWT_SECRET`
- AWS credentials
- LLM provider key
- RPC provider key
- Portals 或 MetaSleuth key
- Docker 运行时数据

## 运行时安全模型

Go server 是 secrets、钱包状态、策略、安全检查、签名和广播的权威。

companion runtime 和 Claude 进程不能拿到原始部署密钥。环境变量传递必须使用 allowlist。

不要把以下值传给 companion 或 Claude：

- `WEB3_SSM_KEY`
- AWS credentials
- JWT secret
- 原始 LLM env key
- RPC provider key
- Portals key
- MetaSleuth key
- 私钥

## 钱包和交易假设

所有钱包都是 EIP-7702 managed。

- Delegator: `0x63c0c19a282a1b52b07dd5a65b58948a07dae32b`
- 非 EIP-7702 钱包会被拒绝。
- 委托是单独的显式流程。
- 写操作构建会拒绝 `not_delegated`。
- `insufficient_gas` 表示用户必须补充原生代币。
- 协议写操作、ERC-20 写操作和原生转账使用 ERC-7821 `execute(bytes32,bytes)` calldata。
- 安全检查在 build 之后、SSM 签名和广播之前运行。
- 扫描失败不是通过。

## TDD

代码变更使用 TDD：

1. 先写或更新失败测试。
2. 实现让测试通过的最小改动。
3. 在保持测试通过的前提下重构。

新增行为应在合适层级添加测试：

- Go package/service 行为：`internal/` 或 `tests/` 下的 Go 测试。
- 前端逻辑和文案：`frontend/src` 下的 Bun 测试。
- companion runtime 行为：`companion/src` 下的 Bun 测试。
- 真实 provider 或链行为：`tests/integration` 下的 Go 集成测试。

项目目标是变更区域和安全关键区域保持 **85%+ 有意义的行覆盖率**。当前脚本尚未自动强制全仓覆盖率阈值，因此 release candidate 除了测试通过，还应附带覆盖率证据。完整测试规则见 `TESTING.md`。

## 运行测试

运行本地 CI 等价检查：

```bash
./scripts/test.sh
```

它会运行：

- Go build
- Go vet
- Go short tests
- frontend type check 和 build
- frontend tests
- companion type check
- companion tests，使用隔离 `HOME`
- Gitleaks current-tree scan
- 项目专用敏感数据检查

直接运行 Go 测试：

```bash
go test -short ./internal/... ./tests/... -count=1 -timeout=120s
```

前端检查：

```bash
cd frontend
bun install --frozen-lockfile
bun run check
bun test
```

companion 检查：

```bash
cd companion
bun install --frozen-lockfile
bun run check
HOME="$(mktemp -d)" bun test
```

companion 测试必须使用隔离 `HOME`，避免本机 Claude 认证配置影响结果。

## 集成测试

集成测试位于：

```text
tests/integration/
```

需要：

```bash
WEB3_INTEGRATION=1
```

通常还需要通过 shell 环境或 `docker/.env.integration` 配置真实 provider：

```text
WEB3_RPC_ETHEREUM_URL=...
WEB3_RPC_BASE_URL=...
WEB3_RPC_POLYGON_URL=...
WEB3_RPC_BSC_URL=...
PORTALS_API_KEY=...
METASLEUTH_API_KEY=...
```

运行：

```bash
WEB3_INTEGRATION=1 go test -count=1 -v ./tests/integration/ -timeout=600s
```

缺少可选 provider key、链上余额或 RPC URL 时，部分测试可能跳过。发布测试应使用真实 provider key 和支持 trace 的 RPC 端点。

## E2E 浏览器测试

E2E 测试使用 Playwright 和虚拟 WebAuthn authenticator，针对运行中的 Docker 实例执行。

位置：

```text
tests/e2e/
```

当前 specs：

| 文件 | 覆盖内容 |
| --- | --- |
| `setup-wizard.spec.ts` | 6 步 setup wizard、默认 RPC、trace warning |
| `auth.spec.ts` | 使用虚拟 Passkey 注册和登录 |
| `settings.spec.ts` | LLM/RPC 展示、保存和重载持久化 |
| `wallets.spec.ts` | 钱包列表、加密模式、策略绑定 |
| `chat.spec.ts` | 发送消息、会话切换、滚动行为 |
| `passkey-pending.spec.ts` | 钱包导出/删除展示 pending Passkey 状态 |
| `version-check.spec.ts` | 版本展示、`/health`、更新提示 |

本地运行：

```bash
cd tests/e2e
bun install
E2E_BASE_URL=http://localhost:8081 bunx playwright test
```

E2E 测试包含在本地发布流程 `scripts/release-local.sh` 中。pre-push hook 不运行 E2E，因为耗时较长。

## Secret Scanning

运行：

```bash
docker run --rm -v "$PWD:/repo" -w /repo ghcr.io/gitleaks/gitleaks:v8.28.0 dir --redact --verbose .
./scripts/check-sensitive-data.sh
```

Gitleaks 检查常见 secret。项目专用脚本检查已跟踪敏感文件名、私钥形态赋值、助记词赋值和 Docker ignore 覆盖。

## Git Hooks

安装 pre-push hook：

```bash
cp scripts/pre-push .git/hooks/pre-push
chmod +x .git/hooks/pre-push
```

该 hook 会运行 secret scan。如果 `docker/.env.integration` 存在并设置 `WEB3_INTEGRATION=1`，它还会运行集成测试。

## 本地发布

构建和发布在本地运行。

不推送的发布演练：

```bash
scripts/release-local.sh --skip-integration v1.2.3
```

正式发布：

```bash
git status --short
git tag v1.2.3
docker login
WEB3_INTEGRATION=1 scripts/release-local.sh --push v1.2.3
```

`--push` 要求：

- 工作区干净
- tag 存在且指向 `HEAD`
- secret scan 通过
- Go/frontend/companion 检查通过
- 集成测试通过
- Docker buildx 多架构构建成功

默认发布 tag：

```text
blocksecteam/web3-companion:v1.2.3
blocksecteam/web3-companion:latest
```

## 扩展 Runtime Skills

用户侧 runtime skills 位于：

```text
skills/
```

共享合同位于：

```text
skills/_shared/
```

修改某个 flow-specific skill 前，先确认行为是否由共享合同约束：

- `_shared/preflight.md`
- `_shared/error-codes.md`
- `_shared/passkey.md`
- `_shared/security-handling.md`
- `_shared/intent-expiry.md`
- `_shared/amount-resolution.md`
- `_shared/token-resolution.md`
- `_shared/slippage.md`
- `_shared/broadcast-output.md`
- `_shared/tags.md`

Skill 文件会被烘焙进 Docker 镜像。修改后需要 rebuild 和 restart。

## 新增集成

开发者扩展流程位于 `.skills/`。

使用对应 skill，不要发明新的注册模式：

| 任务 | Skill |
| --- | --- |
| 注册 swap adapter | `.skills/swap-adapter-register/` |
| 注册 bridge adapter | `.skills/bridge-adapter-register/` |
| 注册静态安全检查 | `.skills/security-check-register/` |

自建部署新增 EVM 链是配置变更：更新烘焙进镜像的 Docker 配置并 rebuild。该链必须支持 EIP-7702。

## 前端开发说明

前端使用 React、Vite、Bun 和 i18n。

用户可见文本应放在 `frontend/src/i18n.ts`。测试会检查 placeholder、aria label 等可翻译属性不要硬编码。

运行：

```bash
cd frontend
bun run check
bun test
```

## Companion 开发说明

companion server 使用 Claude Agent SDK。主提示词：

```text
companion/web3_companion.md
```

Security Agent 提示词：

```text
companion/security.md
```

两个提示词都会被烘焙进 Docker 镜像。修改后需要 rebuild 和 restart。

## Go 开发说明

遵循标准 Go 格式：

```bash
gofmt -w <files>
go vet ./...
go test -short ./internal/... ./tests/... -count=1
```

保持 package 边界与现有职责一致。如果某个 package 变得很小，尤其低于 200 行，考虑合并到相邻职责包，除非会造成依赖环或破坏稳定边界。

## Pull Request Checklist

- 行为变更先写或更新测试。
- `./scripts/test.sh` 通过。
- 涉及 live chain/provider 行为时运行集成测试。
- 没有 tracked secrets 或本地运行时文件。
- 用户可见文案已本地化。
- 行为、设置、发布或扩展合同变化时同步文档。
- 重要变更提供覆盖率证据。
