# DeskPilot AI MVP 0.1 验收记录

日期 2026-11-11。结论 **CONDITIONAL GO**，限 Windows 11 25H2 x64 上已验证的功能。测试使用普通用户、隔离 profile、专用 UIA 测试窗口、回环协议夹具和合成截图。真实 Provider 请求数为 0。

| 场景 | 状态 | 证据 |
| --- | --- | --- |
| A 多轮对话、流式、附件、用量与历史 | VERIFIED | Wails stages 5–6，回环 Chat Completions / Responses。另有 Phase 1 离线回归。 |
| B DeepSeek 真实文本 | PENDING | Provider 适配器和 BYOK 可配置。需单独开启真实请求并使用自己的密钥。 |
| C 截图、遮盖、外发审阅与分析 | CONDITIONAL | Stage 7 共 36 项，合成像素，经真实 Vue 和回环 Provider 核验。最终版真实桌面截图与远程视觉待用户实测。 |
| D 测试窗口语义 UIA | VERIFIED | Stage 8 共 29 项，真实 Helper、原生 SetValue、语义 TypeText、Invoke、独立校验。最终一次聚焦成功，仅说明该次环境允许。 |
| E Notepad 识别、空白编辑 | CONDITIONAL | Go / C# 条件测试和测试窗口原生 Helper 回归通过。实际 Notepad 版本需用户在最终应用中主动验收。 |
| F 环境检测 | VERIFIED | Stage 9 查询七种固定运行时，记录检测结果。 |
| G 工作区分析 | VERIFIED | Stage 9 的隔离目录选择回调、真实文件树、搜索、目录排除、路径穿越拒绝。普通版原生对话框接入，验收自动化使用隔离目录回调。 |
| H Diff | VERIFIED | Stage 9 显示完整前后差异，错误哈希被拒绝。 |
| I 审批后文件写入 | VERIFIED | Stage 9 在本地 NTFS 进行事务替换，独立读回。Go 测试覆盖新建、事务锁、回滚、恢复副本、链接和同路径目标变化。 |
| J 审批后定向测试 | VERIFIED | Stage 9 实际 `go test ./...`。Job 测试核验取消清除子进程。其他项目运行时的兼容性仍需扩大验证。 |
| K 多步骤任务 | VERIFIED | Stage 9 执行项目、搜索、环境三步，持久化并审阅上下文。自动将模型建议转换为任务步骤和提案仍待实现。 |
| L Stop、崩溃恢复 | VERIFIED | Go 测试和 stages 8–10 验证撤销、消费、意图回滚、未知结果及不重放。 |
| M 双语与主题 | VERIFIED | 阶段 7 检查两种语言及明暗布局，阶段 9–10 验证语言切换和恢复。 |
| N 独立启动与历史恢复 | VERIFIED | 九个进程均正常退出，SQLite 可独占重开，窗口退出顺序符合约定。 |

最终便携目录 Wails 回归 193 项断言通过。原生 Windows UIA 仅操作专用测试窗口。Go 通过 135 个顶层测试和 134 个子用例，三个专用入口在常规测试中跳过。原生 Helper 测试单独通过。C# Notepad 策略 16 项、协议 3 项通过。三组前端测试、TypeScript、Vite、Go vet 和 `go mod verify` 均通过。SQLite v5、从旧版升级、失败回滚、单次消费和恢复由 Go 测试覆盖。

一轮 stage 9 隔离样本测得工作区分析 4 ms，定向 Go 测试 1,552 ms，环境诊断 979 ms。最终包的测量记录位于 stage 9 JSON。首 token、真实截图、UIA 查询、不同规模项目、页面切换和长时间内存占用仍缺性能基线。

最终 ZIP 解压后逐个核对 SHA-256，再从解压目录启动。证据位于 [Windows 验收汇总](mvp01-evidence/extracted/summary.json)、[包清单核验](mvp01-evidence/package.json)、[Go 测试](mvp01-evidence/go-tests.jsonl)、[Notepad 条件验收](mvp01-evidence/notepad-summary.json) 与 [双语检查](mvp01-evidence/i18n-check.json)。本轮观测到的 1412 为 0，历史结论保留 **CHARACTERIZED**。
