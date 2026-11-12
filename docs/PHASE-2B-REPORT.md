# DeskPilot AI — Phase 2B 工程报告

2026-11-11，Asia/Shanghai。结论为 **CONDITIONAL GO**。Phase 2B 已交付可配置的 BYOK 构建，并通过 Windows 本地协议与应用回归验收。真实服务商的文本、视觉和用量回包仍待用户在应用中主动验证。 [验收清单](PHASE-2B-ACCEPTANCE.md) · [最终验证](phase2b-evidence/final-verification.json)

| 模块 | 实现 |
|---|---|
| Provider Core | Go 中的统一文本、图片、用量和错误类型。Responses 与 Chat Completions 使用独立请求及 SSE 解析器。Responses 使用 `store=false` 和本地上下文。 |
| Custom Provider | 配置多个端点和模型、独立维护三态能力及价格。允许无密钥网关。HTTPS 使用证书验证。HTTP 限于 loopback 或经确认的私有 IP。端点或协议变化会清除原密钥绑定。 |
| DPAPI Vault | 当前 Windows 用户范围加密。SQLite 单独保存密文，支持替换、删除及存在性查询。损坏密文阻止请求。前端密码框提交后清空。 |
| Assistant | 按所选供应商发送用户确认的多轮上下文。按请求 ID 和序号渲染流式 Markdown。取消传到 Go HTTP 上下文，已收到的文字随终态保存。切换供应商发送已有上下文前须确认。 |
| Vision | 用户主动选择 PNG、JPEG、WebP 或非动画 GIF。校验内容、字节数和尺寸，重新编码后存入应用附件目录。SQLite 保存附件关系及哈希。发送前显示传输及费用提示。 |
| Model Playground | 复用正式适配器，独立会话和配置，支持文本、图片、流式与普通请求。可查看 HTTP 状态、首个可见增量耗时、总耗时、用量和费用。历史按所属页面打开。 |
| Usage & Pricing | 记录供应商报告的可空 Token、请求状态和价格快照。缓存输入按子集计算。未返回的用量及缺失价格的费用显示未知。 |
| 本地持久化 | Schema v3 事务迁移，复用原有会话与消息。中断后未结算的网络请求标为结果未知。删除会话清理内容，失败的附件清理保留待办，可在重试或重启后结算。 |
| 安全及生命周期 | 真实对话接收文本和选定图片，输出文字并拒绝工具调用。原 Mock 文件流程仍由审批及恢复引擎管理。退出时取消 HTTP 并等待工作线程保存，再沿窗口优先顺序关闭。 |

协议实现依据为 OpenAI 的文本、流式、图片与本地会话文档。兼容网关通过对应的可配置 HTTP 协议接入。不同网关对流式 `stream_options`、视觉和用量字段的支持需逐端点验证。 [Provider 架构](PROVIDER-ARCHITECTURE.md) · [密钥安全](CREDENTIAL-SECURITY.md) · [用量与计价](USAGE-AND-PRICING.md)

## 验证结果

- 5 个定向 Go 包通过，共 73 个顶层测试和 85 个子用例。覆盖两种协议、SSE 分块和中断、取消、端点更换、密钥替换与损坏、图片验证、拒绝工具调用及失败结果的用量保留、迁移回滚和 Phase 2A 崩溃边界。 [Go 测试](phase2b-evidence/directed-tests.log)
- 5 项 Vue Store 回归通过，包括 5000 个早到增量、并发请求关联、取消、跨会话附件以及 Playground 历史。类型检查、Vite 和 Windows 构建通过，Go vet 和模块校验通过。 [前端测试](phase2b-evidence/frontend-tests.log) · [构建](phase2b-evidence/build.log)
- 四次 Wails 原生进程通过 97 项断言。新流程 29 项，包含本地 HTTP 图像传输、流式终态、取消部分输出、密钥输入清空、Playground 及重启。Phase 1 功能和历史恢复回归 68 项。四个进程均以 0 退出，SQLite 可独占打开，生命周期顺序正确，stderr 中 1412 计数为 0。 [原生验收](phase2b-evidence/native-4ba7e82f74004c1b8a239b985bb1245b/summary.json)
- 双语各 605 个键，Phase 2A 的 414 个键完整保留。v1/v2 SQL 哈希一致，214 份 Phase 0、1、2A 报告及验收证据的哈希一致。为 WebP 解码新增固定版本 `golang.org/x/image v0.45.0`，现有依赖版本保持固定。 [兼容性](phase2b-evidence/compatibility.json) · [双语检查](phase2b-evidence/i18n-check.json)

## 待验收

本轮使用项目隔离数据目录、测试密钥和 loopback 协议服务。图片测试验证真实选定文件的导入与请求传输，服务返回预设文字。真实模型理解图片、商业端点的返回及收费由用户主动验收。Windows 同一用户下的 DPAPI 往返、替换、删除与损坏密文处理已实测。另一 Windows 用户上下文尚未实测。

Anthropic 和 Gemini 原生协议留待后续阶段。带图上下文由本地字节数、像素和图片数量约束，费用及视觉 Token 以供应商返回和配置价格为准。1412 继续沿用 Phase 2A 的 CHARACTERIZED 结论。

工作区分支为 `codex/phase2b-byok`。验收构建位于 [DeskPilotAI.exe](../build/bin/DeskPilotAI.exe)。Phase 2C 等待用户确认。
