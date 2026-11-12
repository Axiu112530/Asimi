# Windows UIA Implementation · Phase 2D

## 运行时

复用 Windows 自带 .NET Framework UIAutomationClient/UIAutomationTypes，未增加第三方 UIA 框架。`scripts/build.ps1 -OutputDirectory build/bin/phase2d` 编译专用 WinForms host、windowless UIA worker 和 Wails 应用，Go ldflags 固定 helper SHA256。相邻固定配置也接受检查。

host 的 STA 线程运行 WinForms 消息循环。worker 的 `[MTAThread]` 主线程进行一次有界请求。UIA RCW 只在 worker 进程内使用，每次请求完成通过进程退出释放，超时通过终止应用自有 worker 释放隔离的 COM 资源。Go Goroutine 不持有 COM 指针。系统共享 WebView2 进程不参与 worker 清理。

选择 MTA 依据 Microsoft 对 UIA 客户端线程的要求。进程隔离也提供跨进程 UIA provider 阻塞的可终止边界。[Microsoft UI Automation threading](https://learn.microsoft.com/en-us/windows/win32/winauto/uiauto-threading)

## UIA 读取

从用户选定 HWND 的 AutomationElement.FromHandle 开始，RawViewWalker 仅遍历该目标。CacheRequest 读取 Name、AutomationId、ControlType、BoundingRectangle、IsEnabled、IsOffscreen、HasKeyboardFocus、IsPassword、ProcessId、NativeWindowHandle、RuntimeId 和可用 Pattern 标记。父路径由 RuntimeId 链构成。总量、深度、文本长度、序列化大小和 worker 时限都有边界。

Value SHA256 仅用于固定测试输入的前后状态检查。Notepad 读取不会请求文档内容。树中 UIA Name 仍可能包含敏感数据，只在当前用户发起的内存视图显示。

## 操作与验证

专用 host 包含 TestInput、SubmitButton、ResetButton、ResultLabel、TestCheckbox、ScrollableArea 和状态指示。Submit 将 ResultLabel 置为 Submitted，Reset 清空测试文本并置为 Reset。host 没有文件、网络、命令或系统修改功能。

worker 每次重新遍历、唯一匹配元素并校验父路径、RuntimeId、窗口/进程身份、状态和当前 Pattern。SetValue 与 TypeText 对固定 TestInput 调用 ValuePattern.SetValue。Invoke 仅支持两个固定按钮。驱动另一次读取核验文本 SHA256 或 ResultLabel，启动核验新 host 可见且身份匹配，聚焦核验 GetForegroundWindow。

全局输入保持 disabled。Microsoft SendInput 的 UIPI 限制只解决部分权限问题，本阶段未能证明跨焦点切换的原子目标绑定，因此没有开放 raw fallback。[Microsoft SendInput](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput)

## Notepad 兼容性

只读适配器要求相同 SID、会话、中等完整性、可信安装路径、包与 AUMID，以及受支持的窗口类。不同 Notepad 构建可能不满足这些约束，此时返回不可用。尚未完成用户主动启动空白 Notepad 的真实写入验收，编辑与 Focus 不在当前已开放集合。专用测试窗口的结果不能外推到 Notepad 或其他 Windows 应用。
