# DeskPilot 0.2 验收报告

2026-11-12，**CONDITIONAL GO**。最终便携 ZIP 解压后通过散列核对。真实 Provider 和真实代码修复均使用解压后的 `DeskPilotAI.exe`。真实桌面及 Notepad 需要用户在应用内手动验收。

| 场景 | 状态 | 结果 |
| --- | --- | --- |
| A 真实 DeepSeek | VERIFIED | 官方 `deepseek-flash` 共 4 次。Chat Completions 文本、带多轮上下文的 JSON SSE、取消及用量，Responses 合成图片理解均通过。取消后服务商未返回用量，费用保持未知。协议错误和超时由回环测试覆盖。 |
| B 真实代码修复 | VERIFIED | Windows 原生任务走三轮真实规划。模型建议修复 Go `Add` 的减法错误，Vue 展示准确 Diff，用户动作入口批准后 NTFS 事务提交并重读。模型随后提出定向 `go.test`。程序要求单独确认网络风险和精确审批，再运行实际测试。退出码为 0，模型在下一轮分析真实测试输出并总结。独立读回源文件，测试文件哈希未变。 |
| C 真实环境检测 | VERIFIED | 版本检查与命令兼容夹具覆盖 Go、Node.js、npm、Java、Python、Git 的本机可用路径或诊断。Go、Node.js、Java、Python 的代表性固定命令实际运行。缺少 .NET SDK 和 pytest。 |
| D 真实桌面视觉 | CONDITIONAL | 本机原生回归验证合成截图的裁剪、遮盖、审阅和后续文本追问不附图。官方 DeepSeek Responses 验证合成图像。用户主动选择真实显示器及上传仍待验收。 |
| E 真实 Notepad | PENDING | 保留空白、单一可写 ValuePattern 文档门槛。当前轮仅操作专用测试窗口。请通过应用按手动流程验证当前 Notepad 版本。 |
| F 停止与恢复 | VERIFIED | 专用测试覆盖暂停取消正在进行的请求、预算保留、StopAll 拒绝新请求、单次消费、过期、目标变更、重启恢复未知结果并禁止自动重放。完整任务重启恢复三轮和两个实际动作。 |
| G 多语言 | CONDITIONAL | 英文默认、中文切换、重启和资源占位符通过。0.2 真实修复使用英文目标，未单独用中文真实请求验收模型回复语言。 |

真实请求合计 **7 次**。6 次有完整用量，共 **3,774 tokens**，按记录的 DeepSeek 峰值费率估算约 **0.001452 USD**。取消请求的实际用量与费用未知。Provider 凭据未纳入证据。当前费用以服务商账单为准。 [官方费率](https://api-docs.deepseek.com/quick_start/pricing/)

Go 完整回归通过 156 个顶层测试和 150 个子用例，5 个依赖专门环境的入口跳过。最终解压版另通过 193 项 0.1 兼容断言、27 项回环 Agent 断言及 27 项真实 Agent 和重启断言。回环与假驱动验证请求体、取消、错误和审批保护。Windows 原生证据验证文件写入、定向测试、专用 UIA 测试窗口及关闭顺序。Vue 回归、TypeScript、构建、Go vet、依赖校验均通过。

- [便携包散列](evidence/package.json)
- [真实 Provider 请求与用量](evidence/provider-live.json)
- [真实模型修复与重启](evidence/live-1f1bd16fdccc491694d4a6e9f8a95f14/summary.json)
- [实际修复每轮详情](evidence/live-1f1bd16fdccc491694d4a6e9f8a95f14/stage-12.json)
- [回环 Agent 原生验收](evidence/loopback-16ef676307e1482d92e2a674fc219f06/summary.json)
- [0.1 兼容回归](evidence/regression-01/summary.json)
- [Go 测试](evidence/go-tests-final.jsonl)、[双语校验](evidence/i18n-check.json)、[命令兼容性](evidence/command-compatibility.md)
- [真实桌面与 Notepad 手动验收](COMPUTER-MANUAL-ACCEPTANCE.md)

1412 继续沿用 **CHARACTERIZED**。此次程序使用 Wails v3.1.0。
