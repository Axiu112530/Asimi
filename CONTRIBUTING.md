# 参与 Asimi

欢迎提交可复现的错误报告、文档修正、翻译与代码改进。项目源码和文档采用 MIT License。请提交你有权按该协议分享的内容。[MIT License](LICENSE)

## 报告问题

使用 GitHub Issues，附上 Asimi 版本、Windows 版本与架构、最小复现步骤、预期结果和实际结果。涉及 AI 时说明使用离线、回环或外部供应商，以及相关模型和协议。错误报告中的日志、路径和截图请先脱敏。[提交 Issue](https://github.com/Axiu112530/Asimi/issues)

漏洞使用私密报告渠道，准备最小、隔离的复现场景。[安全报告](SECURITY.md)

## 提交改动

1. Fork 仓库，克隆自己的副本并创建工作分支。
2. 完成与目标相关的修改，保持现有接口、数据库迁移和安全约束兼容。
3. 执行与变更有关的检查，并在 Pull Request 中写明结果与尚未验证的范围。
4. 向原仓库发起 Pull Request，由维护者审核后决定是否合并。

维护者管理原仓库的直接写入权限。Pull Request 提供供审核的改动。

## 准备开发环境

按 README 中的 Windows 构建步骤准备 Go 1.26.10、Node.js 24.x、Wails 3.1.0 和前端锁定依赖。工具链脚本使用项目内目录保存工具与缓存。[源码构建](README.md#从源码构建)

在项目根目录的新 PowerShell 中初始化后，执行相关检查。

```powershell
. .\validation\phase0\env.ps1
go test ./... -count=1
go vet ./...
go mod verify
node .\validation\phase1\check-i18n.mjs
node .\validation\v021\check-locales.mjs

Push-Location frontend
npm.cmd run typecheck
npm.cmd run build
Pop-Location

git diff --check
```

Windows 打包使用 scripts/build.ps1 和 scripts/package-asimi.ps1。Go 主程序嵌入 frontend/dist，因此首次执行完整 Go 测试前应先完成一次前端构建。仅修改文档时，检查内容准确性、相对链接和 Git 差异即可。

## 涉及执行行为的修改

- 文件改动、命令和电脑操作需要保持逐动作审批与目标校验。
- 测试应覆盖目标变化、过期或无效审批、中断、执行后验证及恢复。
- 使用可丢弃项目、隔离数据目录和专用测试窗口，注明测试使用模拟、回环、原生或真实外部供应商。
- 保持 SQLite 迁移可升级，保留中英文翻译键和插值参数。修改显示文案时同步更新相关回归期望。
- 真实桌面与记事本验收按现有交互流程进行，记录用户实际参与的范围。[电脑操作验收](docs/0.2.1/COMPUTER-ACCEPTANCE.md)

## 提交文件范围

提交源码、可复现的构建与测试输入、依赖锁文件、Windows 构建资源及经过检查的文档。提交前核对暂存清单。

```powershell
git diff --cached --name-status
git diff --cached --check
```

工具链、缓存、node_modules、编译输出、ZIP、本地配置、数据库、原始验收日志和测试归档由 .gitignore 排除。API Key 与私人项目资料保存在本机。可公开的验收摘要和测试依赖的语言基线按现有规则纳入版本控制。

## 发布检查

维护者根据实际构建执行检查，为新归档生成校验值，确认包内含 LICENSE、第三方声明、隐私说明和用户指南，再发布 ZIP 与 SHA256 附件。历史报告中的通过结果只覆盖当时记录的文件及环境。[发布验收](docs/release/RELEASE-ACCEPTANCE.md) · [协议说明](docs/release/LICENSE-DECISION.md)
