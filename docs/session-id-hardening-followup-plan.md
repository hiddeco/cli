# Session-ID hardening: follow-up design

**Date:** 2026-09-18
**Base:** PR #2405 (`codex/session-id-hardening-20260912`) at `8a70a5ef6`
**Status:** design, awaiting approval

## Why this document exists

PR #2405 fixes a reported path-traversal class in agent session IDs. A multi-lens
review of it at `a79ba75cc`, an adversarial validation pass over that review's own
findings, and an independent re-review of the three commits since, together produced
a set of defects wider than one PR should carry. This document assigns every one of
them to a unit of work, says what each unit changes, and records the decisions that
are already settled so they are not relitigated.

Nothing here is speculative. Every defect below was either executed end-to-end or
traced through real source; each entry says which. Findings that were **refuted**
during validation are recorded in "Corrections" rather than silently dropped, because
two of them are currently encoded in shipped documentation.

## Units

Six units, each independently mergeable, in the order they should land.

| # | Unit | Target | Blocking for #2405? |
|---|---|---|---|
| 1 | Fixes to PR #2405 | stacked on #2405 | **yes** |
| 2 | External-preflight edges | stacked on #2405 | no |
| 3 | Sibling `.`/`..` validators | against `main` | no |
| 4 | `CLAUDE_CONFIG_DIR` + `clean --all` | against `main` | no |
| 5 | `SessionRef` containment | its own PR | no |
| 6 | Session-state namespace collisions | against `main` | no |

Units 2–6 are independent of each other. Unit 1 is the only one gating #2405.

---

## Unit 1 — fixes to PR #2405

Stacked on #2405, or folded into it. Eight items. Each leaves `mise run check` green.

### 1.1 `ValidateExternalSessionRef` misreports containment — CONFIRMED (executed)

`agent/session_store.go:228` and `:234` return `ErrOutsideSessionStore` for refs that
are demonstrably *inside* the store:

```
ref = <sessionDir>/./sess.jsonl
ValidateExternalSessionRef -> "path is outside the agent's session directory: ... contains a dot path component"
store.Name(ref)            -> "sess.jsonl", err=<nil>          # it is inside
store.ValidateExternalWriteRef(ref) -> <nil>                    # the store accepts it
```

Commit `979b28a5a` introduced `ErrUnsafeSessionName` precisely because "reporting
`ErrOutsideSessionStore` for a merely malformed ID names the wrong problem"; the
dot-component and rooted arms were missed, and `f5aa52adc` moved those lines without
fixing them.

**Change:** `ErrUnsafeSessionName` at `:228` and `:234`. Keep `ErrOutsideSessionStore`
at `:237` ("escapes its relative base") — that one is a genuine containment statement.
**Test:** `errors.Is(err, ErrUnsafeSessionName) && !errors.Is(err, ErrOutsideSessionStore)`
for a dot-component ref and a rooted ref; the converse for an escaping ref.

### 1.2 The `ResolveSessionFile` guard under-covers, and CLAUDE.md asserts it doesn't — CONFIRMED (executed)

`agent/resolve_session_file_guard_test.go:63` uses a `cmd/**/*.go` pathspec.
`e2e/testutil/session_paths.go:45` calls `ag.ResolveSessionFile(sessionDir, meta.SessionID)`
with an ID read from checkpoint metadata, outside that pathspec and outside the
allowlist. The call is benign in itself, but it sits in exactly the copy-paste zone the
guard's own rationale names, and new agent E2E runners live under `e2e/`.

This is a defect inherited from the review that recommended the guard: that review's
grep was scoped to `cmd/` and reported "exactly 4 hits". CLAUDE.md now states as
repo-wide fact that "the only sanctioned callers are `SessionFile` itself and
`external`'s pure delegation." That is false.

