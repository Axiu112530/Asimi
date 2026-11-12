# DeskPilot AI · Phase 2D 工程报告

日期为 2026-11-11。最终结论为 **CONDITIONAL GO**，开放范围限专用测试窗口的逐动作 UIA 操作。Notepad 编辑、全局输入和扩大应用范围尚未通过验收。

## 实现模块

| 模块 | 实现与当前范围 |
|---|---|
| UIA Runtime | C# 单请求 MTA worker，CacheRequest、有界目标子树、500 节点/12 层、属性长度及 256 KiB 响应限制。5 秒 worker 时限，COM 对象只存活于 worker。 |
| 专用测试窗口 | WinForms STA，TextInput、Submit/Reset、ResultLabel、Checkbox、滚动区域。固定入口启动，私有 stdin EOF 正常退出，无文件、网络或命令功能。 |
| Windows Driver | 规范路径、镜像散列、PID/创建时间、同 SID/会话、中等完整性、无 UIAccess、HWND/类名/标题/矩形核验。helper 镜像、配置和全部目录祖先持有句柄防替换。 |
| Notepad Adapter | 严格识别受支持的 Microsoft.WindowsNotepad 包、AUMID、可信安装路径和窗口类。只读检查隐藏编辑器内容，旧 System32 Notepad 不在支持范围。当前版本真实 Notepad 操作尚未验收。 |
| Proposal / Policy | 固定动作枚举，目标与元素指纹、准确文本、预期结果、观察时间、45 秒有效期、policyVersion。拒绝未授权 selector、Pattern 缺失、密码/不可用/不可见控件、模态和截断树。 |
| Action Hash | 完整 Proposal 规范化 JSON 的 SHA256，另有 targetHash。批准只携 actionId/actionHash，参数由 Go 内存原件提供。 |
| Approval Broker | 单动作审批，短事务保存批准。执行前重新检查原生身份与元素。带 hash、目标和有效期约束的 CAS 消费审批，与 EXECUTING 意图同事务。 |
| UIA Execution | 专用 TestInput 的 SetValue 与语义 TypeText，固定 SubmitButton/ResetButton 的 Invoke。逐动作独立批准。 |
| Focus | 调用单次 SetForegroundWindow，随后独立核验前台窗口。当前自动化环境中有被 Windows 拒绝的情况，记录 UNKNOWN_RESULT，不重试。 |
| SendInput | 全局输入尚未通过焦点变化的原子绑定门槛。rawInput=false，未开放坐标点击、快捷键和键盘注入。 |
| Verifier | 单独 UIA 读取核验文字 SHA256 与 ResultLabel，启动核验实际窗口身份与可见性，聚焦核验实际前台 HWND。 |
| Execution Journal | SQLite v4 独立三表保存授权、消费、状态事件、hash、字符数、时间及结果码。树、标题和输入字面值仅在内存。 |
| Recovery | 重启取消未执行批准，EXECUTING/VERIFYING 转 RECOVERY_REQUIRED。UNKNOWN_RESULT 保持待人工核查，不自动重放。 |
| Emergency Stop | 原子停止标记与独立 context 取消，随后持久化待批准取消。即使 SQLite 失败也撤销内存授权。StopAll 同时取消旧文件任务和 AI 请求，并报告合并错误。 |
| Vue / I18N | Computer 页增加目标应用、UI Inspector、单动作批准、准确文本、时间线、Stop/Resume。支持原有双语/主题。存储或通信错误发生在批准之后时显示结果不确定。 |

实现文件集中于 `internal/computer`、`internal/app/control_service.go`、`internal/app/control_store.go`、`frontend/src/control-panel.ts`。设计详见 [控制架构](COMPUTER-CONTROL-ARCHITECTURE.md)、[审批安全](ACTION-APPROVAL-SECURITY.md) 和 [UIA 实现](WINDOWS-UIA-IMPLEMENTATION.md)。

## 生命周期与一致性

延续 Phase 2A 的窗口优先关闭协议。关闭任务准入后先发出控制取消，等待受管理调用结束，再保存待批准取消、正常关闭自有 host、释放驱动并关闭数据库。worker 超时只终止该次自有 worker。专用 host 使用私有管道关闭，未使用共享进程清理。

v4 为新迁移，原 v1/v2/v3 SQL 保留。迁移整体事务保护，冲突失败回滚至 v3，修复冲突后可重复启动至 v4。批准、消费/意图、最终状态和事件各自原子提交。原生 I/O 位于这些事务之外。最终落库失败保留可恢复的 EXECUTING/VERIFYING，调用方收到错误。

重启恢复只处理事实状态，不重新规划。审批消费后的意图即使尚未实际调用 Windows，也保守进入 RECOVERY_REQUIRED。journal 中的 hash 可以关联原批准，但不足以重建完整目标。当前处理方式为用户重新检查窗口并独立提出新动作，旧未知记录持续保留。

## 验证结果

