# Asimi 0.2.1 RC 7

This Windows prerelease refines connection controls, skill-package import, and task activity in conversations.

## Changes

- Reduce excessive space between the empty Connections state and its form.
- Align permission checkboxes and labels without changing grants or approval requirements.
- Replace the native skill-file box with a consistent import button, selected filename, size guidance, and local success/error feedback.
- Add expandable task activity with wrench, pencil, terminal, planning, and verification icons.
- Animate running work gently; stop motion for review, completion, failure, pause, cancellation, and uncertain results.
- Show only real tasks linked to the current conversation. Ordinary replies have response progress without inventing tool calls.
- Preserve conversation isolation, discard stale activity reads, and release event subscriptions on navigation.
- Keep English and Simplified Chinese resources consistent and respect reduced-motion preferences.

## Verification

- Six frontend regression entry points, TypeScript, localization parity, Go static checks, and Windows builds passed.
- Go regression: 526 passing records including subtests, 13 packages passed, zero failures; 10 default opt-in/runtime skips.
- Native control/layout/import acceptance: 97 assertions passed.
- Native file/command/activity/conversation/restart acceptance: 50 assertions passed.
- Actual local fixture files and Go tests exercised the approval and verification chain. Skill import exercised the production export/import services.
- Layout checks covered English/Chinese, light/dark themes, 1380/1000/720px viewports, and reduced motion.

The native model responses for this UI validation came from a local loopback fixture; no new paid-provider claim is made. See the [UI acceptance note](https://github.com/Axiu112530/Asimi/blob/v0.2.1-rc.7/docs/reports/2026-11-15-ui-controls-and-activity.zh-CN.md) and [functional acceptance report](https://github.com/Axiu112530/Asimi/blob/v0.2.1-rc.7/docs/reports/2026-11-15-github-ui-functional-acceptance.zh-CN.md).

## Downloads

- **Windows installer:** `Asimi-0.2.1-Setup-x64.exe`
- **Windows portable:** `Asimi-0.2.1-win-x64.zip`
- Matching SHA-256 checksum files are attached.

Close Asimi before upgrading. The installer is unsigned, so Windows may display a publisher or reputation warning.

Task activity displays existing verified execution records. It does not give ordinary chat unrestricted local tool access. Skill drafts still require review, and existing executable-skill and desktop-control limits remain in effect.

## 简体中文

本次收紧外部连接页留白，修正勾选框对齐，并重新整理技能包导入控件。导入现在显示文件名、格式大小要求及局部反馈。

对话和任务页新增可展开的活动记录：工具用扳手、文件修改用铅笔、命令用终端图标。动效跟随真实执行状态，结束或暂停后停止；普通回复不会伪装成工具调用。原有审批、权限及验证逻辑保持生效。
