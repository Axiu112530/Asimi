# DeskPilot AI — Security and Threat Model

Status: Phase 1 controls are implemented for the single file-create scope (final
section). The broader threat model retains future product/release requirements;
Phase 0 DPAPI fixtures alone do not implement a credential vault. Essential
safeguards remain in Community.

## Assets, actors and trust boundaries

Protect user files, credentials, screenshots, account actions, source repositories,
execution integrity and approval/audit records. Untrusted actors include malicious
web/email/file content, compromised model outputs/endpoints, unexpected GUI focus,
malicious executables in an authorized folder and concurrent application changes.
Same-user malware/admin compromise is outside the protection guarantee of DPAPI
and this logical permission engine; the model boundary is not an OS sandbox.

Trusted inputs: explicit user task and grants in the application, signed/validated
application code and immutable policy decisions. Web/UI text and tool stdout
are observations, never policy or human approval. A model proposal has zero OS
authority. Wails bindings must not expose arbitrary execution or key retrieval.
Any remote provider upload crosses a data boundary despite local execution.

## Permission levels

| Level | Examples | Default control |
|---|---|---|
| L0 Observe | Approved window screenshot/UIA metadata; scoped file reads | Explicit session/target grant, redact sensitive data, no external upload |
| L1 Ordinary operation | Focus/move/scroll and context-safe UI actions | Approved app/scope, fresh target identity, no inferred external effects |
| L2 Restricted write | Create/edit non-sensitive files inside approved project | Exact path/action preview and bounded grant; reviewable diff |
| L3 High risk | Install/elevate/delete/registry/system settings/upload/send mail/pay/publish | Explicit human approval of the exact effect; no standing blanket bypass |

Risk is the maximum of tool metadata, arguments and target/effect context.
A generic click/type action on Send, Pay, Publish, terminal or credential UI can
be L3. Unknown effect pauses for approval. Encoding an action as UI input must
never lower its risk. Direct executable launches and arbitrary PowerShell are L3
unless a narrowly defined deterministic read-only tool has a tested policy.
Deleting anything is L3, including within granted folders. Plan acceptance does
not replace action-level permission checks.

## Threats and mitigations

| Threat | Boundary/impact | Required mitigation and verification |
|---|---|---|
| Prompt injection | Page/file asks to upload keys or override policy | Preserve source labels and untrusted blocks; allowlisted schemas; independent deny policy; test forged approval text |
| Confused deputy | Safe tool performs unsafe effect through GUI/shell | Resolve target/effect, escalate uncertainty, never classify only by tool name |
| Path traversal/junction | Directory grant escapes to secrets | Canonical final-handle paths, volume/file IDs, reparse handling, recheck at execution; test `..`, UNC, ADS, junction/rename races |
| Approval replay/TOCTOU | Arguments or context change after consent | Single-use nonce, full canonical request hash, target identity/policy version/expiry; consume atomically and revalidate |
| Focus/geometry race | Input hits an unrelated app | HWND+PID+process start time, foreground/hit-test checks, observation revision and serial input; abort on user interference |
| Credential exfiltration | Logs/frontend/provider redirect leaks key | DPAPI vault, key references only, origin-bound headers, no auth forwarding on redirect; redact all output paths |
| Screenshot disclosure | Sensitive pixels leave device or persist | Selected window/crop, preview/redaction, explicit L3 upload consent, no default raw retention |
| Malicious model response | Unknown action, huge payload, NaN coordinates | Strict schemas, size/depth limits, known actions, finite/bounded geometry; reject before risk check |
| Hanging UIA provider | Agent freezes and stop becomes ineffective | Isolated helper + hard watchdog, bounded tree/cache, independent stop lane; kill/restart without repeating action |
| Shell injection | Unquoted model text becomes executable syntax | Typed executables/arguments, literal paths, exact preview, NoProfile, timeout/job object; arbitrary script requires L3 |
| Git hooks/credential helpers | Read-like Git action launches code/network | Controlled environment, preview exact command, no automatic hooks/config execution, disallow publish without L3 |
| Forged UI approval | Remote content invokes a privileged bridge | Navigation/bridge origin restrictions, CSP, remote content never in privileged WebView, trusted confirmation component |
| Audit loss/tampering | Success recorded without durable outcome | Transactional ordered event IDs, explicit write failures, bounded retention/export consent; no claim of tamper-proof logs |
| Cost runaway | Replanning generates uncontrolled calls | Hard action/time/token/spend budgets, limited retries, explicit provider/cost consent |

