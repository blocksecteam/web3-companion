<p align="center">
  <img src="docs/assets/logo.png" alt="Web3 Companion logo" width="280">
</p>

<h1 align="center">Web3 Companion</h1>

<p align="center"><strong>安全的 Web3 AI Agent</strong></p>

<p align="center">
  <em>一个能自主执行链上操作的 AI Agent —— 拥有它永远无法触及的私钥，和它永远无法更改的策略。</em>
</p>

<p align="center">
  <a href="https://hub.docker.com/r/blocksecteam/web3-companion"><img src="https://img.shields.io/docker/pulls/blocksecteam/web3-companion" alt="Docker"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
</p>

<p align="center"><a href="README.md">English</a></p>

## 为什么选择 Web3 Companion

Web3 AI Agent 可以自动化链上操作 —— 但大多数 Agent 把你的私钥放在与处理不可信内容相同的进程中。一次 prompt injection、一个恶意技能、一个被攻破的依赖，你的资金就永久消失了。

Web3 Companion 采用不同的方法。AI Agent 可以完成 Web3 高级用户需要的一切 —— 查看资产、准备转账、报价兑换和跨链、运行安全分析、签名交易 —— 但它在三个独立的安全边界内运行：

- **Agent 永远无法触碰私钥。** 密钥存在于隔离的安全签名模块中。Agent 使用意图 ID 工作，而不是密钥材料。
- **签名遵循代码强制的策略。** 单笔上限、每日预算、收款人白名单。Agent 无法修改这些 —— 更改需要你的 Passkey。
- **Passkey 守护每一个敏感操作。** 策略更改、钱包导出、手动签名审批 —— 都需要硬件绑定的生物识别证明你本人在场。

即使 Agent 被完全攻破，它也无法清空钱包。这就是设计。

阅读完整的[安全设计](docs/SECURITY_DESIGN.zh.md)了解威胁模型、架构和每个边界的工作原理。

## 快速开始

```bash
docker pull blocksecteam/web3-companion
docker run -d --name web3-companion -p 8081:8081 -v web3-data:/home/web3 blocksecteam/web3-companion
```

打开 **http://localhost:8081** —— Setup Wizard 引导你完成 LLM 和 RPC 配置。不需要 `.env` 文件或预配置。

<details>
<summary>自定义域名（HTTPS + Passkey 绑定）</summary>

```bash
docker run -d --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  -e WEB3_EXTERNAL_URL=https://companion.example.com \
  blocksecteam/web3-companion
```

在前面放一个 HTTPS 反向代理（Nginx/Caddy）。Passkey 绑定到 `WEB3_EXTERNAL_URL` 中的域名 —— 后续修改会使已有 Passkey 失效。
</details>

<details>
<summary>AWS KMS 钱包加密</summary>

```bash
docker run -d --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  -e AWS_REGION=us-east-1 \
  -e AWS_KMS_KEY_ID=arn:aws:kms:us-east-1:123456789:key/your-key-id \
  -e AWS_ACCESS_KEY_ID=AKIA... \
  -e AWS_SECRET_ACCESS_KEY=... \
  blocksecteam/web3-companion
```

四个 AWS 变量必须同时设置。不设置时，钱包密钥使用存储在 SQLite 中的自动生成 AES-256 密钥进行本地加密。
</details>

<details>
<summary>自定义端口</summary>

```bash
docker run -d --name web3-companion \
  -p 9090:8081 \
  -v web3-data:/home/web3 \
  -e WEB3_EXTERNAL_URL=http://localhost:9090 \
  blocksecteam/web3-companion
```

容器内部始终监听 8081 端口。通过 `-p <主机端口>:8081` 映射到任意主机端口。

**重要：** 使用非默认端口时，**必须**设置 `WEB3_EXTERNAL_URL` 与浏览器访问地址一致。Passkey 绑定到完整的 origin（协议 + 主机 + 端口）—— 如果 `WEB3_EXTERNAL_URL` 与浏览器 URL 不匹配，Passkey 注册和认证会因 "origin mismatch" 失败。
</details>

<details>
<summary>从源码构建</summary>

```bash
git clone https://github.com/blocksecteam/web3-companion.git
cd web3-companion/docker
./start.sh
```

`start.sh` 构建镜像并启动 Docker Compose。不需要 `docker/.env` 或 `docker/config.toml`；首次运行的 Setup Wizard 会处理配置。
</details>

