# Harness Reliability Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans and test-driven-development. Track each test and result.

**Goal:** Persist completion only when local task evidence satisfies the requested outcome, and enforce the task deadline across the approval and execution lifecycle.
**Architecture:** Keep Agent 02, its immutable product action journal and Wails bridge. Add a versioned acceptance contract and a local verifier. Reuse the bounded workspace reader, static analysis, registered commands, and Windows recovery.
**Tech Stack:** Go, SQLite, Wails, Vue/TypeScript.
**Spec:** Approved Asimi integration design, sections 4–5.

## Constraints

Preserve current working-tree changes and existing approval, cancellation, recovery and BYOK paths. Run offline and loopback acceptance before claiming success. Maintain English and Chinese UI. Scope the first plan to Harness.

## Review focus

- A final summary cannot bypass a missing write or test.
- A successful test predating the latest edit, or from another task, is insufficient.
- A declared observation must produce bounded evidence or an explicit error.
- Deadline expiry must not consume an unused approval or erase a verified outcome.
- A stopped or unresolved action must not be replayed.

## Tasks

- [x] Add loopback regression cases for false FINALIZE, real observation steps, no matching test and stale file evidence. Persist versioned acceptance with explicit read/repair/test intent, optional paths and exact registered test target. Unknown intent requires clarification. Inspect consumed and settled product actions and read back file identity/hash. Keep model explanation and status separate.
- [x] Thread the existing absolute deadline through preview, pending review, approvals, file work and command work. Add an integration fixture for a slow registered test and for approval already past the task deadline. Preserve recovery when mutation may have started.
- [x] Show evidence, unmet conditions, legacy verification state and expiry in the Agent panel. Reuse actual desktop availability and aggregate persisted approvals and tasks in Security. Verify localized keys and run the offline Go and frontend suites.
- [x] Record which acceptance scenarios ran and which require real provider or desktop validation. Continue next capability only after Harness checks pass.

The user approved implementation in this checkout. Changes remain available for review in the working tree alongside the existing edits.

## Evidence recorded on 2026-11-14

- The full Windows Go suite and go vet passed. TypeScript, the production build, the three frontend test files, localization checks, and git diff --check passed.
- Regression fixtures confirmed a model cannot complete a repair without evidence. An untested repair, changed file, later failed test, or passing test from an unrelated package leaves completion unverified.
- The registered slow command stopped at the task deadline. A pending approval past the task deadline remained unconsumed.
- The isolated native Windows app passed stages 11 and 13 with 34 and 7 assertions respectively. The run used a loopback provider, approved a real file change, read it back, ran a Go test, persisted completion and candidate evidence, then restarted without replay. The fixture preserved the existing working-copy comment and Git index.
- Build SHA256: B991CB6E6BAE4C250ABB6EE8A9674DED69A6DE74CE5AEFB1822167BB6C0549B2. Reports: docs/release/evidence/agent-loopback-a44ad28cfb5e46939b02c2c50392e199/summary.json and its two stage reports.

## Boundaries for the next increment

The acceptance contract covers reviewed local analysis, file repair and registered tests. Language recognition is conservative, with a clarification path for other goals. Go repair evidence is matched to the edited package. Other runners do not yet have equivalent package-level evidence. Stop and timeout are cooperative for bounded filesystem reads. Atomic changes that may have taken effect remain subject to the existing recovery flow.

The current application smoke run used loopback, with zero external calls. Real BYOK and real desktop control require their separate acceptance scenarios. Executable Skills, memory governance, desktop and connector tools in Harness, and the cross-project learning cycle remain separate roadmap work.
