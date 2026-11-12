# DeskPilot 0.2 产品报告

2026-11-12，**CONDITIONAL GO**。0.2 的核心开发闭环已在最终解压版上验证。一个自然语言目标驱动三轮真实 DeepSeek 计划，生成可审阅补丁，审批后修改实际 Go 文件并运行测试，再用实际退出码和输出生成总结。应用重启后保留计划及动作记录。

| 功能 | 状态 | 已交付范围 |
| --- | --- | --- |
| 真实 Provider、用量、价格估算 | VERIFIED | 现有 BYOK 适配器支持文本、带上下文的 JSON SSE、取消和 Responses 合成视觉。0.2 预设记录可编辑费率。 |
| Project Context Builder | VERIFIED | 本地文件候选、符号和依赖；只发送审阅后的选定上下文。路径、字节和请求数有界。 |
| 自动计划、结构化补丁、测试建议 | VERIFIED | 计划由模型生成并由后端验证。真实 Go 修复与测试完成，支持继续审阅下一轮结果。 |
| 逐文件 Diff、TxF 与回读 | VERIFIED | 完整基线哈希与原始身份，动作精确审批、事务写入及独立核验。文件删除不开放。 |
| 固定命令与 Job Object | VERIFIED | Go、Node.js、Java、Python 已测试代表性模板。支持分流输出、时限、取消与进程树终止。 |
| .NET 及 pytest 运行 | PENDING | 本机缺少相应 SDK 或依赖。 |
| 恢复、停止、模型预算 | VERIFIED | 请求预算发送前持久化。未知动作保留，暂停或重启不自动重放。 |
| 电脑任务证据关联 | IMPLEMENTED | 用户选择的视觉请求和语义 UIA 动作可关联任务。保留现有逐动作审批及时间线，专用测试窗口原生能力通过。 |
| 模型自动提出 Computer Action | PENDING | 本版电脑动作从用户主动观察和提案开始。任务模型能使用关联结果状态继续分析。 |
| 真实用户桌面视觉、Notepad 写入 | CONDITIONAL | 已有入口与安全门槛，仍需用户在最终应用中主动验收。 |
| 其他应用写入、全局输入、坐标点击 | UNSUPPORTED | 专用测试窗口继续按已验证的语义 UIA 范围运行。 |
| 双语、历史与便携交付 | VERIFIED | 保留现有资源。最终 ZIP 已解压、检查成员散列并独立启动。 |
| Community 准备 | IMPLEMENTED | 已补充安装、架构、贡献、安全和发布说明。许可证与公开发布待项目所有者决定。 |

本轮沿用 Phase 0～2D 与 0.1 的安全执行链、SQLite、版本固定及窗口优先退出。v6 仅新增电脑任务证据表，计划保存在原任务状态中。StopAll、Provider 凭据与端点匹配、未知结果恢复三个边界经独立审查并补充测试。

继续验证用户桌面、实际 Notepad 和更复杂的工作区。模型驱动的电脑动作规划，以及无需在 `Developer` 中手动获取 Git 状态的 Agent 上下文仍可改进。性能采样覆盖当前本机样例，长时间资源占用和不同 Windows 版本待扩展。

[验收报告](ACCEPTANCE-REPORT.md) · [安全审查](SECURITY-REVIEW.md) · [性能基线](PERFORMANCE-BASELINE.md) · [便携版入口](START-HERE.md)