## Agent 能做什么

- **资产组合** —— 查看 Ethereum、Base、Polygon 和 BSC 上的持仓
- **转账** —— 向任意地址发送原生代币和 ERC-20 代币
- **兑换** —— 通过 7 个 DEX 聚合器（Odos、Kyber、ParaSwap、OpenOcean、Uniswap、PancakeSwap、LiFi）报价和执行同链兑换
- **跨链** —— 通过 Bungee、Relay 和 LiFi 报价和执行跨链桥
- **安全审查** —— 在签名前模拟交易、解码 calldata、检查无限授权、销毁地址、价值守恒和地址风险标签
- **自动签名** —— 在策略限额内无需人工审批即可签名；超出限额时停止并要求 Passkey

## 安全机制

每笔交易经过三个独立层级。每一层都假设前一层已被绕过：

```
第 1 层：安全审查       → "这笔交易安全吗？"
   模拟、14 项静态检查、地址标签、AI 审查
   可被绕过：数据缺失、prompt injection、新型攻击

第 2 层：签名策略       → "这笔交易被允许吗？"
   单笔上限、每日预算、收款人白名单
   代码强制。Agent 无法修改。更改需要 Passkey。

第 3 层：Passkey 审批   → "人类确认了吗？"
   硬件绑定的生物识别挑战
   没有用户在场无法绕过
```

| 如果这个被攻破…… | 这些仍然保护你 |
|----------------|-------------|
| Agent（prompt injection） | 无密钥 → 不能签名。策略 → 不能超限。Passkey → 不能审批。 |
| 安全审查（被操纵） | 策略无论如何强制限制。Passkey 仍然需要。 |
| 除 Passkey 外的一切 | 硬件绑定凭据 → 攻击者需要你的物理身体。 |

## 支持的链

| 链 | Chain ID | 原生代币 |
| --- | ---: | --- |
| Ethereum | 1 | ETH |
| Base | 8453 | ETH |
| Polygon | 137 | POL |
| BSC | 56 | BNB |

添加新 EVM 链只需配置 —— 在 `config.toml` 中添加 `[[chains]]` 块并重新构建。

## 文档

| 文档 | 内容 |
|------|------|
| [用户手册](docs/USER_GUIDE.zh.md) | Setup Wizard、账户注册、钱包管理、工作流、问题排查 |
| [开发指南](docs/DEVELOPER_GUIDE.zh.md) | 本地开发、测试、发布流程、扩展技能和适配器 |
| [安全设计](docs/SECURITY_DESIGN.zh.md) | 威胁模型、私钥隔离、交易安全层级、策略设计 |
| [English README](README.md) / [User Guide](docs/USER_GUIDE.md) / [Developer Guide](docs/DEVELOPER_GUIDE.md) / [Security Design](docs/SECURITY_DESIGN.md) | 英文版 |

## 开源研究版本

本仓库是开源研究版本，仅供研究、审阅和实验使用。尚未完成完整的生产级安全审计或安全验证。

请自行承担使用风险。BlockSec 不保证本软件的安全性、正确性、可用性或适用性，也不对因使用本仓库或基于本仓库部署而产生的安全事件、功能故障、交易错误、密钥丢失、资产损失或任何直接、间接损失承担责任。

## 仓库结构

```text
cmd/                  # 入口：server、cli、dbdump
companion/            # AI Agent 运行时（TypeScript + Claude Agent SDK）
frontend/             # 嵌入式 React 浏览器应用
internal/             # Go API 服务器
  ├── auth/           # JWT + 邮箱 + Passkey 登录
  ├── ssm/            # 安全签名模块（金库、签名、委托）
  ├── security/       # 交易模拟 + 14 项静态检查 + AI 审查
  ├── policy/         # 签名策略 + 自动签名预算追踪
  ├── swap/           # 7 个 DEX 聚合器适配器
  ├── bridge/         # 3 个跨链桥协议适配器
  ├── chain/          # 原生 + ERC-20 转账、广播
  └── ...
skills/               # 内置到 Docker 镜像的 Agent 技能
.skills/              # 开发者扩展技能（贡献者使用）
tests/                # Go 单元 + 集成 + Playwright E2E 测试
docker/               # Dockerfile、compose、配置、入口脚本
```

## 许可

基于 [MIT License](LICENSE) 授权。
