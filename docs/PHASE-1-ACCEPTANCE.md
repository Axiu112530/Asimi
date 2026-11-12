# DeskPilot AI — Phase 1 Acceptance

Date: 2026-11-10, Asia/Shanghai. Result: **PASS for the authorized functional
prototype**, with the known 1412 native teardown issue retained. This does not
approve Phase 2 or a production release. Full evidence: [report](PHASE-1-REPORT.md).

| Requirement | Observed result | Evidence |
|---|---|---|
| Native program starts | PASS, GUI build and three actual Wails processes exit 0 | build.log; app-stage1/2/3 JSON and stderr |
| Complete workbench/sidebar/routes | PASS, eight routes change active navigation; Assistant/inspector/top stop wired | stage 1 route assertions and own-WebView renders |
| Settings usable/persistent | PASS, app/Agent language/theme/startup through service and UI | stages 1/2, dark-theme render checks |
| English default, live Chinese, restore | PASS, first fresh profile English, live Chinese and Chinese after restart | stages 1/2/3 |
| Translation key parity/fallback | PASS, 396 equal keys and placeholders, 194 literal uses, fallback checked | i18n-check.json |
| Independent Agent language | PASS, Chinese response under English UI and English under Chinese UI; history preserved | stage 1 language assertions |
| Input and messages | PASS, real textarea/send, persisted multi-turn timestamps/Markdown/code renderer; empty send disabled | stage 1 sends, source-reviewed handler |
| Task/plan/current step | PASS, actual backend task and three-step plan appear in inspector | stage 1 exact plan + task ID |
| Exact approval accept/reject | PASS, no execution before approval, correct approved result; rejected file absent | stage 1; external file oracle |
| Cancellation | PASS, UI cancel/StopAll; pending file absent; post-commit preservation tested | native stage 1 and Go safety tests |
| Scoped single tool | PASS, only filesystem.file.create registered; no other real executors | stage 1 security/provider assertions and registry review |
| Traversal/UNC/ADS/name/reparse denied | PASS, no escaped file, real junction and pinned rename cases covered | safety-tests.log, external oracle |
| No overwrite / atomic target race | PASS, original bytes preserved, target created during approval not replaced | Go tests and native duplicate task |
| Independent verification | PASS, exact content, 42-byte count and approved SHA-256 agree with separate file read | stage 1 verification + acceptance-summary.json |
| Unverified result never completes | PASS, forged/mismatched verifier evidence produces failed task | app verification-gate test |
| Unsupported request honest | PASS, screenshot/PowerShell request FAILED/UNSUPPORTED_REQUEST with no approval/execution | updated stage 1 + regression test |
| Clarification and plan explanation | PASS, missing content pauses, explicit continuation creates a proposal; explanation read-only | stage 1 + provider/app tests |
| Quoted unsupported command isolation | PASS, cannot merge terminal command into pending file draft | stage 1 + targeted parser regression |
| Nine local entities/migration/history | PASS, v1 schema, restored conversation/task/approval/execution/audit, no automatic replay | store.go; stage 2; recovery tests |
| Agent cannot bypass safety chain | PASS within current code/bindings: no OS authority in provider, private executor, durable exact consumer always called | reviewed wiring + denial/tamper tests |
| No real model/desktop/terminal | PASS, future providers disabled, security reports false; no transports/control tools in production | source and stage 1 assertions |
| Responsive light/dark | PASS targeted 1380×820/1000×700/900×620 checks; no document horizontal overflow | visual-stage1/2/3 JSON and PNGs |
| Preserve Phase 0 | PASS, original report/evidence 27/27 hashes unchanged; probe/tool directories retained | baseline + acceptance-summary.json |
| No automatic Phase 2 | PASS, only Mock + file-create remains enabled | report/scope and final evidence |

## Verification commands

See [TEST-STRATEGY](TEST-STRATEGY.md) for exact scope and sandbox conditions.
Build: `./scripts/build.ps1`. Unified native acceptance:
`./scripts/accept-phase1.ps1 -SkipBuild`. Tests use fresh project-local app data;
they do not operate the user's desktop or call a model. The independent PowerShell
oracle reads only the deliberately created test file and verifies expected bytes.

## Explicitly not accepted as complete

1412 teardown resolution, signed installer/release, Windows 10 support matrix,
full accessibility/UI regression, performance/soak/race/fuzz testing, general
crash-recovery reconciliation, DPAPI vault implementation, real providers,
computer input/capture/UIA, terminal/Git and workflows remain outside this pass.
No assertion here converts those planned capabilities into implemented ones.
