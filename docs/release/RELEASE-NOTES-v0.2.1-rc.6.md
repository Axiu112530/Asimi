# Asimi 0.2.1 RC 6

This Windows prerelease improves task navigation, approval stability, and the learning workflow.

## Changes

- Show model and workspace requirements where they are needed, with a direct link to model configuration.
- Keep connector approval consent across unchanged background refreshes. Changed requests or grant revisions still require fresh consent.
- Open, highlight, scroll to, and focus the exact task selected from recent work or learning history.
- Refresh learning and memory views when committed background results change; release subscriptions on navigation and discard stale reads.
- Distinguish a pending skill draft, an enabled reviewed skill, and processed experience without a reusable draft.
- Add regression coverage for learning notifications, skill-file projection, recovery, and real-provider acceptance.

## Verification

- Go regression: 526 passing records including subtests, 13 packages passed, zero failures; 10 default skips documented in the report.
- Five frontend test entry points, TypeScript, localization parity, Go static checks, and Windows builds passed.
- Native UI: 91 assertions; real DeepSeek development and restart: 41 assertions; provider connection and restart: 17 assertions.
- Real-provider checks exercised reviewed file changes, Go tests, independent verification, learned-skill reuse, and the dedicated Windows control fixture.
- A real task's reviewed skill package was read back from `SKILL.md` and `manifest.json`, with version, source, and checksum validation.

See the [full acceptance report](https://github.com/Axiu112530/Asimi/blob/v0.2.1-rc.6/docs/reports/2026-11-15-github-ui-functional-acceptance.zh-CN.md) for evidence and limitations.

## Downloads

- **Windows installer:** `Asimi-0.2.1-Setup-x64.exe`
- **Windows portable:** `Asimi-0.2.1-win-x64.zip`
- Compare each file with its matching `.sha256` attachment before use.

Close Asimi before upgrading. The installer is unsigned; Windows may display a publisher or reputation warning.

## Scope and limitations

Skill drafts require explicit review before activation. Automatic skill creation is an optional setting and is disabled in new profiles. Executable learned reuse currently supports a reviewed, approved Go test for a verified changed package; broader skills supply guidance and context.

A task that recovers from an invalid model plan can complete successfully without producing a trusted skill draft. Two such real-provider cases are retained in the acceptance report. This prerelease does not claim general autonomous operation across arbitrary desktop applications or all programming languages.

## 简体中文

本次修复了后台刷新清除授权勾选、来源任务无法准确定位、学习结果不能及时显示等问题，并完善了模型配置引导与技能产出说明。

Harness 的真实修复、审批、测试与重启恢复，以及技能候选生成、审核落盘和实际复用均有实测证据。自动创建技能需要开启对应设置，候选仍需审核；包含失败计划的已恢复任务可能不生成技能。当前自动执行技能主要限于受控 Go 测试，完整边界见验收报告。
