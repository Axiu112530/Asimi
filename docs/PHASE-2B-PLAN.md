# DeskPilot Phase 2B implementation plan

Goal: deliver user-triggered BYOK text/vision conversations, two real HTTP adapters,
streaming, DPAPI, Playground and honest local usage without expanding tool authority.
Architecture: independent `internal/ai` contracts and HTTP adapters; additive app
services/schema v3; existing Vue render components extended using the same bridge.
Tech stack: Go 1.26.10, Wails 3.1.0, Vue 3.5.45, Node 24.21.0; no dependency upgrades.
Spec: user's complete Phase 2B attachment, received 2026-11-11.

## Constraints and rulings

- Preserve all Phase 0/1/2A evidence and schemaV1/schemaV2 SQL byte content.
- No commit, push, real API request, credential collection or desktop control by agents.
- User explicitly requests implement-first and unified acceptance; use focused tests
  during development and integration checks afterwards, overriding skill TDD order.
- User authorizes continuous implementation: no extra design/plan approval gates.
- Existing dirty checkout is the Phase 2A baseline. Work on `codex/phase2b-byok`
  preserving it in place; do not create a clean checkout that omits these changes.
- Real provider failures never route to Mock; Mock task planner remains unchanged.
- Price, token counts and capabilities remain unknown when evidence is absent.

## Tasks and ownership

- [ ] Provider: `internal/ai/{types,client,sse,...}.go`: validate profile/endpoint;
  `NewClient(Profile) (*Client,error)`, implement Adapter. Fixed bounded HTTP client,
  no redirects/retries, TLS verification, separate protocol parsers, tools rejected.
  httptest asserts request shapes, data URLs, SSE chunks, errors and cancellation.
- [ ] Backend: app AI files, credential vault, attachment files, schema v3 and
  narrow dispatcher integration. Reuse conversations/messages; metadata side table;
  admission/WaitGroup/lifetime cancellation; startup unknown request reconciliation.
- [ ] Frontend: profile/model/key editor, real Assistant selection/composer,
  independent Playground and Usage routes, event sequence handling and cleanup;
  bilingual keys; maintain current Mock Assistant and all prior routes.
- [ ] Acceptance: focused Go/DPAPI/HTTP tests, typecheck/build/locale checks,
  own-WebView loopback fixture where feasible, Phase 1/2A regression and evidence hashes.
- [ ] Documentation: five Phase 2B deliverables plus architecture/security/i18n/
  roadmap/testing updates. Mark live-provider acceptance pending user action.

## Shared backend bridge contract

All JSON uses camelCase. Existing business methods unchanged. New methods:

| Method | Payload | Result |
|---|---|---|
| ListAIProfiles | {} | ai.Profile[] |
| SaveAIProfile | {profile: Profile} | Profile |
| DeleteAIProfile | {id,confirm:true} | boolean |
| StoreCredential | {providerId,secret} | {hasCredential} |
| DeleteCredential | {providerId} | {hasCredential} |
| GetAISettings / SaveAISettings | {} / Selection | Selection |
| StartChat / TestAIConnection | ChatInput | {requestId,messageId,conversationId} |
| CancelChat | {requestId} | boolean |
| GetUsage | {} | UsageRecord[] |
| ImportAttachment | {name,dataBase64} | Attachment (previewDataUrl) |
| GetAttachment | {id} | Attachment (previewDataUrl) |
| DeleteAttachment | {id} | boolean |
| ClearChatConversation / DeleteChatConversation | {id,deleteAttachments:true,confirm:true} | boolean |

ChatInput: {conversationId?,providerId,modelId,content,attachmentIds:[],stream,
maxOutputTokens,temperature?,imageDetail,confirmSend:true,confirmProviderSwitch:false,
playground:false}. Only selected conversation context is sent. Existing GetConversation
returns enriched domain.Message with providerId/modelId/requestId/status/errorCode,
attachments:Attachment[],usage?:Usage. No secret or data URL in persistent message JSON.

Events `assistant.stream.started/delta/completed/failed/cancelled` contain
{conversationId,messageId,requestId,sequence,contentDelta,status,errorCode?,response?}.
Final messages persisted before terminal event. Cancellation saves partial output.

## Review focus

1. Secret disclosure through raw transport/provider errors and editable URLs.
2. SSE EOF/tool-call events misclassified as completed.
3. Cancel/close during a request losing partial output or racing SQLite close.
4. Switching providers unintentionally sending previous conversation context.
5. Attachment decoding bombs, unselected paths and unknown-cost reporting.