| 验证 | 结果与证据 |
|---|---|
| Go 定向回归 | 7 包、93 个顶层测试、110 个子用例通过。3 个仅由独立运行器启动的 fixture/原生用例跳过。Go vet、go mod verify 通过。[测试计数](phase2d-evidence/test-counts.json) · [逐项记录](phase2d-evidence/directed-tests.jsonl) |
| 受控崩溃 | Fake Driver 子进程在意图提交、效果返回、验证返回、最终事务提交四个边界强制退出。前三种恢复为 RECOVERY_REQUIRED，最终已提交保持 COMPLETED，消费记录保留，恢复重复执行无重放。 |
| SQLite / 并发 | 执行意图失败无驱动调用，最终记录失败无成功回复，Stop 写入失败仍撤销授权，StopAll 继续取消其他 worker，8 个并发批准只有一个派发，v4 DDL 冲突回滚和重复升级通过。 |
| 原生 Driver | 中等完整性专用 host，23 个节点，实际 SetValue 中文文本与独立 hash 核验、Submit 与独立状态核验、重复启动拒绝及正常退出通过。[原生驱动](phase2d-evidence/native-driver.log) |
| 原生 Worker | 严格协议拒绝、进程/元素变化、已变更输入指纹拒绝、Value/TypeText、Submit/Reset 与 EOF 正常退出通过。[原生 worker](phase2d-evidence/native-worker.log) |
| Wails 最终构建 | 7 个独立进程、165 项断言通过，其中控制 Stage 8 为 29 项。真实 Vue 输入审批、Invoke 审批与独立原生验证通过。Phase 1/2B/2C fixture 回归通过。[最终原生汇总](phase2d-evidence/native-186daec79e7e4561bf7b7c8703e134da/summary.json) |
| 退出资源 | 7 个进程退出码均 0，stderr/runtime 保存，关闭事件顺序正确，SQLite 可独占重开。最终隔离 profile 的 Wails、helper 和 WebView2 残留为 0。[进程核查](phase2d-evidence/process-release.json) |
| Vue | 现有 AI store 六项场景和真实 Vue 控制组件测试通过，涵盖准确审批、拒绝、过期、未知/存储结果、Stop/Resume、延迟查询、全局 StopAll。类型检查与构建通过。[前端记录](phase2d-evidence/frontend-tests.log) |
| 旧成果 | 300 份历史报告/证据 SHA256 一致，Phase 2C 原 exe 保留。双语各 844 键，原 716 键和值保留，插值参数一致。[保留校验](phase2d-evidence/preservation.json) · [双语校验](phase2d-evidence/i18n-check.json) |

真实操作仅针对专用测试 host。Fake Driver 的崩溃与策略测试不能证明任意应用执行能力。Phase 2B/2C 回归使用回环协议服务和合成图片，本阶段外部模型调用数为 0。

## 调试发现与修复

首次 Wails 控制启动因 host 继承隐藏启动配置而超时，最终结果保持 UNKNOWN_RESULT。将用户已批准的专用交互窗口显式设置可见后，后续独立原生及 Wails 运行通过。[初始证据](phase2d-evidence/diagnostics/hidden-fixture.json)

高负载原生 UIA 曾触发 3 秒时限，结果保留未知。调整为有界 5 秒 worker、6 秒只读、20 秒完整动作上下文后，最终验证通过。

聚焦请求在最终环境被 Windows 拒绝，独立验证没有产生成功。最终 Stage 8 明确核验“已验证完成或如实未知”两种结果，并记录实际为 UNKNOWN_RESULT。此项不代表聚焦成功。[未验证聚焦证据](phase2d-evidence/diagnostics/focus-not-verified.json) · [最终详细结果](phase2d-evidence/native-186daec79e7e4561bf7b7c8703e134da/stage-8.json)

定向回归还发现旧消息查询在时间戳相同时按随机 ID 排序，偶发将助手回复排在用户消息之前。最小修复改为时间戳相同时按 SQLite 插入 rowid 排序，保留历史数据，增加确定性测试。此项属于保护既有会话行为的兼容修复。

## 1412 与最终门禁

1412 继续为 **CHARACTERIZED**，引用 Phase 2A 的调查与证据。本次最终七进程观测为 0 次，未据此升级为 FIXED。Wails 版本继续为 v3.1.0。

2D-0 和 2D-1 的专用 host、只读/身份与审批基础通过。2D-2/2D-3 的专用 host 语义操作、验证、停止和恢复通过，聚焦受 Windows 前台限制。Notepad 写入、raw SendInput 和人工消除恢复状态的工作流仍未完成。不同 Windows/Notepad 版本、多显示器与混合 DPI 的实机矩阵尚未覆盖。模型生成动作计划的接入留待后续授权。

最终 **CONDITIONAL GO** 仅用于当前专用测试窗口的已验收能力。扩大应用范围和下一阶段均等待用户确认。工作区沿用 `codex/phase2b-byok` 及既有未提交成果，没有提交或推送。

最终程序位于 `build/bin/phase2d/DeskPilotAI.exe`，应与两个 helper 和固定 `.config` 一同使用。主程序 SHA256 为 `2CC26B3EA2C83E746FC9EBDAE9E0D4C7645E5FDE5D47973AC84C24C0D90EBFD0`。
