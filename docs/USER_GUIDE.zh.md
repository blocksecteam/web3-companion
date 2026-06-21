# Web3 Companion 普通用户使用指南

[English](USER_GUIDE.md)

本指南面向希望自托管并使用 Web3 Companion 的普通用户。Web3 Companion 是一个安全的 Agentic Wallet，支持自主链上操作。

Web3 Companion 运行在你自己的机器或服务器上。它提供一个浏览器应用，用于查看资产、管理钱包、执行 Web3 操作、进行交易安全审查，并在敏感签名前要求 Passkey 确认。

## 需要准备

- Docker。
- 支持 WebAuthn Passkey 的浏览器。
- LLM provider key，可在 Setup Wizard 或 Settings 页面配置。
- 你要使用的链对应的 RPC 端点。
- 钱包在委托和写操作前需要有对应链的原生 gas 代币。

默认链包括 Ethereum、Base、Polygon 和 BSC。

## 快速启动

使用发布镜像快速试用：

```bash
docker run -d \
  --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  blocksecteam/web3-companion
```

打开：

```text
http://localhost:8081
```

首次启动时，Setup Wizard 会引导你完成配置。

常用选项：

```bash
# 自定义 HTTPS 域名，用于 Passkey 绑定
docker run -d --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  -e WEB3_EXTERNAL_URL=https://companion.example.com \
  blocksecteam/web3-companion

# 使用 AWS KMS 加密钱包 key
docker run -d --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  -e AWS_REGION=us-east-1 \
  -e AWS_KMS_KEY_ID=arn:aws:kms:us-east-1:123456789:key/your-key-id \
  -e AWS_ACCESS_KEY_ID=AKIA... \
  -e AWS_SECRET_ACCESS_KEY=... \
  blocksecteam/web3-companion

# 自定义宿主机端口
docker run -d --name web3-companion -p 9090:8081 -v web3-data:/home/web3 blocksecteam/web3-companion
```

非 localhost 部署请在前面放置 HTTPS 反向代理，并把 `WEB3_EXTERNAL_URL` 设置为浏览器实际访问的 origin。Passkey 会绑定到这个 origin。

四个 AWS KMS 变量必须同时设置。未设置时，钱包 key 会使用自动生成并保存在 SQLite 中的 AES-256 key 做本地加密。

## 从源码启动

在源码仓库中运行：

```bash
cd docker
./start.sh
```

`start.sh` 会构建镜像、准备数据目录并启动 Docker Compose。它不需要 `docker/.env` 或 `docker/config.toml`。

LLM、RPC 和可选 provider key 通过 Setup Wizard 或登录后的 Settings 页面配置。需要覆盖部署参数时，可以使用 shell 环境变量：

```bash
WEB3_PORT=8082 WEB3_EXTERNAL_URL=https://companion.example.com ./start.sh
```

打开：

```text
http://localhost:8081
```

使用其他本地端口：

```bash
WEB3_PORT=8082 ./start.sh
```

Docker Compose 默认只绑定到 `127.0.0.1`。

## 首次配置

Setup Wizard 会依次配置：

1. 实例安全密钥。
2. RPC 端点。
3. LLM provider。
4. 可选的数据 provider key。
5. 邮箱和 Passkey 账户。
6. 最终状态检查。

如果向导显示自动生成的安全密钥，请保存好。容器数据卷丢失时，可能需要这些密钥恢复加密的本地状态。

## 账户和 Passkey

邮箱是账户标识。

以下操作需要 Passkey：

- 注册
- 登录
- 策略变更
- 钱包删除
- 钱包导出
- 手动签名审批

Passkey 在 `localhost` 可以直接用于本地开发。非 localhost 部署必须使用 HTTPS，并正确配置浏览器访问 URL。

## 钱包模型

所有钱包都是 EIP-7702 managed wallet。

- 钱包委托到 MetaMask delegator `0x63c0c19a282a1b52b07dd5a65b58948a07dae32b`。
- 非 EIP-7702 钱包会被拒绝。
- 新钱包默认获得预定义 `default` 手动策略。
- 默认关闭自动签名。
- 协议写操作、ERC-20 写操作和原生代币转账都使用 ERC-7821 `execute(bytes32,bytes)` calldata。