## Approval and emergency stop

Pipeline: resolve tool/schema → derive trusted facts → assess risk → check grant →
request human decision if needed → issue internal permit → immediately recheck
stop epoch/target/scope → execute → record result → verify through approved tools.
Default deny applies on parser errors, missing grants, storage failure and policy
exceptions. Approval shows tool, purpose, affected paths/window/domain, exact
PowerShell command, side effects and relevant payload preview. Changed inputs
require new consent. Text read from an app cannot simulate approval.

Emergency stop must work independently of the model/UIA worker: persistent UI
button plus tested global shortcut registration. Stop invalidates all permits,
cancels requests, stops scheduling and terminates owned child process jobs/helpers
when safe. Pause allows review; resume requires fresh observations/authorization.
Cancellation cannot undo an already committed external action; display its last
known state and require verification before retrying. Never bypass Windows UAC,
secure desktop or UIPI. Launch asInvoker, not administrator. [SendInput is subject
to UIPI](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput).
Global shortcut/job behavior is a Phase 1/2 acceptance requirement, not implemented.

## Storage, transport and privacy

Use CryptProtectData/UnprotectData in current-user scope with UI forbidden and
NULL prompt structure. Optional application entropy is not an extra password.
Store only encrypted blobs with per-user ACLs; decrypt just before a permitted
request and keep out of front-end DTOs. Best-effort zero temporary buffers; Go
GC/string copies prevent a complete memory-erasure guarantee. [DPAPI normally
binds decryption to the user and machine](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata).
Do not use machine-wide protection. Test account migration, locked profile and
corrupt blobs before release. The spike used a dummy secret and tested wrong
entropy rejection, not another-user access or ACL correctness.

Default logs: task/step/tool IDs, risk/decision/outcome, timing, error codes and
minimal evidence references. No API keys, full headers, terminal histories,
clipboard contents, raw prompts or screenshots by default. Redaction is necessary
at event creation, error wrapping, HTTP tracing and export boundaries. SQLite
does not itself encrypt ordinary task metadata; document this and apply per-user
ACLs and retention settings. No telemetry export or screenshot upload without
explicit consent. Whitelist provider origin, reject credential redirects, validate
TLS and bound responses. Do not alter global proxy settings.

## Release security gates

Require executable import/binding checks, denial/no-side-effect tests, prompt
injection corpus, path/junction races, permission replay, focus races, helper
hang/kill and emergency-stop latency tests. Review all transitive runtime licenses
and dependency vulnerabilities. Signing/installer verification belongs to Phase 4.
No unattended execution or elevated helper is authorized by this design.

## Phase 1 enforced controls

The prototype enables only L2 single-file creation. Future permission levels are
descriptions, not enabled executors. `internal/provider` has no OS/network authority;
the native binding offers no tool-by-name execution or task state setter. Only
the app's private approval consumer can be wired into a normal execution request.
The registry unconditionally validates the operation and calls that consumer before
its private executor. Test injection is restricted to Go test code.

Workspace safety uses a flat single-component name, Windows reserved-name/control/
ADS/separator checks, UTF-8 validation and a 65536-byte cap. Existing fixed-drive
ancestors are opened without following reparses and held without delete sharing.
NT operations are relative to pinned directory handles; junctions are refused,
including a reparse injected while approval is pending. A random exclusive temp
handle is written/flushed, then atomically renamed without replacement. Temporary
cleanup acts only on the owned uncommitted handle; committed files are never
silently deleted. A separate read handle verifies identity, exact content, actual
byte count and SHA-256 while the writer still blocks competing modification.

Approvals persist task ID, tool, operation, canonical path, exact content/hash/size,
ID and expiry. Five-minute, single-use consumption compares the complete stored
operation and CASes APPROVED → CONSUMED transactionally. Clients only approve by
ID; they cannot change approval arguments. Reject/expiry/cancel/restart invalidates
unused authority. Queue cancellation and post-commit cancellation are distinct:
the latter records Created=true and retains the file/evidence.

