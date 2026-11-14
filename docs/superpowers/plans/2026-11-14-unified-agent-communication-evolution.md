# Communication、统一调度与 Evolution 实施计划

> **For agentic workers:** 使用 superpowers:subagent-driven-development / executing-plans，按任务验证。
**Goal:** 让外部连接、统一目标和证据驱动进化进入现有 Asimi 产品。
**Architecture:** 领域执行器保持原审批语义；新连接器与统一编排记录采用追加式独立表。新 API 在 Service.Call 前置分派，不改变旧调用格式。
**Tech Stack:** Go/SQLite/DPAPI、Vue 3/TypeScript；复用本地依赖。
**Spec:** ../specs/2026-11-14-unified-agent-communication-evolution-design.md

## Global Constraints
- 公开 GitHub 只读真实验收。DEEPSEEK_API_KEY 只供隔离测试，禁止记录。
- 保留当前未提交成果；原工作树继续实施，不提交或推送。
- 不以工作区隔离代替系统用户隔离；连接器写入只做协议测试。
- 所有副作用逐次审批、精确资源绑定、期限、停止、重启不重放。
## Review Focus
- 连接配置/凭据/范围在审批后改变，必须阻断旧审批。
- 外部结果中的恶意内容不能提升权限或触发调用。
- 停止/重启和网络未知写入不能重试或误标完成。
- 技能组合不能通过依赖变化、关闭/废弃或提升工具权限绕过审核。
- 过期/冲突/归档记忆和跨工作区/个人数据不能进入召回。

### Task 1: Communication 领域
Files: 新 internal/communication/* 与 internal/app/communication*.go/test.go。
Consumes: Service.db/lifetime、credential.Vault、现有 transaction 和 mvp cancellation。
Produces: initializeCommunication() error、callCommunication(method,payload)(domain.Reply,bool)、stopCommunication() error；API ListConnections/SaveConnection/RevokeConnection/PrepareConnectorAction/ApproveConnectorAction/RejectConnectorAction/ListConnectorActions。
- [ ] 先写范围、审批一次、配置变更、回读、未知结果、重启不重放测试并运行失败。
- [ ] 实现 GitHub/GitLab、Microsoft Graph 邮件/日历、Slack、远程 MCP 契约与适配；无账户仅回环验收。
- [ ] 用公开 GitHub metadata 真实只读验证；写入需独立 GET 回读确认。
- [ ] 领域全量测试通过；给根代理准确 API 契约和中心接线点。

### Task 2: Evolution
Files: 新 internal/app/cognition_evolution*.go/test.go；小范围 cognition_* hooks。
Produces: initializeEvolution() error、callEvolution(method,payload)(domain.Reply,bool)；GetEvolutionInsights/RunMemoryMaintenance/ComposeCognitionSkills/ProposeSkillImprovement。
- [ ] 先写版本效果、失败经验、过期召回排除、固定依赖组合测试并运行失败。
- [ ] 实现事实指标、失败摘要与待审改进、组合依赖校验、长期记忆治理和个人资料边界。
- [ ] 验证次任务行为确实受效果/审核技能影响，保留之前闭环。
- [ ] 领域测试通过；给根代理中心接线点。

### Task 3: Unified Harness
Files: 新 internal/app/unified_agent.go/test.go；Service、新模式任务恢复与取消接线。
Produces: SubmitAgentGoal/GetUnifiedRuns/CancelUnifiedRun；子领域状态从实际记录读取。
- [ ] 测试普通聊天、公开 GitHub、开发与桌面路由，无审批不外发。
- [ ] 单入口转交现有领域；持久化任务绑定，记录来源与状态，停止和重启安全。
- [ ] 增加组合目标的明确步骤依赖与结果上下文，不执行模型生成的任意代码。

### Task 4: Product UI
Files: frontend/src/unified-agent*.ts、locales、main.ts 等最小入口。
- [ ] 展示普通模式目标、连接列表与账户配置、准确操作审批和事实结果。
- [ ] 展示进化指标、记忆治理和待审技能组合；处理 loading/error/empty。
- [ ] TypeScript、UI 交互测试、资源检查、Vite 构建。

### Task 5: Complete acceptance and report
- [ ] 全量 Go/vet、前端测试、原生桌面与既有开发流程回归。
- [ ] 独立 opt-in 的真实 DeepSeek 和公开 GitHub 端到端测试，输出 token 使用、网络调用、事实证据但不输出密钥。
- [ ] 独立代码审查、修复实质问题；记录源码/构建/日志哈希。
- [ ] 输出模块完成报告和真实/模拟/未实测矩阵，构建本地程序，明确安装包状态。