**Change:** pathspec to `:(glob)**/*.go`; add `e2e/testutil/session_paths.go` to
`resolveSessionFileCallers` with its reason; correct the CLAUDE.md sentence.
**Test:** the guard already fails correctly on a planted violation (verified with an
untracked decoy) and fails on zero matches; add the new allowlist entry's staleness
direction.

> Everything else about the guard checks out: it delegates `--untracked`, `--no-color`
> and repo-selector scrubbing to `testutil.GitGrepGuard`, restricts to `*.go` with a
> fatal unparseable branch, and enforces both directions.

### 1.3 The `RepoPath`-independence of the preflight is untested — CONFIRMED

All nine cases in `TestWriteSession_RejectsUnsafeReferenceBeforeSubprocess`
(`agent/external/external_test.go:279-395`) pass `RepoPath: t.TempDir()`. The doc
comment on `ValidateExternalSessionRef` says the independence exists because "gating
them on a field both current callers happen to set is a check that disappears for the
next caller that does not" — and a refactor that re-gated on `RepoPath` would pass the
entire suite.

**Change:** none to source.
**Test:** one table case with `RepoPath: ""` and `sessionRef: "../outside.jsonl"`,
asserting the subprocess marker is absent.

### 1.4 `0o700` does not reach the directory that holds transcripts — CONFIRMED (executed)

Under `umask 022`:

```
outer                   0700    <- fixed by 979b28a5a
outer/inner (store)     0700    <- fixed by 979b28a5a
outer/inner/sub         0750    <- NOT fixed
outer/inner/sub/a.jsonl 0600
```

`agent/session_store.go:325` still creates nested directories at `0o750`. For every
agent that nests — Copilot (`<dir>/<id>/events.jsonl`), Gemini (project hash), Pi
(encoded repo path) — that is the directory actually holding the transcript. Separately,
on the common path the *agent* creates the store root, so the `MkdirAll` is a no-op and
the `0700` never applies; the fix only covers "Entire got there first", which is the
`resume`-onto-a-fresh-machine case.

Severity low: all transcripts are written `0o600` (verified at all eight
`WriteSessionFile` call sites), so the exposure is directory listing — session IDs —
not content.

**Change:** `osroot.MkdirAllNoSymlink(root, dir, 0o700)` at `session_store.go:325`.
**Test:** extend the existing `//go:build !windows`, non-parallel, `syscall.Umask(0o022)`
test to assert the nested level too.

### 1.5 `logging.WithAgent` still dropped — CONFIRMED

`resume.go:905` is `logging.WithComponent(ctx, "resume")`; main had
`logging.WithAgent(logging.WithComponent(ctx, "resume"), ag.Name())`. Resume debug lines
lose the `agent` attribute. None of the three new commits addressed it.

**Change:** restore it. The signature is `WithAgent(ctx, types.AgentName)`
(`logging/context.go:93`), so `types.AgentName(metadata.Agent)` is a plain conversion,
not an open question.
**Test:** capturing `slog` handler asserts the `agent` attribute on the resume path.

### 1.6 `SessionRefIsFilesystemPath` is exported with no outside caller — CONFIRMED

`agent/session_store.go:201`. Widens the package API for nothing.

**Change:** unexport. **Test:** compilation.

### 1.7 `\` is rejected on Unix, where it is a legal filename byte — CONFIRMED (executed)

```
store.WriteFile(`weird\name.jsonl`, ...) ->
  validate session file name: unsafe session file name:
  invalid file name component "weird\name.jsonl": contains path separators
```

`validateWriteName` splits on `/` only (`filepath.ToSlash` is identity on Unix), so a
backslash stays inside a component and is then rejected. Reachable only through an
external plugin's `session_ref` on Linux/macOS.

**Decision:** keep the rejection, fix the justification. It is fail-closed and
consistent with `ValidateSessionID`'s long-standing unconditional `ContainsAny(id, "/\\")`,
and it matches the settled decision that session IDs travel in checkpoints to Windows
readers. What is wrong is the doc comment, which justifies it as "a separator is
rejected rather than split on" — on Unix `\` is not a separator.

