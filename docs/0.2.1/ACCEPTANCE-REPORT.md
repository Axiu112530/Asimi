# DeskPilot 0.2.1 验收报告

验收主机为 Windows 11 Home 25H2 x64，Build 26200.9974，WebView2 157.0.4341.46。使用 Go 1.26.10、Wails 3.1.0 和现有锁定的 Vue 依赖。所有程序级验收从独立解压目录运行，使用专用数据目录与生成的测试项目。

## 最终候选包

- 文件为 `build/bin/DeskPilot-Community-0.2.1-win-x64.zip`。
- ZIP SHA256 为 `C247227298E0396F386168B8016927B49D3C48D8A1B5C3748C4FE907302E24A1`。
- 可执行文件 SHA256 为 `EDBEDFB84CB4D507DA6F9A914A28EF7A2A1C7DF0CE2D167D2FA7FAEA7DACB8B8`。
- 包含程序、两项 Helper、两个固定配置、五份指南、一份第三方声明和文件散列清单，共 12 个文件。ZIP 旁另有归档 SHA256 文件。

[归档和独立解压记录](evidence/package.json)

## 已完成的验证

| 项目 | 状态 | 实际结果 |
| --- | --- | --- |
| Go 回归、静态检查和依赖完整性 | VERIFIED | `go test ./... -count=1`、`go vet ./...` 和 `go mod verify` 通过。 |
| 前端构建和渲染回归 | VERIFIED | TypeScript、Vite 构建，以及 AI 会话、电脑审批、项目状态三组渲染和存储测试通过。 |
| 双语资源 | VERIFIED | 原 844 个键及其文本保留，主资源每种语言 1,073 个键，新增三组资源分别为 49、59、22 个键，占位符一致。 |
| 首次设置、重启和旧配置升级 | VERIFIED | 六次运行共 46 项断言通过，包括离线、中英文切换、真实连接、DPAPI 保存、默认模型、限制和来源、恢复默认、旧 Provider 与任务历史。两份原始 0.2 测试配置仅使用副本升级。 |
| 既有 Windows 回归 | VERIFIED | 九次运行共 193 项断言通过，包括审批、取消、历史、屏幕预览和确认、合成图外发、文字追问、专用原生窗口的批准和回读，以及退出后数据库释放。 |
| 回环 Go/Git Agent | VERIFIED | 两次运行共 34 项断言通过。检查自动选源、Git 改动、完整 Diff、命令独立审批和重启恢复。 |
| 真实 DeepSeek Go/Git Agent | VERIFIED | 两次运行共 35 项断言通过。使用向导保存的 DPAPI 凭据完成补丁、Go 测试、依据实际输出总结的三轮请求。已有本地注释、Git index 和回归测试文件保持一致。 |
| 普通启动和关闭 | VERIFIED | 从独立工作目录正常启动两次，仅注册一个产品服务绑定，正常关闭后均可独占重新打开数据库。 |
| 250、2,000、11,000 文件扫描 | VERIFIED | 扫描按现有文件数、字节和结果上限终止，测量结果见性能报告。 |
| 最终包两小时采样 | VERIFIED | 最终可执行文件连续运行 7,200.020 秒，完成 105 轮流式对话、20 个本地任务、5 次请求取消和 5 次任务暂停恢复循环，记录 233 组进程样本。关闭后数据库可独占重新打开，完整性检查通过。 |
| 实际桌面视觉、空白 Notepad | PENDING | 需要用户主动在最终包中操作并核对。 |
| 其他 Windows 11 版本及 Windows 10 | PENDING | 本轮只对上述实际主机给出验证结论。 |

原生功能验收共 308 项断言。合成图、假驱动和回环测试各自证明其覆盖的协议与状态行为。最终包的实际桌面及 Notepad 手动验收步骤见随包指南。

[设置和升级](evidence/native-cca84eddf7b84708b93ec7e99b7c6007/summary.json) · [Windows 回归](evidence/regression-b449ecebeba946c0a279c85adc312e98/summary.json) · [回环 Agent](evidence/agent-loopback-f7f46d7afef8419fae3aa56ce79bccd8/summary.json) · [真实 Agent](evidence/agent-live-1b04ca5db11c4e42918e4880449e1d89/summary.json) · [普通启动](evidence/ordinary-launch.json)

[长测原始结果](evidence/soak-9d57debd74ac4ac092656314cefa9e8c/summary.json) · [资源范围与测试覆盖](PERFORMANCE-REPORT.md) · [数据库完整性](evidence/database-integrity.json) · [最终核对](evidence/final-checks.json)

## 外部请求和保留的失败记录

最终包执行了一次固定短消息连接测试和三次项目规划请求。三轮规划的 Provider 用量分别为 1,365、1,372、1,527 Tokens。按保存的峰时费率快照估算，规划请求共 USD 0.000872832。该数值是本地估算，实际扣费以供应商账单为准。预设费率于本轮对照官方资料核查。[真实请求记录](evidence/agent-live-1b04ca5db11c4e42918e4880449e1d89/stage-12.json) · [DeepSeek 官方费率](https://api-docs.deepseek.com/quick_start/pricing/)

真实连接验收由本应用的 WebView 向导触发。专用验收启动器在原生侧一次性替换测试占位符，以内存中的环境凭据调用相同的生产连接、DPAPI 保存和默认选择逻辑，随后清除输入。实际密钥输入前端的普通路径由独立 DPAPI 与回环测试覆盖。

本轮迭代共发起 17 次真实外部请求，包含 4 次连接检查和 13 次规划请求。早期两个试验分别在第二轮返回格式无效计划、重复补丁，后端拒绝了对应动作，原始记录保留。之后补充已验证修改的上下文说明，最终版本的真实修复以三轮完成。模型输出仍需结构校验和用户审批。[无效计划记录](evidence/agent-live-55ef50623703401292bcf7186cdbcf34/stage-12.json) · [重复补丁记录](evidence/agent-live-0111d7e88ed548c9815c4fdf1e78f900/stage-12.json)

## 审查和工程保护

独立代码审查确认并修正了三项问题。Git 读取缺失的工作区 `.gitattributes` 时会回退到索引中的校验过的 blob。实际步骤上限和最后一轮规则会进入已审阅且实际发送的上下文。降低图片上限后，历史附件重新外发同样受限，本地历史预览保持可读。定向回归和完整 Go 测试通过。

基线分支为 `codex/phase2b-byok`，HEAD 为 `4accd35e6b7a801274e034adc103f59df37e686c`。历史迁移仍为 v1～v6，本轮使用兼容 JSON 字段增加配置和证据。既有版本便携包和历史报告保留。1412 继续为 CHARACTERIZED，最终功能验收日志中计数为零。

## 发布判断

当前为 **CONDITIONAL GO**，用于受控的小范围候选版试用。公开分发所需的应用许可证由所有者决定。真实桌面、空白 Notepad 和其他 Windows 版本需各自完成现场验收。
