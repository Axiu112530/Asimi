# Computer Control Architecture · Phase 2D

## 范围与入口

独立引擎位于 `internal/computer`。`internal/app/control_service.go` 实现动作策略、单动作审批和调度，`control_store.go` 实现 SQLite v4 journal。Vue 的 `control-panel.ts` 通过现有 Wails `DesktopService.Call` 白名单调用。执行接口接收后端内存中的完整 Proposal，前端批准请求只携带 actionId 和 actionHash。

目前动作由用户在 Computer 面板提出。模型返回值、屏幕文字和 UIA Name 都是非可信数据，现有 AI text/vision 调用没有原生驱动入口。后续模型规划接入仍需逐动作审批。

## 原生边界

`driver_windows.go` 管理固定测试进程、查询生命周期和效果验证。`window_windows.go` 获取 PID、创建时间、规范路径、镜像 SHA256、会话、SID、完整性等级、HWND、类名、标题及矩形。白名单为 DeskPilot 启动的专用 host 和严格识别的 Windows Notepad。

`helper_windows.go` 只启动构建时散列绑定的相邻 UIA worker 和 host。固定二进制、配置及目录在启动期间保持句柄保护。普通未注入构建散列的 Go build 默认不可执行控制。请求不能指定 exe、参数或目录。

独立 C# worker 的 MTA 主线程持有 UIA 对象。Go 只接收 JSON，每次请求重新建立当前 UIA 对象。CacheRequest 降低属性查询次数。5 秒 worker 超时由父进程处理，工作进程返回后等待回收，输出上限 256 KiB。单次只读请求 6 秒，批准动作的核验、执行和验证总上下文 20 秒。树查询限目标子树、500 节点、12 层，并报告截断。UIA 查询不运行于 Wails UI 线程。

## 授权与执行

Proposal 绑定动作类型、完整参数、目标和元素指纹、预期结果、观察时间、45 秒有效期、policyVersion。规范化 JSON 的 SHA256 绑定完整 Proposal。targetHash 单独绑定窗口与元素。

`PROPOSED → VALIDATING → AWAITING_APPROVAL → APPROVED → REVALIDATING → EXECUTING → VERIFYING → COMPLETED`。

同一时刻只允许一个待审批或执行中的控制动作。审批前后分别重新校验身份，元素必须唯一且支持所需 Pattern。审批事务保存授权和 REVALIDATING。消费事务同时执行带 hash、targetHash、未消费、有效期约束的 CAS 和 EXECUTING 意图记录。数据库事务期间不执行 UIA。

执行效果分别为 NOT_STARTED、ISSUED、UNKNOWN。ISSUED 后必须独立验证。COMPLETED 仅表示验证已证实预期条件。请求超时、Stop、窗口变化和存储错误都不能凭空产生成功记录。

## 已开放能力

专用 host 支持固定入口启动、聚焦、Submit/Reset Invoke、TestInput ValuePattern 输入。`keyboard.type_text` 在该 host 中采用语义 ValuePattern 写入，面板明确提示其语义。全局 SendInput、坐标点击和快捷键保持关闭。Notepad 当前只读识别及检查。

## 退出与恢复

Stop 先设置原子停止标记并取消执行上下文，再持久化待审批取消。模型与旧文件任务锁均不参与取消信号。应用关闭先关闭任务准入和发出取消，再等待受管理调用，保存控制状态，释放驱动，最后关闭 SQLite。继续沿用 Phase 2A 的窗口优先退出次序。

重启取消尚未执行的审批，执行中或验证中的动作进入 RECOVERY_REQUIRED。不自动重放。原始授权、消费事实和事件历史保留，人工重新读取窗口后可另行提出新动作。当前没有自动消除 UNKNOWN_RESULT 或 RECOVERY_REQUIRED 的功能。

原生调用前后存在外部应用状态变化窗口，故不能提供 exactly-once 桌面效果承诺。单次授权消费与不重放控制重复派发，未知结果通过 journal 显式保留。