写操作前会检查委托状态。如果钱包尚未委托，写操作构建会返回 `not_delegated`。

## 委托和 Gas

新钱包第一次在目标链委托前，需要有该链原生代币支付 gas。具体数额因链而异。

不要凭记忆估算金额。使用以下命令预览：

```bash
web3 ssm delegate-estimate --wallet <wallet_id> --chain <chain>
```

如果委托失败并返回 `insufficient_gas`，请给该钱包补充对应链的原生代币后重试。

## 使用浏览器应用

登录后，浏览器应用提供：

- 对话工作区
- 资产视图
- 代币数据
- 交易历史
- 钱包管理
- 策略管理
- 设置
- 审计日志

可以尝试：

```text
我有哪些资产？
转 0.01 ETH 到 0x...
在 Base 上把 25 USDC 换成 ETH。
帮我找一条把 USDC 从 Ethereum 跨到 Polygon 的路线。
解释我这笔待处理交易的安全检查结果。
```

## 交易安全流程

敏感写操作会分阶段执行：

1. 构建交易意图。
2. 检查 EIP-7702 委托。
3. 模拟并检查准备好的交易。
4. 运行确定性的安全规则。
5. 按配置运行 Security Agent 复核。
6. 应用钱包签名策略。
7. 需要时请求 Passkey 审批。
8. 通过 Secure Signature Module 签名。
9. 广播已签名交易。

静态安全 verdict 包括：

- `pass`
- `warn`
- `fail`

扫描失败不会被当作通过。如果交易无法自动放行，应用会要求用户显式确认。

## 设置

在登录后的 Settings 页面可以配置：

- LLM provider
- LLM base URL
- 模型名称
- 模型 API key
- RPC 端点
- Portals API key
- MetaSleuth API key

不要把部署密钥提交到源码仓库。不要提交 `.env`、私钥、钱包导出、API key 或运行时数据。

## 数据和日志

容器内重要路径：

| 路径 | 用途 |
| --- | --- |
| `/home/web3/web3.db` | SQLite 数据库 |
| `/home/web3/keys/` | 加密的钱包 key 文件 |
| `/home/web3/settings/` | 运行时 LLM 和 RPC 设置 |
| `/home/web3/companion/users/<user-id>/` | 每个用户独立的 companion home |
| `/home/web3/logs/web3.log.jsonl` | 轮转 JSONL server 日志 |

从源码用 Docker Compose 运行时，宿主机数据默认保存在：

```text
docker/data-8081
```

## 钱包导出和删除

钱包导出和删除都需要 Passkey 审批。

导出会显示一次私钥，并将钱包从管理列表移除。离开页面前请安全保存私钥。

如果最近的资产检查显示钱包资产超过配置的极小 dust 阈值，删除会被阻止。

## 可选 CLI

大多数用户应该使用浏览器应用。

Docker 镜像包含 `web3` CLI，用于 companion/container 内部工具。已认证的写入和执行命令需要后端签发的 container credential，预期在 companion runtime 内运行。

服务启动后，在源码仓库中运行：

```bash
docker compose -f docker/compose.yml exec web3 web3 --help
```

命令组包括：

```text
web3 portfolio
web3 token
web3 chain
web3 swap
web3 bridge
web3 security
web3 ssm
```

## 常见问题

| 现象 | 检查项 |
| --- | --- |
| 模型认证缺失 | 在 Setup Wizard 或 Settings 中配置 LLM provider key。 |
| RPC chain ID 校验失败 | 确认每个 RPC URL 指向配置中的对应链。 |
| Passkey 在非 localhost 环境失败 | 使用 HTTPS，并设置 `WEB3_EXTERNAL_URL`、`WEB3_RP_ID` 和 `WEB3_RP_ORIGIN`。 |
| `not_delegated` | 先运行显式委托流程。 |
| `insufficient_gas` | 给该钱包在对应链补充原生代币。 |
| 安全检查失败或无法扫描 | 在复核或显式确认前，不要把它视为安全。 |