**Change:** rewrite the comment to state the cross-platform rationale.
**Test:** golden message for a backslash-bearing component, asserting rejection is
intended rather than incidental.

### 1.8 `SessionStore.Name` still returns a rooted string on Windows — CONFIRMED (traced, not executed)

`Name("\foo")` takes the relative branch, cleans to `/foo`, and
`paths.IsRelativeTraversal("/foo")` is false, so a rooted string is returned as a "name
inside the store". `f5aa52adc` refactored these lines into `cleanRelativeName` /
`relativeNameEscapes` without changing the behaviour.

It is covered downstream — `validateWriteName` splits `/foo` into `["", "foo"]` and
rejects the empty component, and `ValidateExternalSessionRef` rejects it as rooted
first — so this is a wart, not a hole. The one residual leak is copilot's
`isSubagentAgentStop`, where a rooted name flips a `SessionEnd` into a `TurnEnd`; no I/O
follows, so it is cosmetic.

**Change:** reject `os.IsPathSeparator(p[0])` before the relative branch's `Clean`.
**Test:** `\foo` and `/foo` rejected; must run under `GOOS=windows` to be meaningful, so
the test is a unit test over the helper rather than an integration test.

### 1.9 `RestoreLogsOnly`'s new `point.Agent` fallback — PLAUSIBLE, not confirmed

