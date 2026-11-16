# Asimi Communication、统一调度与 Evolution 完成报告

日期 2026-11-14（北京时间）。交付范围为当前 Windows 工作树，基线提交 `4ee9051a3f126801c8f2ffd2a9026621d44edb36`。本报告对应本地验收构建。

| 用户要求 | 已交付 | 当前范围 |
| --- | --- | --- |
| Communication | 普通模式提供连接配置、令牌存储、账户验证、精确资源授权、逐次审批、撤销、取消、超时和结果记录。GitHub、GitLab、Microsoft Graph、Slack 和 MCP 共用连接器契约。 | 真实外部服务验收采用公开 GitHub 只读。其余适配器和写入验证使用本机协议测试。 |
| 六大能力统一协作 | 普通模式的「目标」收录聊天、开发、桌面和连接器任务。明确的 GitHub 公开仓库 URL 可自动生成匿名读取提案，审批后校验结果。连接器事实可以经单独审阅后交给模型总结。 | 开发保留源码审阅，桌面操作需要新鲜的窗口及控件观察。已打通明确的组合路径，任意跨应用多步自主规划仍待实现。 |
| Evolution | 区分技能出现在上下文和技能实际执行，按版本记录成功、失败、耗时和模型请求。失败经验可形成待审改进，个人组合固定依赖版本和校验和。 | 可执行组合覆盖已有 Go 变更包测试，其他组合步骤作为指导。不同本地资料目录与 Windows 用户密钥保护为当前个人隔离边界。 |
| 产品验收 | 真实 BYOK、真实 Windows 桌面和真实公开 GitHub 均有证据。真实修复任务产生了经审批步骤审核的候选，后续独立修复任务实际复用。 | 单轮对照测得模型请求 3 → 2，首次试跑完成修复但未产出候选。当前证据不足以确定长期提速或稳定命中率。 |

## 使用方式

在普通模式打开「目标」，输入目标并选择相应类型，或让应用识别明确的公开 GitHub 仓库链接。对项目任务，在工作流程查看源码预览并审核计划。对桌面任务，先在「电脑 → 观察与操作」选定支持的窗口和控件。统一列表显示实际请求、动作和子任务状态。

「连接」提供服务、令牌和精确授权范围配置。选中已授权资源及操作后可生成提案，用户核对目标、HTTP 方法、路径和请求字段再批准。认证连接可添加账户身份读取权限并单独批准验证。改配置或凭据会使旧审批失效。运行中可以取消单个动作或使用全局停止，结果不确定时保留待恢复记录。

「目标」中的进化面板提供技能效果、失败经验、待审改进、个人技能组合及记忆维护。组合创建前显示固定依赖和权限，需显式确认。记忆策略在启动和后台按日检查，归档超过 180 天的低价值条目。个人偏好、手工记录、显式纠正、高优先级条目和已验证流程受到保留规则保护。过期、待解决冲突和归档记录不参与召回。

## 真实验收记录

| 路径 | 观察结果 | 证据 |
| --- | --- | --- |
| 公开 GitHub → 真实 DeepSeek | 目标生成匿名读取提案，审批后核对 `octocat/Hello-World`，审阅事实后使用 `deepseek-flash` 生成总结。同一确认重复提交复用原请求。成功用例为 1 次付费模型请求、0 次外部写入。 | [请求和用量收据](../release/evidence/unified-agent-20261114/live-github-deepseek.json) |
| 最终版 GitHub 匿名读 | 独立只读用例通过。首次遇到网络故障，有界重试通过。 | [通过日志](../release/evidence/unified-agent-20261114/public-github-final.log) · [首次失败](../release/evidence/unified-agent-20261114/public-github-final-first-attempt.log) |
| 真实 Windows + DeepSeek 视觉 | 专用窗口完成文本写入、提交按钮操作和读回。裁剪真实的 180 × 46 按钮截图后，DeepSeek 给出重置建议。批准后 UIA 实际执行，独立验证通过并推进 Harness 完成。 | [原生桌面收据](../release/evidence/unified-agent-20261114/native-desktop-real-vision.json) |
| 真实修复 → 学习 → 下一任务复用 | 两个隔离 Go 修复任务均有文件及真实测试验证。第一项产生待审技能，审核后第二项实际调度本地测试，模型请求从 3 次降至 2 次。 | [真实学习证据](../release/evidence/unified-agent-20261114/live-development-learning.json) · [首次试跑](../release/evidence/unified-agent-20261114/live-development-learning-first-attempt.log) |

[DeepSeek API 文档](https://api-docs.deepseek.com/) · [GitHub 仓库读取 API](https://docs.github.com/en/rest/repos/repos#get-a-repository)

## 最终构建验证

- 全量 Go 测试和 `go vet ./...` 通过。18 项 Evolution 测试覆盖版本效果、组合实际执行、个人资料隔离、冲突和过期召回、自动维护周期及暂停。连接器回归覆盖凭据加密、审批过期、执行取消、重启、单次消费及独立回读。 [Go 日志](../release/evidence/unified-agent-20261114/go-test.log)
- 4 组前端测试、TypeScript、Vite 和中英文资源检查通过。新增普通模式页面在 1380、1000、720 像素宽度进行了原生 WebView 布局检查。 [前端日志](../release/evidence/unified-agent-20261114/frontend.log) · [布局记录](../release/evidence/native-d84fd1ae0b1046f2a1b1988f26c90fdd/visual-stage15.json)
- 最终二进制通过 66 项原生界面与重启检查，以及 41 项原有开发工作流检查，共 107 项。 [界面与重启](../release/evidence/native-56515f2d4e01478faac48787e7fb43b4/summary.json) · [开发流程](../release/evidence/agent-loopback-cf1afc00195f489dbca69abfd1076bbd/summary.json)
- 独立审查发现总结可能附带未审阅的个人记忆。修复后，实际模型请求只使用已审阅的连接器事实，相关外发内容、服务地址变更和并发复用回归通过。最终应用数据库回归还修复了令牌外键冲突，连接器密文独立存储并受 DPAPI 保护。 [验证清单](../release/evidence/unified-agent-20261114/manifest.json)

## 仍待扩展

- 账户接入目前采用手动令牌。OAuth 登录和自动刷新尚未实现。私人账户、Graph 邮件与日历、Slack、GitLab 和 MCP 的真实授权端到端验收需要对应账户或服务。通用 MCP 工具结果没有独立验证依据时，记录为结果未知。
- 支持的桌面操作以白名单语义控件为限。真实视觉验收覆盖专用测试窗口。任意应用自主多步操作、空白记事本端到端和模型视觉泛化未验收。
- 已验证的跨域路径为连接器事实到模型总结，已验证的学习复用路径为 Go 开发任务。更通用的跨域计划和可执行技能组合仍待扩展。

[运行 Asimi.exe](../../.local-validation/unified-native-4371c959072c4e8aa7ded326127be835/Asimi.exe)。请保留同目录两个 UIA 辅助程序及其配置。最终应用 SHA-256 为 `F68B6DA806CFF9522BF1264643A7F9034DA6626BA8A2B4C0823D3EADDC48E806`。现有安装包尚未更新。
