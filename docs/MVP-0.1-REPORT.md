# DeskPilot AI MVP 0.1 工程报告

日期 2026-11-11。**CONDITIONAL GO**，可在已验收 Windows 11 x64 环境试用离线工作台、工作区和逐动作审批。真实 Notepad 写入、DeepSeek 和真实桌面视觉需按验收条件单独确认。

| 功能 | 状态 | 当前范围 |
| --- | --- | --- |
| Home、首次启动、双语主题 | VERIFIED | 使用现有 Provider 配置页与离线跳过。首次启动、语言切换和重启通过。 |
| Chat、BYOK、DPAPI、SSE、Usage、Pricing | VERIFIED | 复用 Phase 2B，回环请求验证协议和实测用量。未知价格保持 Unknown。 |
| 真实 DeepSeek | PENDING | 能力配置已接入。外部实测未开启。 |
| Screen Capture / Vision | CONDITIONAL | 复用 Phase 2C。最终版通过合成图像、预览、裁剪、遮盖、审阅及回环 Vision 测试。 |
| Computer Control | VERIFIED | 真实专用窗口 UIA 的启动、SetValue、语义 TypeText、Submit、Reset、停止与验证。 |
| Notepad | CONDITIONAL | 实现身份、单一空白可写 Document、精确审批、独立回读。实际 Notepad 写入由用户主动验收。 |
| 其他应用 | PENDING | 可通过用户主动截图观察。任意应用的受限 UIA Inspector 尚未纳入本版。 |
| 全局输入、坐标点击 | UNSUPPORTED | 保持禁用。 |
| Workspace 与 Developer | VERIFIED | 目录树、搜索、语言和依赖清单、Go 符号和调用关系、其他语言词法提示、读取、Diff、逐文件创建和 NTFS 事务替换。 |
| 模型代码修改建议 | IMPLEMENTED | 可审阅源代码并使用既有 Provider 请求建议。用户将拟修改内容送入本地精确 Diff，再独立审批。模型输出自动导入补丁及测试建议仍待实现。 |
| Environment | VERIFIED | 七种运行时固定探测、路径发现、版本、诊断和官方来源。安装为人工流程。 |
| 安装执行 | PENDING | 有安装指导和 Go 命令预览，应用内受控安装及网络依赖恢复仍待实现。 |
| Command Engine | VERIFIED | 固定命令模板、已审批的实际 Go 测试、进程树取消、内存/CPU/时间/输出限制。其余项目命令兼容矩阵待验收。 |
| Agent Tasks | VERIFIED | 持久化本地只读计划、步骤、停止、恢复和结果。支持关联文件/命令提案及预览后发送模型分析。 |
| 自动模型编排、Computer Task 关联 | PENDING | 模型分析与动作审批分开。任务当前支持手动关联文件和命令，Computer 保留独立时间线。 |
| Skills | IMPLEMENTED | 八项内置能力与启用状态、Schema、工具清单。第三方扩展执行不开放。 |
| SQLite v5 与退出 | VERIFIED | 事务迁移、恢复、旧版审批链和窗口关闭顺序通过。 |
| 便携交付 | VERIFIED | 独立程序、两个固定 Helper、固定配置、指南与 SHA-256 清单。WebView2 与 .NET Framework 由 Windows 或官方运行时提供。 |
| Windows 10 与其他 Windows 11 版本 | PENDING | 当前测得 Windows 11 25H2 x64（26200.9711）。 |

新增 Go 代码集中在 `internal/developer`、`internal/command` 和 `internal/app/mvp_*`。保留 Phase 0～2D 的 Provider、视觉、审批和执行恢复链。新增产品工具目录描述共享安全边界，文件和命令使用同一审批 Broker，历史控制及文件创建沿用既有 Broker。统一 Stop All 覆盖新旧任务。v1～v4 SQL 保留，v5 提供产品工作台持久化。

最终验收通过 193 项 Wails 断言以及 135 个 Go 顶层测试和 134 个子用例。假驱动覆盖崩溃和存储失败。回环 Provider 覆盖协议、流式、用量、定向图像审阅和历史。Windows 实测覆盖专用 UIA、NTFS 事务写入、实际定向测试和进程树取消。对真实外部模型发出 0 次请求。详见 [验收记录](MVP-0.1-ACCEPTANCE.md) 与 [安全模型](SECURITY-MODEL.md)。

文件写入依赖本地 NTFS 事务，以隔离目标身份变化。应继续验证不同文件系统和 Windows 版本，并评估 TxF 长期替代方案。下轮优先完成真实 Notepad 与 DeepSeek 验收，扩大运行时命令兼容矩阵，再实现模型建议到审批提案的联动、只读应用观察和安全安装。继续保留 **1412 CHARACTERIZED**。