`strategy/manual_commit_pending.go:398`. A multi-session checkpoint where session *i*
records no agent but was produced by a *different* agent than the checkpoint's top-level
one now has its transcript written into the checkpoint agent's directory, and `entire
resume` prints that agent's resume command for it. Previously it was skipped with a
warning.

The fallback is net-positive — it is what made resume shape C better than main — and no
shipped code path was found that produces a mixed-agent checkpoint with a partially-empty
per-session agent field.

**Change:** keep the fallback; emit a stderr warning when it is used, naming the assumed
agent, so a mixed-agent checkpoint is visible rather than silent.
**Test:** a two-session checkpoint where one session lacks agent metadata asserts both
the restore and the warning.

### 1.10 Verify the protocol doc matches the relaxed preflight — NOT YET CHECKED

`8a70a5ef6` edited `docs/architecture/external-agent-protocol.md` (15 changes) as part of
"document what this branch changed". The re-review confirmed one forwarded case matches
the new doc (`tenant/../session-key`), but did not audit the document as a whole against
the preflight's actual behaviour.

`session_ref` was previously documented only as "Path/reference to session in agent's
storage", with no containment requirement. The preflight adds real constraints for
filesystem-shaped refs. The doc must state exactly the constraints that **survive** the
settled relaxation — escapes, symlinked components and refs outside the store are fatal;
empty `session_dir`, `get-session-dir` errors, `./` components and POSIX-legal
colons/device names are not — and must not promise the ones Unit 2 will later remove.

**Change:** audit and correct as needed.
**Test:** none mechanical; this is a read.

---

## Unit 2 — external-preflight edges

Stacked on #2405, non-blocking. The author triaged these as "edges this PR introduced,
none blocking"; the re-review executed all four against the delta and agreed — unchanged,
all fail-closed, none reachable without a cooperating plugin.

| edge | detail |
|---|---|
| MSYS form rejected | `/c/Users/u/agentx/s.jsonl` rejected as rooted on Windows, a shape `paths.normalizeMSYSPath` compensates for elsewhere |
| `d:opaque-key` misrouted | single-character volume prefix routes an opaque DB key into the filesystem branch; `db:key` is unaffected |
| `/./` rejected | a plugin doing naive `dir + "/./" + id` concatenation is refused; overlaps 1.1's wrong sentinel |
| `get-session-dir` errors fatal | a plugin whose `get-session-dir` fails now hard-fails a write that previously never invoked it |
| legacy-ID hard fail | `resume.go` — theoretical; no shipped agent emits such IDs, and `ValidateSessionID` accepts every UUID and ULID shape |
| device-name regex | `COM0`/`LPT0` uncovered, plus the `aux.js` caveat — fail-closed narrowing only |

---

## Unit 3 — sibling `.`/`..` validators

Against `main`. The same exact-match `name == "." || name == ".."` weakness PR #2405
fixed in `ValidateSessionID`, in two validators it did not touch.

- `plugin_store.go:169 validatePluginName` accepts `...`, `.. `, `. `, `foo.`, `C:foo`,
  `com1`. Not exploitable — `PluginDataDir`'s only caller is reached after `resolvePlugin`
  found a real executable on `$PATH`, so the attacker already has code execution as the
  user, and all CLI-side I/O goes through `pluginRoot`'s `os.Root`.
- `plugin_store.go:114-117`'s doc comment claiming the validation "guarantees"
  containment is **provably false** and should be corrected regardless.
- `cursor/images.go:133 sessionIDFromTranscriptPath` — `"/x/.. .jsonl"` yields `".. "`,
  which then reaches `findStoreDBs` and is joined as a literal component of a SQLite path
  (`cursor/images.go:163`). Contained by `filepath.Base`, read-only, size-capped,
  magic-byte filtered, Windows-only.
  **Reachability must be re-verified first.** It was reachable at `a79ba75cc` because
  `cursor/lifecycle.go` returned the payload transcript path verbatim, bypassing
  `SessionFile` entirely. `f5aa52adc` changed that function to resolve through
  `agent.OpenSessionStore(...).SessionFile(id)`. If the `SidecarImages` →
  `sessionIDFromTranscriptPath` → `findStoreDBs` route now only ever sees a
  store-validated path, this item drops to a hygiene fix; if it still reads
  `state.TranscriptPath` from a pre-upgrade state file, it does not.

**Explicitly NOT in scope:** `checkpoint/parse_tree.go`. It splits on `/` and rejects
`.`/`..`/empty/absolute/`.git`/`git~1` per segment — the claim that it shares this
weakness was refuted.

---

## Unit 4 — `CLAUDE_CONFIG_DIR` and `clean --all`

Against `main`. Two pre-existing live bugs, unrelated to session-ID hardening, both
confirmed by execution.

### 4.1 `CLAUDE_CONFIG_DIR` unhonoured — CONFIRMED, high for affected users

`agent/claudecode/claude.go:95-108` and `:111-119` hardcode `~/.claude/projects/…`. Only
`generate.go:115` reads the variable. Demonstrated on a machine where
`CLAUDE_CONFIG_DIR=/Users/h/.claude-work`, `~/.claude/projects` does not exist, and
`~/.claude-work/projects/<sanitized>` does:

- `entire attach` reports `transcript not found for agent "claude-code" with session <id>;
  is the session ID correct?` while the transcript is on disk and the ID is correct.
- `entire resume` **silently restores to the wrong place** — creates
  `~/.claude/projects/<sanitized>/`, writes the transcript, reports success, and
  `claude --resume` never sees it. This is the worst failure mode in the whole set.
- `AgentForTranscriptPath` returns `ok=false`, silently disabling the #1262 forwarded-hook
  guard and removing the strongest signal from `resolveSessionAgentType` /
  `correctSessionAgentType`.

Not affected: hook capture and checkpointing, which re-resolve from stored
`state.TranscriptPath`.

**Change:** one `claudeHomeDir()` helper honouring `CLAUDE_CONFIG_DIR`, used by both
functions, by `generate.go:115`, and by the four `~/.claude` joins in `discovery.go:42-52`
which have the same defect for skills, commands and agents.

### 4.2 `listAllTempFiles` basename bug — CONFIRMED, medium

`clean.go:549-556` discards the walk's root-relative name and appends `d.Name()`;
`removeTempFileNoSymlinks` (`:616`) then rebuilds `.entire/tmp/<basename>`.
`osroot.WalkDirNoSymlinks` recurses, so nested files are counted but keyed wrongly:

```
listAllTempFiles -> ["pi-active-session", "top.json"]
deleteTempFiles  -> deleted=["top.json"] failed=[]
BUG: .entire/tmp/pi/pi-active-session still exists
```

Real producer: `agent/pi/lifecycle.go:294` writes `.entire/tmp/pi/pi-active-session` on
every pi hook. The ENOENT is swallowed at `:588-594`, so the file is reported as neither
deleted nor failed and survives every `clean --all` forever. A basename collision
additionally double-counts in the "Delete N items?" prompt.

**Refuted:** no *wrong* file is deleted — a collapsed basename can only name a file
already in the deletion set.

**Change:** append `strings.TrimPrefix(name, entireTmpName+"/")`.
`osroot.OpenParentNoSymlinks` already walks multi-segment names component-by-component, so
`removeTempFileNoSymlinks` needs no change. `listOrphanAgentTemps` at `:491-497` already
does this correctly and is the model.

---

## Unit 5 — `SessionRef` containment

Its own PR. This is the only item in the set that crosses a machine boundary.

### The hole

`event.SessionRef` is never validated anywhere. `lifecycle.go` takes it verbatim →
`ag.ReadTranscript` → `os.ReadFile` → `.entire/metadata/<sid>/full.jsonl` → condensation
→ committed and pushed. Demonstrated end-to-end: a canary planted at `/tmp`, fed in as
`transcript_path` to `entire hooks claude-code stop`, reached a real remote under **both**
checkpoint backends. Redaction is **not** a mitigation — an SSH private key and
`/etc/passwd` lines shipped verbatim; only an AWS-key line was replaced.

`entire hooks <agent> <verb>` has no caller authentication; the gates are "inside a git
repo" and "Entire enabled". The practical attacker is a prompt-injected agent in a
network-restricted sandbox that is nonetheless allowed to run `entire` — which converts
an arbitrary local file into a push over the user's own credentials.

Pre-existing on `main`, and tracked: `agent/transcript_read_guard_test.go` is a ratchet
over exactly this gap, with a written deferral rationale.

### The design — settled

Admissible roots are the **union**, not a fallback chain, of:

1. `ag.GetSessionDir(repoPath)`
2. `ag.GetSessionBaseDir()` where the agent provides one
3. an agent-declared extra root via a new optional `TranscriptRootsProvider`, implemented
   only by OpenCode (`<repoRoot>/.entire/tmp`)

The union is mandatory, not stylistic. `GetSessionBaseDir` deliberately ignores
`ENTIRE_TEST_CLAUDE_PROJECT_DIR` — the comment at `claudecode/claude.go:111-113` says so —
so a "base dir else session dir" ordering refuses every ref in the integration and e2e
harnesses for Claude Code, Cursor, Gemini, Droid and Pi. Tier 3 is required, not
defensive: `opencode.sessionTranscriptPath` returns `<repoRoot>/.entire/tmp/<id>.json`
while its `GetSessionDir` returns a temp directory it no longer uses.

Comparison is `pathHasDirPrefix` (already Windows case-folding) over **resolved** paths on
both sides: walk up to the longest existing prefix, `EvalSymlinks` that, re-append the
remaining lexical components. This handles macOS `/var` ↔ `/private/var`, handles a leaf
OpenCode has not created yet, and refuses a symlink planted inside the store pointing at
`~/.ssh/id_rsa`.

`TranscriptRoots` returning `(nil, nil)` means "no opinion" and admits — the settled
external-plugin policy applied verbatim.

### Commits

| # | contents | if it stalls |
|---|---|---|
| B1 | `agent.AdmitTranscriptRef`, its call sites, **fail-soft** refusal, telemetry, `status`/`doctor` surfacing | exploit already closed |
| B2 | turn-end copy reads through the admitting `os.Root`; ratchet entries drop | gate remains a check; nothing reopens |
| B3 | delete `agent.HookInput`, `Agent.ReadSession`, `Agent.GetSessionID`; correct the ratchet's doc comment | eight uncounted reads survive, correctly counted |

**The exploit closes at B1.** B2 and B3 are separable by construction.

### Settled decisions

- **Fail-soft**, not fail-hard. The cost is stated plainly rather than buried: a
  mis-modelled agent stops checkpointing, and the loudest symptom is a line in
  `entire status`. The degraded marker and telemetry are part of B1, not an afterthought.
- **B3 deletes the exported API outright** rather than deprecating it. Nothing in-tree
  uses it and the package lives under `cmd/`.

### Constraints any implementation must not break

Measured, not assumed:

- Claude Code launched from a repo **subdirectory** already returns `ok=false` from
  `AgentForTranscriptPath`.
- **Every** opencode session returns `ok=false` by construction.
- macOS `/var` vs `/private/var` never matches — `pathHasDirPrefix` does no symlink
  resolution.
- Hooks are fail-open by design; a hard failure breaks the user's agent turn.

Unit 4.1 is a **precondition**: without `CLAUDE_CONFIG_DIR` honoured, the gate refuses the
repo's own real-agent e2e suite.

---

## Unit 6 — session-state namespace collisions

Against `main`. `.git/entire-sessions/` mixes two namespaces: session state files
(`<sessionID>.json`) share a flat directory with control files written by other
subsystems — `review-pending.json` (`review/marker_fallback.go`), `.warn-stale-ended`,
`<sessionID>.model`, `<sessionID>.trail-scope.json`.

A session ID of literally `review-pending` passes `ValidateSessionID` — alphanumerics and
a hyphen — and `Save` writes to the same path, replacing the marker via atomic rename. The
marker's `Prompt` field is consumed as `opts.ReviewPromptOverride` for a
permission-bypassed agent. A case-folded variant collides on APFS/NTFS.

`.entire/tmp/` is a second instance of the same class: Entire keys state there by
**prefix** (`pre-prompt-<sessionID>.json`, `pre-task-<toolUseID>.json`, with
`FindActivePreTaskFile` scanning and stripping affixes back into a tool-use ID) while
OpenCode writes a bare `<sessionID>.json` into the same directory.

**Correction carried forward:** an earlier analysis claimed `List`/`Load`/`IsStale`
deletes `review-pending.json` immediately because `StartedAt` is never backfilled. That is
wrong — `State.StartedAt` and `PendingReviewMarker.StartedAt` share the json tag
`started_at`, so the marker's timestamp is unmarshalled and the threshold is
`StaleSessionThreshold` = 7 days.

**Design direction:** a reserved subdirectory for control files, plus a reserved-name rule
in `ValidateSessionID`, with migration for existing on-disk markers. Detailed design is
deferred to this unit's own brainstorm — it is a storage-layout change and does not belong
in the same breath as the rest.

---

## Corrections — claims that were made and are wrong

Recorded because two of them are currently encoded in shipped documentation and one in a
guard test.

| Claim | Status |
|---|---|
| "An `Lstat`-based `SameFile` pin only stops a race, not a pre-planted symlink" | **False.** `before` is the link's inode, `root.Stat(".")` the target's, so the pin rejects a planted link — which also means it is not a middle ground, since it rejects a dotfile-managed `~/.claude` by the same mechanism. No mechanism-level middle ground exists; a real one must be policy. |
| "Copilot's `GetSessionDir` uses the hook payload's CWD" | **False.** Signature is `GetSessionDir(_ string)`. |
| "`normalizeGitTreePath` shares the exact-match `.`/`..` weakness" | **False.** Per-segment. |
| "One unsafe ID fails the whole resume" | **Overstated.** `RestoreLogsOnly` skips and continues; the hard error fires only when every session is unsafe. |
| "Moving the `:` check into `unsafeFileNameComponentReason` is byte-identical" | **False** for messages, but a differential fuzz of 3,633 inputs shows **zero** accept/reject changes for `ValidateSessionID`. The reorder is safe. |
| "`external.go` duplicates `SessionStore.Name` and the copy is removable" | **False.** Different empty-ref handling and a rooted check `Name` has no counterpart for. `f5aa52adc` collapsed it correctly anyway. |
| "The copilot `isSubagentAgentStop` residual is a live misclassification" | **False.** `DispatchLifecycleEvent` rejects an unsafe `SessionID` before any handler runs. Unreachable in both directions. |
| "Exactly 4 `ResolveSessionFile` callers" | **False.** The grep was scoped to `cmd/`; `e2e/testutil/session_paths.go` is a fifth. Now encoded in CLAUDE.md and the new guard test — see 1.2. |
| "The `.git` session ID poisons the shared checkpoint branch" | **False.** `Tree.Encode` → `ValidTreePath` makes it a hard write failure. |
| "Agent session stores have no sensitive sibling" | **False.** See Unit 6. |

## Confirmed clean — do not relitigate

- The Windows device-name regex has **zero** false positives. Exhaustive over all hex 3-
  and 4-character prefixes, 2M UUIDs, 2M ULIDs, and all `[a-zA-Z0-9_-]` strings of length
  ≤ 4 (rejected set is exactly 176 strings, every one a device-name case variant). No repo
  fixture is rejected.
- The checkpoint-metadata → path vector is properly defended. A crafted checkpoint with
  `session_id: "../../../../../../../../tmp/pwned"` was rejected at two independent gates
  and wrote nothing; attempts via the agent's own layout and via
  `RestoredSessionPathResolver` also failed.
- All three resume regression shapes are fixed by `8a70a5ef6`, plus a fourth the
  re-review invented.
- Build, vet, lint, gofmt and `GOOS=windows` cross-compile are clean on the branch. 13
  failing tests, all fetch/remote, identical on the branch and on `a79ba75cc`.

## Test strategy

- **Resume shapes A, B, C and "all sessions lack agent metadata"** get pinned as a table
  test, A/B'd against `main` semantics. Three of the four are currently proven only by
  throwaway probes.
- **Permissions** (1.4) need `//go:build !windows`, non-parallel, `syscall.Umask(0o022)`,
  asserting both the store root and a nested level.
