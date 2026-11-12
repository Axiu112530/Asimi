# DeskPilot AI · Phase 2D 验收记录

2026-11-11，结论为 **CONDITIONAL GO**。当前仅允许专用测试窗口中已通过的逐动作语义 UIA 操作。扩大应用白名单需要用户确认。

## 门禁

| 子阶段 | 结论 | 说明 |
|---|---|---|
| 2D-0 | PASS / 限定范围 | MTA UIA、受限树、进程与窗口指纹、固定 host。Notepad 有严格只读适配器，当前 Windows 版本的真实 Notepad 尚待用户手测。 |
| 2D-1 | PASS | SHA256 Proposal、单动作批准、有效期、重校验、SQLite v4、单次 CAS 和事件历史。 |
| 2D-2 | CONDITIONAL | 测试 host 的 UIA Invoke、Value 和语义 TypeText 通过。聚焦返回 UNKNOWN_RESULT，raw SendInput 与 Notepad 写入未开放。 |
| 2D-3 | PASS / 限定范围 | 独立效果核查、Stop/StopAll、存储故障、重启未知状态和不重放、Wails 原生审批通过。 |

## 定向测试

7 个 Go 包通过 93 个顶层测试及 110 个子用例。3 个独立 fixture 入口跳过，其中原生 driver 用例另行使用编译时 helper 散列在中等完整性进程运行并通过。Fake Driver 崩溃测试与原生测试分别记录。[逐项测试](phase2d-evidence/directed-tests.jsonl) · [统计](phase2d-evidence/test-counts.json) · [原生 driver](phase2d-evidence/native-driver.log)

覆盖未批准、错误 hash、重复/并发批准、过期、拒绝、进程创建时间变化、元素状态、危险输入、Stop 前/中/后、数据库事务失败、最终落库失败、取消落库失败以及恢复幂等。子进程在四个控制边界中断，恢复保留原始单次消费。旧文件工具的七边界崩溃、不可写、损坏库、busy、full 等原有定向测试同时通过。

原生 worker 的独立脚本通过协议异常、目标变化、失效元素和文字指纹、实际文本写入与独立 hash 核验、Submit/Reset、私有管道退出。[Worker 记录](phase2d-evidence/native-worker.log)

## 最终 Wails 运行

| Stage | PID | 断言 | 退出码 |
|---|---|---|---|
| 8 · Computer Control | 22848 | 29 | 0 |
| 7 · Phase 2C | 14108 | 36 | 0 |
| 1 · Phase 1 | 5640 | 58 | 0 |
| 2 · Phase 1 restart | 21816 | 10 | 0 |
| 3 · Phase 1 fresh | 11716 | 3 | 0 |
| 5 · Phase 2B | 21920 | 21 | 0 |
| 6 · Phase 2B restart | 25528 | 8 | 0 |

共 165 项断言。Stage 8 正向链包括固定启动、23 节点树、语义 TypeText、Submit、Reset、实际 Vue 准确文本审批和 Invoke 审批、单次消费、Stop/Resume 与只读检查。最终聚焦结果为 UNKNOWN_RESULT，测试通过仅表示结果报告符合事实。[完整汇总](phase2d-evidence/native-186daec79e7e4561bf7b7c8703e134da/summary.json) · [控制断言](phase2d-evidence/native-186daec79e7e4561bf7b7c8703e134da/stage-8.json)

所有最终进程保留 stdout、stderr 和有序 runtime 记录，SQLite 可以独占打开。按本次二进制目录及隔离 profile 核对，残留进程为 0。1412 观测为 0，结论仍沿用 CHARACTERIZED。[资源释放](phase2d-evidence/process-release.json)

## 兼容性与构建

Vue 类型检查、生产构建、真实 Vue 控制组件测试、旧 AI store 回归、Go vet 和 go mod verify 通过。两种语言各 844 个键，原 716 个键和值与插值参数保留。300 份旧报告和 evidence 散列一致，原 Phase 2C exe 保留。[双语](phase2d-evidence/i18n-check.json) · [保留证据](phase2d-evidence/preservation.json) · [v1/v2 迁移](phase2d-evidence/schema-compatibility.json)

构建使用 `scripts/build.ps1 -OutputDirectory build/bin/phase2d`。Wails 使用 `scripts/accept-phase2d.ps1 -SkipBuild`，独立 worker 使用 `validation/phase2d/native-worker-smoke.ps1 -Bin <绝对构建目录>`。所有可写 profile 位于 `.local-validation/phase2d`，回归仅连接本地 fixture。实际 Notepad 测试需由用户主动启动空白窗口，先检查身份与树，当前配置会拒绝写入。

## 限制与决策

全局输入、坐标点击、快捷键、Notepad 编辑、自动动作规划及扩大应用范围均未通过本次开放验收。未知记录没有自动结案功能，用户需重新观察并独立提出新动作。测试 host 的兼容性不能用于推断其他应用。聚焦受 Windows 策略限制，仍需真实用户手动确认其可用性。

最初的隐藏 host 超时已修复，聚焦不确定结果保留，原生 UIA 3 秒时限调整为 5 秒。这些发现记录在 [工程报告](PHASE-2D-REPORT.md)。

当前门禁允许继续使用专用测试 host 中已通过的能力。下一阶段及应用范围扩展等待用户确认。
