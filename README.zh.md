# claude-bts

**B**ulletproof **T**echnical **S**pecification — 在规范错误变成调试会话之前将其捕获。

[![CI](https://github.com/imtemp-dev/claude-bts/actions/workflows/ci.yml/badge.svg)](https://github.com/imtemp-dev/claude-bts/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/imtemp-dev/claude-bts)](https://github.com/imtemp-dev/claude-bts/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev)

[English](README.md) | [한국어](README.ko.md) | [日本語](README.ja.md) | [For AI Agents](llms.txt)

## Why

如果你认真使用 Claude Code，你可能已经建立了自己的流程：提醒 AI 注意整体架构、要求它审查自己的输出、检查边界情况。这是对的——你的直觉没有错。

但每次都手动执行，有现实的局限：

- **不一致。** 有些会话你记得要求审查，有些你忘了。质量取决于你当天有多仔细。
- **错误越早发现越便宜。** 当计划中的错误直接变成代码，它会扩散到多个文件，修复需要构建和调试。在计划阶段发现只需要改文本。但没有验证步骤，计划级别的错误在变成代码问题之前没有机会被发现。
- **实现会淹没目的地。** AI 能做计划，但一旦深入代码——修复类型错误、追查测试失败——它就会忘记完成的系统整体应该是什么样子。bts 从意图、范围、线框开始正是为此：在细节占满上下文之前，先确立全局。

模式总是一样的：你在通过对话做质量控制，每个会话从头来过，上次发现的问题这次不一定能再次发现。

## bts 做什么

bts 是一个接入 Claude Code 生命周期钩子的 CLI 工具。它将你已经在做的流程结构化——但使其自动化、可追踪，并由独立的 AI 上下文进行验证。

**结构化的全局优先。** 在任何代码之前，bts 经历意图探索、范围定义和线框设计。这为后续每个步骤——起草、验证、实现——提供了可以回溯参照的目的地，防止 AI 为了眼前的问题而牺牲整体。

**隔离验证。** 当 AI 在同一会话中审查自己的输出时，它共享相同的盲点。bts 在独立的代理上下文中运行验证——一个不共享生成该文档的对话历史的不同 AI 实例。

**跨会话状态追踪。** bts 记录验证中发现的每个问题，追踪哪些已解决，并在会话和上下文压缩之间持久化。当会话恢复时，它确切知道进度和未解决的问题。

**完成门控。** 验证不通过就无法定稿。测试通过、审查完成、规范与代码的偏差被记录之前，实现无法完成。这些门控自动执行——不依赖你记得去检查。

核心思想很简单：**在文档中而非代码中捕获错误。** 修改规范是文本编辑，修改代码是构建-测试-调试循环。实现之前过滤掉的错误越多，实现之后的返工越少。

## 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code)。

```bash
# Homebrew (macOS / Linux)
brew tap imtemp-dev/tap
brew install bts

# 或一行安装
curl -fsSL https://raw.githubusercontent.com/imtemp-dev/claude-bts/main/install.sh | bash

# 或从源码构建 (Go 1.22+)
git clone https://github.com/imtemp-dev/claude-bts.git && cd claude-bts && make install

# 在项目中初始化
cd your-project
bts init .

# 启动 Claude Code
claude
```

然后在 Claude Code 中：

```bash
# 创建完美规范 → 实现 → 测试 → 完成
/bts-recipe-blueprint 添加 OAuth2 认证

# 修复已知漏洞
/bts-recipe-fix 登录 bcrypt 哈希比较失败

# 调试未知问题
/bts-recipe-debug 5分钟后会话断开
```

## 工作原理

bts 将工作分为**规范**和**实现**两个阶段。每种配方类型有自己的规范阶段，但都共享相同的实现循环。

在规范阶段，bts 迭代文档——探索意图、调研代码库、起草详细设计，并在独立的 AI 上下文中进行多轮验证。此阶段发现的错误只需文本编辑即可修复。

在实现阶段，bts 从定稿规范生成代码、运行测试（失败时重试）、模拟代码路径、审查质量，并将偏差同步回规范。每个步骤都有自动门控，在满足要求之前阻止完成。

各配方类型的详细流程见[配方生命周期](#配方生命周期)。

## 配方

| 配方 | 用途 | 输出 |
|------|------|------|
| `/bts-recipe-blueprint` | 完整实现规范 | Level 3 规范 → 代码 → 测试 |
| `/bts-recipe-design` | 设计功能 | Level 2 设计文档 |
| `/bts-recipe-analyze` | 理解现有系统 | Level 1 分析文档 |
| `/bts-recipe-fix` | 已知漏洞修复 | 修复规范 → 代码 → 测试 |
| `/bts-recipe-debug` | 未知漏洞调查 | 6视角分析 → 规范 → 代码 |

对于多功能项目，bts 将工作分解为**愿景 + 路线图**。每个配方映射到路线图项目，完成状态自动跟踪。

## 功能

### 21 个技能

| 类别 | 技能 |
|------|------|
| **配方** | blueprint, design, analyze, fix, debug |
| **发现** | discover, wireframe |
| **验证** | verify, cross-check, audit, assess, sync-check |
| **分析** | research, simulate, debate, adjudicate |
| **实现** | implement, test, sync, status |
| **质量** | review (basic / security / performance / patterns) |

### 生命周期钩子

| 钩子 | 用途 |
|------|------|
| session-start | 上下文感知恢复（注入配方状态 + 下一步提示） |
| stop | 完成门控（在允许完成前验证规范、测试、审查） |
| pre-compact | 上下文压缩前快照工作状态 |
| session-end | 为跨会话恢复持久化工作状态 |
| post-tool-use | 工具使用指标追踪（工具名、文件、成功/失败） |
| subagent-start/stop | 子代理生命周期指标追踪 |

### 指标与成本估算

```
bts stats
```

```
Project Overview
────────────────────────────────────────
  Recipes:     3 complete, 1 active, 4 total
  Sessions:    12 total, 5 compactions
  Models:      claude-opus-4-6, claude-sonnet-4-6

Estimated Cost
────────────────────────────────────────
  Total:       $4.52
  Input:       $1.23
  Output:      $2.89
```

会话级和配方级令牌追踪，带模型特定的成本估算。支持导出为 CSV（`--csv`）或 JSON（`--json`）以供外部分析。

### 状态栏

```
bts v0.1.0 │ JWT auth │ implement 3/5 │ ctx 60%
```

在 Claude Code 状态栏中实时显示配方进度、阶段和上下文使用情况。

### 文档可视化

```bash
bts graph              # 项目级文档关系图
bts graph <recipe-id>  # 配方级文档图
```

生成 mermaid 图表，显示文档依赖关系、辩论结论和验证链。

## 配方生命周期

每种配方类型有自己的规范阶段。生成代码的配方共享相同的实现阶段。

### 规范阶段（按配方类型）

**Blueprint** — 新功能的完整规范：

```mermaid
flowchart LR
    DIS["发现"] --> SC["范围"] --> R["调研"] --> W["线框"]
    W --> D["草稿"] --> V["验证"]
    V -->|"问题"| D
    V -->|"通过"| F["定稿"]
```

**Fix** — 轻量诊断：

```mermaid
flowchart LR
    DIAG["诊断"] --> SPEC["修复规范"] --> F["定稿"]
```

**Debug** — 多视角根因分析：

```mermaid
flowchart LR
    BP["6 蓝图"] --> CROSS["交叉分析"] --> SPEC["修复规范"] --> F["定稿"]
```

**Design** / **Analyze** — 仅规范，无实现：

```mermaid
flowchart LR
    R["调研"] --> D["草稿"] --> V["验证"]
    V -->|"问题"| D
    V -->|"通过"| F["定稿"]
```

### 实现阶段（共享）

所有生成代码的配方通过 `/bts-implement` 进入相同的实现循环：

```mermaid
flowchart LR
    F["定稿规范"] --> IMP["实现"] --> T["测试"]
    T -->|"失败"| IMP
    T -->|"通过"| SIM["模拟"] --> RV["审查"] --> SY["同步"] --> DONE["完成"]
```

## 架构

**Go 二进制文件** — 单一静态链接二进制文件（约 5ms 启动）。管理状态、验证完成、部署模板、追踪指标。除 Go 之外零运行时依赖。

**Claude Code 集成** — 21 个技能提供配方协议，8 个生命周期钩子处理会话事件（恢复、完成门控、指标），6 个规则强制约束。验证始终在独立的代理上下文中运行。

## 模型与配置

bts 使用两层 AI 模型：

**主会话模型** — 你在 Claude Code 中运行的模型（Opus、Sonnet 等）处理所有主要工作：起草规范、实现代码、进行辩论、编排生命周期。

**专家代理** — 验证、审计、模拟和审查在**独立代理上下文**（fork）中运行，不与主会话共享盲点。默认为 Sonnet，可在 `.bts/config/settings.yaml` 中配置：

```yaml
agents:
  # verifier: sonnet         # 默认：会话模型
  # auditor: sonnet          # 默认：会话模型
  # simulator: sonnet        # 默认：会话模型
  # reviewer_quality: sonnet # 默认：会话模型
  reviewer_security: sonnet  # 基于模式，sonnet 足够
  # reviewer_arch: sonnet    # 默认：会话模型
```

选项：`sonnet`（均衡）、`opus`（更深分析，更高成本）、`haiku`（快速，可能遗漏细微问题）。取消注释即可覆盖。

### 各阶段使用的模型

| 阶段 | 技能 | 上下文 | 模型 |
|------|------|--------|------|
| 发现、范围、调研 | discover, blueprint, research | 主会话 | 会话模型 |
| 线框、草稿、改进 | wireframe, blueprint | 主会话 | 会话模型 |
| 辩论、裁决 | debate, adjudicate | 主会话 | 会话模型 |
| **验证** | verify | **fork** | 会话模型（核心关卡） |
| **审计** | audit | **fork** | 会话模型（查找缺失） |
| **模拟** | simulate | **fork** | 会话模型（深度推理） |
| **交叉检查、同步检查** | cross-check, sync-check | **fork** | Sonnet（基于模式） |
| 实现、测试、同步 | implement, test, sync | 主会话 | 会话模型 |
| **审查**（质量、架构） | review | **fork** | 会话模型 |
| **审查**（安全） | review | **fork** | Sonnet（基于模式） |
| 状态 | status | 主会话 | 会话模型 |

fork 上下文是关键——当同一模型在同一会话中审查自己的输出时，它共享相同的盲点。Fork 代理只看到文档，看不到产生该文档的对话。

## 核心原则

- **文档优先** — 迭代规范，而非代码
- **禁止自我验证** — 验证使用独立的代理上下文
- **上下文即胶水** — 技能提供情境感知，而非强制规则
- **偏差 = 后续工作** — 规范与代码的差异是报告，不是关卡
- **崩溃恢复** — 通过 JSON 持久化工作状态；会话自动恢复
- **层级地图** — 轻量级项目概览，按需查看细节
- **高速** — 单一 Go 二进制文件，零运行时依赖，约 5ms 启动

## CLI

```
bts init [dir]              初始化项目（部署技能、钩子、规则）
bts doctor [recipe-id]      健康检查（系统、配方、文档）
bts validate [recipe-id]    JSON 模式合规性检查
bts verify <file>           文档一致性检查，级别评估
bts recipe status           显示活动配方
bts recipe list             所有配方列表
bts recipe create           创建新配方
bts recipe log <id>         记录操作 / 阶段 / 迭代
bts recipe cancel           取消活动配方
bts stats [recipe-id]       指标和成本估算 (--json, --csv)
bts graph [recipe-id]       文档关系可视化 (--all)
bts sync-check <id>         验证配方内文档同步
bts update                  更新模板以匹配二进制版本
bts version                 显示二进制和模板版本
```

## 要求

- **Go** 1.22+（[安装](https://go.dev/dl/)）
- **Claude Code**（[安装](https://docs.anthropic.com/en/docs/claude-code)）
- **操作系统**：macOS、Linux（Windows 通过 WSL）

安装后运行 `bts doctor` 验证环境。

## 贡献

欢迎贡献。请通过 [issue](https://github.com/imtemp-dev/claude-bts/issues) 提交漏洞报告或功能请求。

```bash
# 开发环境设置
git clone https://github.com/imtemp-dev/claude-bts.git
cd claude-bts
make install          # 构建并安装到 ~/.local/bin
go test -race ./...   # 运行测试
```

## 许可证

MIT
