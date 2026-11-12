# Action Approval Security · Phase 2D

## 权限合同

Computer Control 使用 PER_ACTION_APPROVAL。每次真实启动、聚焦、Invoke、Value 操作都独立审批。前端不能传入状态、路径、命令或批准后的替换参数。审批页展示目标进程身份、元素、准确文本、预期效果和有效期。一次批准只对应一个 actionId 和 SHA256。

目标身份包括创建时间，防止 PID/HWND 复用。专用测试进程要求实际启动关系、相同用户与会话、中等完整性、固定路径与镜像散列。Notepad 要求可信系统安装目录、包身份及窗口类等约束。无法验证的 Windows 版本拒绝进入操作适配器。

执行前重新获取窗口和 UIA 元素，校验进程、路径、创建时间、窗口位置、元素 RuntimeId、父路径、类型、名称、AutomationId、状态和 Pattern。候选重复、树截断、模态窗口、密码控件、无效边界和非白名单 selector 均拒绝。每次操作获取新的 Pattern。

## 审批持久化

SQLite v4 增加 computer_actions、computer_approvals、computer_action_events。批准与意图各有短事务，单次消费使用数据库 CAS。SQLite 异常向调用方显式返回，执行意图失败阻止原生派发。执行后的数据库失败保留 durable EXECUTING/VERIFYING 状态用于恢复。

journal 保存操作类型、SHA256、字符数、时间、状态和结果码。输入文本、窗口标题、控件值和 UIA Tree 留在当前进程内存。哈希属于可关联元数据，短文本的 SHA256 不能提供密码学匿名性。用户仍应将本地数据库视为私有数据。

## 输入与隔离

专用 TestInput 接受最多 512 Unicode 字符的一行普通文本。控制字符、换行、命令标点、常见命令起始词和凭证关键词会被拒绝。字面值筛查用于缩小测试范围，真正的能力边界为固定无外部效果的 host、精确 selector、ValuePattern 和审批链。应用无密码/API Key 输入能力。

现有 filesystem.file.create 的批准、审批消费和恢复路径独立存在。Computer Control 不提供文件工具桥接。模型提供的任何文字均不能触发 Windows API，屏幕与 UIA 内容只以文本显示。

## 未通过开放门槛的能力

全局 SendInput 不能把“检查前台窗口”和“注入输入”合并为可证明原子的动作。目前没有启用 raw mouse、raw keyboard、Ctrl/Alt/Win 快捷键或 PressAllowedKey。Notepad 的编辑器结构和空文档状态具有版本差异，当前保留只读适配器。扩大应用范围需要用户的新授权和对应适配器验收。

## 终止与未知结果

Stop 先停止新动作，再取消在途 worker。中断时若效果可能已发出，写入 RECOVERY_REQUIRED。没有回滚 UI 或偷偷删除数据的行为。重启使尚未消费的批准失效，未知效果保留人工核查状态。原始批准和执行历史可供查看。