StopAll/CancelTask are independent business control calls, usable while input/
approval actions are busy. No provider response is needed to cancel. Short DB
transactions protect state; filesystem preparation, execution and Mock continuation
do not hold the control mutex. The UI always retains a global stop entry. This is
an app-level stop, not an implemented OS-wide hotkey or forced kernel-IO interrupt.

The app has no real API keys, provider HTTP transport, screenshot storage/upload,
terminal/Git executor or desktop-control bindings. Runtime logs and SQLite audit
events remain local. Conversation and approval content are intentionally persisted
for the prototype and are not encrypted by SQLite; DPAPI is only a reserved vault
interface until real providers are authorized. Standard user execution, no UAC
bypass, no hidden uploads and no secret collection remain enforced scope.

Minimum safety tests exercise approval denial/replay/tampering, expiry, path and
junction boundaries, pinned rename protection, no-replace races, verifier mismatch,
cancel cleanup and committed-result preservation. They are not a full penetration
test or a same-user-malware security guarantee. See the current acceptance report.

## Phase 2A evidence and failure handling

The permission transaction records an execution intent when consuming approval.
The private executor requires a persisted native identity for its staged file
before rename. Storage failures at either gate prevent the final file effect.
The identity-bound recovery reader is limited to the approved workspace file
and remains outside the tool registry exposed to the model. It checks the
existing path, file identity and hash without granting execution authority.

Unknown outcomes remain `RECOVERY_REQUIRED`. Cancelling or stopping tasks
preserves that state. Failed SQLite commits roll back on the original connection
before it returns to the pool. Worker persistence faults surface through a stable
error and a session-only recovery state that can show known file evidence.
Database repair and unresolved legacy attribution remain manual. See
[Phase 2A acceptance](PHASE-2A-ACCEPTANCE.md).

## Phase 2B BYOK boundary

User-confirmed text and selected images may now leave the application for the
configured provider. Saving a provider/key does not trigger network activity.
No automatic screenshot, directory upload, provider fallback or model tool
execution exists. The real conversation path never grants filesystem or desktop
authority. The original Mock-approved file tool remains separately controlled.

DPAPI credentials are encrypted at rest and never returned to the frontend.
Images enter through explicit file selection, are content-decoded with byte/pixel
limits and re-encoded into application-managed storage; provider requests use
data URLs, not original absolute paths. Unsent attachments are not uploaded.
HTTPS certificate validation remains mandatory. Redirects and automatic retries
are disabled, and uncertain dispatched requests require an explicit resend.

Ordinary conversation/attachment data is local but not DPAPI-encrypted. The user
must consent to provider transmission and costs; `store=false` does not establish
a provider-wide retention guarantee. DPAPI does not protect against same-user
malware or process-memory inspection. See [credential details](CREDENTIAL-SECURITY.md).

## Phase 2C capture and upload

Screen capture and vision upload are separate explicit user actions. Upload
consent refers to the reviewed encoded bytes and the selected endpoint and model.
Preview data is kept in a bounded in-memory cache and cleared on discard,
expiry, consume or shutdown. Model text and UI coordinates are display data
without execution authority. [Screen capture privacy](SCREEN-CAPTURE-PRIVACY.md)

## Phase 2D · 单动作授权

控制权限为 PER_ACTION_APPROVAL。短期 Proposal 的 SHA256 绑定完整目标、元素、文本、预期结果和有效期，执行前数据库 CAS 单次消费。原生白名单仅为固定启动关系的测试 host 与只读 Windows Notepad。原生输入采用精确 UIA Pattern，全局 SendInput 尚未通过开放门槛。中等完整性、同用户/会话和进程出生身份为必要条件。

Stop 使用独立取消信号，未知效果保存为 UNKNOWN_RESULT/RECOVERY_REQUIRED，重启不会重放。参见 [Action Approval Security](ACTION-APPROVAL-SECURITY.md)。
## MVP 0.1

MVP 0.1 的命令、工作区和 Notepad 安全范围见 [安全模型](SECURITY-MODEL.md)。
