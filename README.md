# Asimi

面向开发者的本地优先 AI 桌面助手。把 AI 对话、项目理解、代码修改、测试执行和受控电脑交互放在一个 Windows 工作空间中。

Asimi is a local-first AI desktop assistant for developers. Review project context, proposed changes, commands and computer actions before approving execution.

当前版本为 **0.2.1 试用候选版**，面向 Windows x64。原生验收环境为 Windows 11 25H2 x64，其他 Windows 版本仍需验证。

[安装指南](docs/release/INSTALLATION-GUIDE.md) · [使用指南](docs/release/USER-GUIDE.md) · [已知限制](docs/release/KNOWN-LIMITATIONS.md) · [更新记录](CHANGELOG.md) · [MIT 许可证](LICENSE)

## 可以做什么

| 功能 | 使用方式 |
| --- | --- |
| AI 对话 | 使用自己的 API Key 连接 DeepSeek，或在高级配置中添加兼容 Chat Completions、Responses 的服务。支持流式回复和本地会话历史。 |
| 理解本地项目 | 选择工作区，查看项目结构、相关源文件、Git 状态和运行环境。 |
| Agent 任务 | 描述目标，核对将发送的上下文，审阅模型提出的计划、文件 Diff 和命令。 |
| 审批后执行 | 文件修改和命令分别审批，执行后展示文件回读、测试结果和任务记录。 |
| 截图分析 | 选择显示器或区域，预览并遮挡敏感内容，再单独确认发送给支持图像的模型。 |
| 受控电脑交互 | 在支持的目标窗口中检查控件、审阅操作并批准执行。模型辅助操作当前以专用测试窗口为已验证范围。 |
| 本地记录与配置 | 在本机保存设置、会话和任务记录，支持英语、简体中文、主题切换及任务停止。 |

每次修改或命令都有独立的审阅与审批。任务时间线展示提案、审批、实际执行、验证和恢复状态。点击界面顶部的 **停止全部** 可取消正在运行的任务。

## 安装与首次使用

### 运行要求

| 项目 | 要求 |
| --- | --- |
| 系统 | Windows x64，当前原生验证环境为 Windows 11 25H2 |
| 界面运行时 | Microsoft Edge WebView2 Evergreen Runtime |
| 电脑交互 Helper | Windows .NET Framework 4.x |
| 权限 | 使用普通 Windows 用户账户运行 |
| AI 服务 | 自备供应商账户、API Key 和可用额度 |

便携版运行所需文件位于同一目录中。源码构建另需 Go、Node.js、npm 和 Wails。

### 获取便携版