- **Sentinels** (1.1) are asserted with `errors.Is` in both directions, not by message
  substring.
- **The guard test** (1.2) is verified by planting an untracked decoy, which is how the
  existing coverage gap was found.
- **`RepoPath: ""`** (1.3) is the regression-proofing case for the preflight's stated
  independence.
- Unit 5's admission predicate needs a table over all nine built-in agents plus an
  external stub, asserting that each agent's own real transcript path is admitted — the
  test that would have caught the `GetSessionBaseDir` ordering error.

## Open questions

1. **Base strategy.** The agreed plan was to rebase #2405 onto `main` and stack. That was
   decided when the tip was `a79ba75cc`, which did not touch CLAUDE.md and rebased
   cleanly. `8a70a5ef6` edits CLAUDE.md, which has 13 commits of churn on `main`, so the
   rebase now conflicts there. Options: resolve the conflict during the rebase (rewrites
   the author's doc commit), or merge `main` into the follow-up branch instead (leaves
   #2405's history alone, one merge commit). **Recommendation: merge.**
2. **Unit 1 placement.** Folded into #2405, or stacked as its own PR? Folding keeps the
   PR self-consistent; stacking keeps #2405 reviewable against what has already been
   reviewed.
3. **Unit 6 scope.** This document gives a direction, not a design. It needs its own
   brainstorm before it is planned.
