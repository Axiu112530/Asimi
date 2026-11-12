# Phase 1 implementation plan and ledger

The supplied Phase 1 brief authorizes continuous implementation and minimum safety
checks without page-by-page confirmation. It supersedes extra skill approval/
commit handoffs. No Git repository exists; no worktree or commit will be created.
Phase 0 files/evidence/probes/tools are preserved. This plan applies only to the
functional prototype; no Phase 2 capability is enabled.

## Fixed design

Go/Wails 3.1.0 shell with native business binding `main.DesktopService.Call(method,
payloadJSON)`, an explicit allowlisted business dispatcher, and native
`deskpilot:changed` events (no polling). Vue 3.5.45/TS7/Vite8.3.7/I18n11.4.18 use
the existing cache and pinned lock as baseline. No external content or HTTP API.
SQLite 1.44.3 persists nine entity classes, with versioned migration and recovery
to PAUSED for interrupted tasks. Default app data is LOCALAPPDATA/DeskPilotAI;
acceptance uses an explicitly isolated workspace data directory.

Only `filesystem.file.create` is registered. Flat workspace: a single validated
basename, maximum 65536 UTF-8 bytes, no overwrite, absolute/UNC/traversal/reparse
paths denied. Security prepares a canonical immutable operation; exact persisted
approval is atomically consumed by Permission Engine immediately before execution.
Registry owns the private executor and independent verifier. No agent imports
filesystem/Windows APIs. No shared indefinite grants. Five-minute approval expiry.
Commit races report created state honestly and never remove a committed file.

## Frozen contracts

DTOs: `internal/domain/types.go`. UTC RFC3339 timestamps; uppercase task/step/
approval states; error codes and nameKey/descriptionKey rendered by Vue resources.

Tools package produces:
`New(workspace string) (*Registry,error)`;
`(*Registry).Prepare(taskID,fileName,content,approvalID string,expires time.Time)
(domain.Operation,error)`;
`(*Registry).Execute(ctx context.Context,op domain.Operation,consume func(context.Context,domain.Operation)error)
(domain.Verification,error)`;
`(*Registry).Workspace() string`; `(*Registry).Close() error`.
Tools errors expose `Code() string`, never raw syscall details in UI.

App package produces:
`New(dataDir string,registry *tools.Registry,emit func(string,any))(*Service,error)`;
`(*Service).Call(method,payload string) domain.Reply`; `(*Service).Close() error`.
Only the root adapter is bound to Wails. Emit `deskpilot:changed` with
`{conversationId,taskId}` after durable state changes.

Business dispatch requests/responses:

| Method | Input | Data |
|---|---|---|
| GetAppInfo | {} | {version,goVersion,wailsVersion,dataDir,workspace,mode,provider,model} |
| GetSettings / UpdateSettings | {} / Settings | Settings |
| CreateConversation / ListConversations | {} | Conversation / Conversation[] |
| GetConversation | {id} | Conversation including messages/tasks |
| SendMessage / CreateTask | {conversationId,content} | {conversationId,taskId} |
| GetTask / ListTasks | {id} / {} | Task / Task[] |
| CancelTask | {id,reason?} | Task |
| StopAll | {} | {cancelled:number} |
| GetPendingApprovals / GetApprovalHistory | {} | Approval[] |
| ApproveAction / RejectAction | {id} | Task |
| ListProviders / ListModels | {} / {providerId} | ProviderProfile[] / Model[] |
| GetModelCapabilities | {providerId,modelId} | Model |
| GetSecurityStatus | {} | {workspace,maxFileBytes,toolNames,pendingApprovals,activeTasks,stopAvailable,externalNetwork,desktopControl} |
| GetAuditEvents / GetToolExecutions | {} | AuditEvent[] / ToolExecution[] |

Mock parsing: explicit create request with quoted filename + quoted content, or
guided JSON `{ "fileName":"notes.txt", "content":"Hello" }` in chat. English
and Chinese forms, capabilities, explain plan and clarification continuations.
Unsupported requests are honest. Agent response language is independent; changing
UI language never rewrites persisted messages. No external provider is enabled.

## Tasks and ownership

- [x] UI: full three-panel desktop workbench; all routes, bilingual settings,
  conversation history, inspector, precise approval preview and persistent stop.
  Owner: frontend worker, only `frontend/`.
- [x] Task services: SQLite entities/migrations, deterministic Mock, state machine,
  approval CAS/expiry/rejection, event-driven updates, recovery and minimum tests.
  Owner: backend worker, `internal/app/` and `internal/provider/` only.
- [x] File security: private executor/verifier, pinned non-reparse workspace,
  exact-operation validation, no replace commit, minimum invariant tests.
  Owner: file tool worker, `internal/tools/` only.
- [x] Desktop integration: root shell/binding, local build/run scripts and app
  lifecycle; root owner. Then integrate all completed components before acceptance.
- [x] Unified acceptance: core deny/path/overwrite/cancel/verify tests, typecheck,
  build, actual WebView interactions/restart and known 1412 tracking; no large UI
  regression/performance suite. Update six docs by preserving Phase 0 design and
  adding Phase 1 status; add report and acceptance with actual results.

## Review focus

Cross-component races: cancelled approval; changed path/content/hash; create after
rejection/expiry; overwrite/junction escape; verification mismatch; restart after
commit; multilingual clarification and UI persistence. Every user-visible result
must come from durable backend state and independent file evidence.

## Completion ledger

Components were implemented in separate owned paths, then integrated before the
unified native acceptance. Review found/corrected callback lock inversion,
unsupported requests falsely marked complete, unreachable pending steps and
ambiguous quoted-command clarification. Focused regression tests and actual
native acceptance passed after fixes. UI has 396 locale keys. No phase boundary
was expanded; the known 1412 native exit diagnostic remains recorded. Phase 0
report/evidence preservation passed 27/27 hashes. See PHASE-1-REPORT and acceptance.