查看仓库的 Releases 页面。发布了二进制附件时，下载同一版本的 ZIP 和对应的 SHA256 文件。当前打包脚本生成的文件名为 `Asimi-0.2.1-win-x64.zip`。若页面尚无二进制附件，可按下文从源码构建。[Releases](https://github.com/Axiu112530/Asimi/releases)

在 PowerShell 中计算校验值，并与同一发布页面的 SHA256 文件核对。

```powershell
Get-FileHash .\Asimi-0.2.1-win-x64.zip -Algorithm SHA256
Get-Content .\Asimi-0.2.1-win-x64.zip.sha256
```

1. 将 ZIP 完整解压到新的本地文件夹。
2. 保留 `Asimi.exe`、两个 Helper 和各自的 `.config` 文件。
3. 打开 `Asimi.exe`，选择英语或简体中文。
4. 连接 AI 供应商，或选择离线继续。
5. 选择一个本地项目，从查看结构和阅读文件开始。

历史候选包的校验值对应当时生成的文件。每个下载文件应使用其所属版本随附的校验值。[安装详情](docs/release/INSTALLATION-GUIDE.md)

### 连接 AI

首次使用可选择 DeepSeek 预设，输入自己的 API Key，点击「测试连接并保存」。测试会发送一条短请求，可能消耗供应商额度。连接成功后会保存密钥并设置默认模型。

其他服务在「设置 → AI 供应商」的高级区域配置。请核对服务地址、协议、模型 ID 和能力声明，再测试连接。图像分析需要模型支持图像输入。

供应商费用由你的服务账户承担，价格、可用模型和数据保留规则以供应商为准。Asimi 使用 Windows DPAPI 保护保存的 API Key。[隐私说明](PRIVACY.md)

### 完成一次项目任务

1. 在工作区中选择项目，检查路径、Git 状态和检测到的运行环境。
2. 新建 Agent 任务，例如「解释这个模块，并提出一个小范围修复」。
3. 核对源文件与测试输出的上下文预览，确认后发送给模型。
4. 审阅每份文件 Diff、命令参数、工作目录和网络范围，再逐项批准。
5. 查看实际执行、文件回读和测试结果，再判断任务是否完成。

建议第一次在可丢弃的示例项目中熟悉流程。已有本地 Git 改动会出现在审阅中。操作结果不确定时，先检查实际文件或目标窗口，再准备后续操作。[完整使用指南](docs/release/USER-GUIDE.md)

## 数据存储与隐私

默认应用数据目录为 `%LOCALAPPDATA%\DeskPilotAI`，也可在「设置 → 关于」查看实际位置。该名称沿用早期版本，便于直接读取原有配置与历史。

| 数据 | 保存或发送方式 |
| --- | --- |
| 设置、会话、任务、审计记录和导入附件 | 保存在本地应用数据目录 |
| 保存的 API Key | 使用当前 Windows 账户的 DPAPI 加密保护 |
| AI 对话 | 向所配置供应商发送当前提示词及相关会话上下文 |
| Agent 项目上下文 | 在预览确认后发送选中的源文件及相关测试输出 |
| 截图 | 在预览、遮挡和单独确认后发送 |

本地数据库、导入附件和运行日志可能包含私人内容，备份与故障反馈时请妥善处理。DPAPI 保护覆盖保存的密钥，其他本地内容按实际文件保存。关闭所有 Asimi 实例后再备份数据目录，跨账户或电脑恢复时可能需要重新输入 API Key。[完整隐私说明](PRIVACY.md) · [升级指南](docs/release/UPGRADE-GUIDE.md)

## 从源码构建

### 准备环境

构建目标为 Windows x64。当前验证工具链为 Go **1.26.10**、Node.js **24.x**、Wails **3.1.0**。Vue、TypeScript、Vite 等前端版本由 `frontend/package-lock.json` 固定。

1. 安装 Git、Node.js 和 npm。
2. 从 Go 官方下载页获取 `go1.26.10.windows-amd64.zip`，核对官方 SHA256 后，将 ZIP 内的 `go` 目录解压到项目的 `.phase0-tools/go`。
3. 确保 WebView2 和 Windows .NET Framework 4.x 可用。构建脚本会使用系统自带的 `Framework64/v4.0.30319/csc.exe` 编译 Helper。[Go 下载](https://go.dev/dl/) · [Wails 安装](https://v3.wails.io/getting-started/installation/)

在新的 PowerShell 窗口中执行下列命令。初始化脚本为当前进程设置项目内的工具链和缓存目录。

```powershell
git clone https://github.com/Axiu112530/Asimi.git
Set-Location Asimi

# 将 Go 解压到 .phase0-tools/go 后，选择项目工具链
. .\validation\phase0\env.ps1
go version

# 安装与 go.mod 一致的 Wails CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
go mod download
go mod verify

Push-Location frontend
npm.cmd ci --ignore-scripts --no-audit --no-fund
Pop-Location

.\scripts\build.ps1
```

构建产物位于 `build/bin/asimi-0.2.1/`。运行完整目录中的 `Asimi.exe`，或执行启动脚本。

```powershell
.\scripts\run.ps1
```

需要独立测试配置时可以指定本地数据目录。

```powershell
.\scripts\run.ps1 -DataDir .\build\bin\asimi-dev-profile
```

### 检查与打包

在已初始化工具链、安装前端依赖并完成构建的 PowerShell 中运行。

```powershell
go test ./... -count=1
go vet ./...
go mod verify
node .\validation\phase1\check-i18n.mjs
node .\validation\v021\check-locales.mjs

Push-Location frontend
npm.cmd run typecheck
npm.cmd run build
Pop-Location

.\scripts\package-asimi.ps1
```

打包脚本包含应用许可证、第三方声明、安装和使用指南、隐私说明，并生成逐文件校验清单与 ZIP 的 SHA256。源码、测试结果与本地构建产物的检查完成后，由维护者发布 Releases 附件。原生电脑操作验收需要交互式 Windows 桌面，历史验收范围见报告。[验收说明](docs/release/RELEASE-ACCEPTANCE.md)

## 项目目录与本地生成文件

| 路径 | 用途 | Git 处理方式 |
| --- | --- | --- |
| `main.go`、`internal/` | Wails 入口、后端、AI、存储及执行逻辑 | 提交源码 |
| `frontend/` | Vue 3、TypeScript 界面及中英文资源 | 提交源码和锁文件 |
| `build/windows/` | Windows 图标、版本信息和 manifest | 提交构建输入 |
| `scripts/`、`validation/` | 可复现的构建、打包和测试输入 | 提交脚本与夹具 |
| `docs/` | 用户指南、架构、许可和验收摘要 | 提交经过检查的文档 |
| `.local-validation/`、旧阶段数据目录 | 隔离测试配置和临时数据 | 由 .gitignore 排除 |
| `.phase0-tools/`、`.phase0-cache/` | 本地工具链与缓存 | 由 .gitignore 排除 |
| `build/bin/`、`frontend/dist/`、`frontend/node_modules/` | 可执行文件、打包产物和依赖 | 由 .gitignore 排除 |

应用数据目录、测试数据库、API Key 和原始诊断记录应保存在本地。清理测试数据前先关闭相关进程，确认后续无需使用该夹具。清理工具链或缓存后，下一次构建需要重新准备环境。日常使用的应用数据目录保存个人历史，请先备份。[贡献指南](CONTRIBUTING.md)

## 当前限制

- 0.2.1 为试用候选版，Windows 验证结果限于已记录的环境。
- 真实桌面视觉分析和空白记事本场景仍需要用户在目标桌面主动验收。
- 额外供应商和自定义模型需要各自的连接与能力检查。
- 离线模式支持本地浏览、历史及离线体验，真实 AI 推理需要可用的模型服务。
- 修改可能需要恢复或重新审批，请根据任务时间线和实际文件状态处理。[已知限制](docs/release/KNOWN-LIMITATIONS.md)

## 反馈、贡献与仓库权限

使用 Issues 报告可复现的问题或提出功能建议。提交错误信息前，请去掉密钥、私有代码、个人路径和截图中的敏感内容。漏洞按私密渠道报告。[Issues](https://github.com/Axiu112530/Asimi/issues) · [安全报告](SECURITY.md)

贡献代码时 Fork 仓库，在自己的分支修改并发起 Pull Request，由维护者审核和合并。原仓库的直接写入权限由维护者管理。[贡献指南](CONTRIBUTING.md)

## 开源协议

Asimi 的源码和项目文档采用 **MIT License**，版权归属见根目录 LICENSE。

MIT 允许个人使用、商业使用、修改、复制和再分发。分发软件副本或实质性部分时，需要保留版权声明和许可声明。软件按许可证中的「AS IS」条款提供。MIT 本身无需修改者公开其全部源代码。第三方组件遵循各自许可证。[完整 MIT 原文](LICENSE) · [协议说明](docs/release/LICENSE-DECISION.md) · [第三方声明](docs/release/THIRD-PARTY-NOTICES.md)

API 服务商的账户、费用与数据规则按各自服务条款执行。源码许可与原仓库的 GitHub 写入权限分别由各自规则管理。

## 更多文档

- [安装指南](docs/release/INSTALLATION-GUIDE.md)与[使用指南](docs/release/USER-GUIDE.md)
- [升级与数据兼容](docs/release/UPGRADE-GUIDE.md)
- [架构说明](docs/ARCHITECTURE.md)
- [发布报告](docs/release/RELEASE-REPORT.md)与[验收范围](docs/release/RELEASE-ACCEPTANCE.md)
- [历史文档索引](docs/README.zh-CN.md)
