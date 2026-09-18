# Session-ID Hardening Unit 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the twelve defects that gate PR #2405, so the session-ID hardening branch can merge without shipping a wrong error sentinel, a containment check that disappears, a permission regression, or documentation that contradicts the code.

**Architecture:** Every change is local to an existing function. No new packages, no new types except one exported sentinel already present, and one helper gains a parameter so a Windows-only rule becomes testable on Unix. Nine tasks, each independently reviewable and independently revertable.

**Tech Stack:** Go 1.27.1, `testify/require` + `testify/assert`, `mise run check` (gofmt, golangci-lint, `test:ci`), `testutil.GitGrepGuard` for source-level guard tests.

**Spec:** `docs/session-id-hardening-followup-plan.md` (committed `e21a1aff0`). Read it before starting — this plan implements only its Unit 1, and the spec's Corrections table records ten claims that were made during review and are false. Do not reintroduce them.

## Global Constraints

- **Acceptance bar is a comparison, not a count.** Measure `go test ./cmd/entire/cli/ ./cmd/entire/cli/strategy/` on your base commit BEFORE starting. At `8a70a5ef6` it produced 15 `--- FAIL` lines across 14 top-level tests, all fetch/remote or checkpoint-remote. No task may add a new failure. The absolute number is environment-sensitive — do not treat 15 as a target.
- **`mise run check` before every commit**, per CLAUDE.md. The comparison above is about *test* regressions only; fmt and lint must be clean.
- **`t.Parallel()` in every test** unless it calls `t.Setenv` or `t.Chdir` or `syscall.Umask`. Those cannot be parallel.
- **Tests that touch git use `testutil.InitRepo`**, never bare `git init`.
- **Never call `git.PlainOpen` directly** — `gitrepo.OpenCurrent` / `OpenPath` only.
- **Every `git status` invocation passes `--no-optional-locks`.**
- Branch: `fix/session-id-hardening-followup`, based on `8a70a5ef6`. **Do not push.**

## Decisions already settled — do not relitigate

These were open in the spec and have been decided. Each is load-bearing for a task below.

| Decision | Settled as |
|---|---|
| 1.3 — out-of-store absolute ref with no `RepoPath` | **Refuse.** Both in-tree callers already set `RepoPath`. |
| 1.1 + `/./` | **Keep the dot-component refusal.** Both `:228` and `:234` become `ErrUnsafeSessionName`. Unit 2's `/./` row drops to "document". |
| 1.4 — already-created `0750` directories | **Accept and document.** No chmod; Entire does not own those directories. |
| 1.8 — how to test the Windows rule | **Parameterise the separator predicate** so it is exercisable from Unix. |
| 1.12 — the deleted model-extraction guard | **Scoping note only.** No code change; the unconfined read belongs to Unit 5. |

Two cascades outside Unit 1, recorded so Unit 2's author does not re-derive them: Unit 2's `/./` row is now **document**, and its `get-session-dir` row is governed by 1.3 — refuse for filesystem-shaped refs rather than skip the preflight.

## File Structure

| File | Responsibility | Tasks |
|---|---|---|
| `cmd/entire/cli/agent/session_store.go` | store containment, ref preflight, name rules | 1, 2, 3, 6, 7 |
| `cmd/entire/cli/agent/session_store_test.go` | tests for the above | 1, 3, 6, 7 |
| `cmd/entire/cli/agent/external/external.go` | external-plugin write preflight call site | 2 |
| `cmd/entire/cli/agent/external/external_test.go` | stub-plugin preflight tests | 2 |
| `cmd/entire/cli/resume.go` | resume orchestration | 4, 8 |
| `cmd/entire/cli/resume_test.go` | resume regression tests | 4, 8 |
| `cmd/entire/cli/strategy/manual_commit_pending.go` | `RestoreLogsOnly` agent fallback | 5 |
| `cmd/entire/cli/strategy/restore_logs_test.go` | tests for the above | 5 |
| `cmd/entire/cli/agent/resolve_session_file_guard_test.go` | source-level caller guard | 9 |
| `CLAUDE.md` | the two false sentences | 3, 9 |
| `docs/architecture/external-agent-protocol.md` | `session_ref` constraints | 2 |

---

### Task 1: Correct the sentinel on the lexical arms of `ValidateExternalSessionRef`

Spec item 1.1. `session_store.go:228` and `:234` report `ErrOutsideSessionStore` for refs that are *inside* the store — a dot component and a rooted ref are malformed names, not containment failures. `f5aa52adc` added `ErrUnsafeSessionName` for exactly this and missed these two arms.

`:237` ("escapes its relative base") keeps `ErrOutsideSessionStore` — that one genuinely is a containment statement.

**Files:**
- Modify: `cmd/entire/cli/agent/session_store.go:226-240`
- Test: `cmd/entire/cli/agent/session_store_test.go`

**Interfaces:**
- Consumes: `agent.ErrUnsafeSessionName` (exists, `session_store.go:58`), `agent.ErrOutsideSessionStore` (exists, `:47`)
- Produces: no signature change. `ValidateExternalSessionRef(ref string) (filesystemPath bool, err error)` is unchanged.

- [ ] **Step 1: Write the failing test**

Add to `cmd/entire/cli/agent/session_store_test.go`:

```go
func TestValidateExternalSessionRef_SentinelNamesTheActualProblem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name       string
		ref        string
		wantUnsafe bool // ErrUnsafeSessionName
		wantOutorside bool // ErrOutsideSessionStore
		wantMsg    string
	}{
		{
			name:       "dot component is a malformed name, not an escape",
			ref:        filepath.Join(dir, ".", "sess.jsonl"),
			wantUnsafe: true,
			wantMsg:    "contains a dot path component",
		},
		{
			name:       "rooted relative ref is a malformed name",
			ref:        string(os.PathSeparator) + "outside.jsonl",
			wantUnsafe: true,
			wantMsg:    "is rooted",
		},
		{
			name:          "escaping its own base IS a containment failure",
			ref:           filepath.Join("..", "outside.jsonl"),
			wantOutorside: true,
			wantMsg:       "escapes its relative base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := agent.ValidateExternalSessionRef(tt.ref)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)

			assert.Equal(t, tt.wantUnsafe, errors.Is(err, agent.ErrUnsafeSessionName),
				"ErrUnsafeSessionName")
			assert.Equal(t, tt.wantOutorside, errors.Is(err, agent.ErrOutsideSessionStore),
				"ErrOutsideSessionStore")

			// The sentinel and the message are separate strings here. Swapping the
			// sentinel while leaving "path is outside..." in the text would pass an
			// errors.Is-only assertion, so pin the text too.
			if tt.wantUnsafe {
				assert.NotContains(t, err.Error(), "path is outside the agent's session directory")
			}
		})
	}
}
```

Note: `filepath.Join(dir, ".", "sess.jsonl")` cleans the dot away. Build the ref by string concatenation instead so the dot survives:

```go
			ref:        dir + string(os.PathSeparator) + "." + string(os.PathSeparator) + "sess.jsonl",
```

Use that form for the first case.

- [ ] **Step 2: Run it and confirm it fails**

```bash
cd /Users/h/Projects/entire/cli-followup
go test ./cmd/entire/cli/agent/ -run TestValidateExternalSessionRef_SentinelNamesTheActualProblem -v
```

Expected: FAIL on the first two cases — `ErrUnsafeSessionName` is false, `ErrOutsideSessionStore` is true, and the message contains "path is outside the agent's session directory".

- [ ] **Step 3: Change the two sentinels**

In `cmd/entire/cli/agent/session_store.go`, inside `ValidateExternalSessionRef`:

```go
	if SessionRefIsFilesystemPath(ref) {
		// A dot component survives no round trip through a plugin that joins or
		// normalizes it, so it is refused rather than cleaned away here.
		//
		// ErrUnsafeSessionName, not ErrOutsideSessionStore: the ref may be well
		// inside the store — store.Name accepts <sessionDir>/./sess.jsonl and
		// resolves it to sess.jsonl. What is wrong with it is its shape.
		for _, component := range strings.Split(filepath.ToSlash(ref), "/") {
			if component == "." || component == ".." {
				return false, fmt.Errorf("%w: %s contains a dot path component", ErrUnsafeSessionName, ref)
			}
		}
		return true, nil
	}
	if os.IsPathSeparator(ref[0]) {
		return false, fmt.Errorf("%w: %s is rooted", ErrUnsafeSessionName, ref)
	}
	if relativeNameEscapes(cleanRelativeName(ref)) {
		// This one IS a containment statement: the ref leaves its own base.
		return false, fmt.Errorf("%w: %s escapes its relative base", ErrOutsideSessionStore, ref)
	}
```

- [ ] **Step 4: Run the test and the package**

```bash
go test ./cmd/entire/cli/agent/ -run TestValidateExternalSessionRef -v
go test ./cmd/entire/cli/agent/... ./cmd/entire/cli/agent/external/
```

Expected: PASS. If `external_test.go` fails, it is asserting the old sentinel — update those assertions to `ErrUnsafeSessionName` in the same commit; do not weaken them to `require.Error`.

- [ ] **Step 5: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/agent/session_store.go cmd/entire/cli/agent/session_store_test.go cmd/entire/cli/agent/external/external_test.go
git commit -m "fix(agent): name the actual problem on the lexical ref arms

A dot component and a rooted ref are malformed names, not containment
failures — store.Name accepts <sessionDir>/./sess.jsonl and resolves it
to sess.jsonl. Report ErrUnsafeSessionName for both and keep
ErrOutsideSessionStore for the arm that genuinely states containment."
```

---

### Task 2: Make the containment half independent of `RepoPath`, and fix the protocol doc

Spec items 1.3 and 1.10, which are one change: the doc comment claims `ValidateExternalSessionRef` is deliberately independent of `RepoPath`, but `external.go:215` skips the store half when `RepoPath == ""`, so a filesystem-shaped ref outside the store is forwarded. **Settled: refuse.**

`external-agent-protocol.md:533` currently says the *correct* thing and must stay correct.

**Files:**
- Modify: `cmd/entire/cli/agent/external/external.go:208-227`
- Modify: `docs/architecture/external-agent-protocol.md` (the `session_ref` constraints paragraph)
- Test: `cmd/entire/cli/agent/external/external_test.go`

**Interfaces:**
- Consumes: `agent.ValidateExternalSessionRef` (Task 1), `agent.ErrOutsideSessionStore`
- Produces: no signature change to `(*Agent).WriteSession`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/entire/cli/agent/external/external_test.go`. The existing helper `newWriteRecordingAgent(t)` returns `(ea, sessionDir, marker)`; the marker file exists only if the `write-session` subprocess ran.

```go
func TestWriteSession_ContainmentDoesNotDependOnRepoPath(t *testing.T) {
	t.Parallel()

	t.Run("absolute ref outside the store is refused without RepoPath", func(t *testing.T) {
		t.Parallel()

		ea, _, marker := newWriteRecordingAgent(t)
		outside := filepath.Join(t.TempDir(), "outside.jsonl")

		err := ea.WriteSession(t.Context(), &agent.AgentSession{
			RepoPath:   "", // the field the check must not depend on
			SessionRef: outside,
		})
		require.ErrorIs(t, err, agent.ErrOutsideSessionStore)

		_, statErr := os.Stat(marker)
		assert.True(t, os.IsNotExist(statErr), "write-session subprocess must not run")
	})

	t.Run("relative escaping ref is refused without RepoPath", func(t *testing.T) {
		t.Parallel()

		ea, _, marker := newWriteRecordingAgent(t)

		err := ea.WriteSession(t.Context(), &agent.AgentSession{
			RepoPath:   "",
			SessionRef: filepath.Join("..", "outside.jsonl"),
		})
		require.ErrorIs(t, err, agent.ErrOutsideSessionStore)

		_, statErr := os.Stat(marker)
		assert.True(t, os.IsNotExist(statErr), "write-session subprocess must not run")
	})

	t.Run("opaque relative key is still forwarded without RepoPath", func(t *testing.T) {
		t.Parallel()

		ea, _, marker := newWriteRecordingAgent(t)

		require.NoError(t, ea.WriteSession(t.Context(), &agent.AgentSession{
			RepoPath:   "",
			SessionRef: "tenant/session-key",
		}))

		_, statErr := os.Stat(marker)
		assert.NoError(t, statErr, "an opaque key must still reach the plugin")
	})
}
```

- [ ] **Step 2: Run and confirm the first subtest fails**

```bash
go test ./cmd/entire/cli/agent/external/ -run TestWriteSession_ContainmentDoesNotDependOnRepoPath -v
```

Expected: the "absolute ref outside the store" case FAILS — `err` is nil and the marker exists. The other two pass already.

- [ ] **Step 3: Refuse instead of skipping**

In `cmd/entire/cli/agent/external/external.go`, replace the `needsStoreCheck && session.RepoPath != ""` block:

```go
	if needsStoreCheck {
		// A filesystem-shaped ref needs the store to decide containment, and
		// without a RepoPath there is no store to resolve. Refuse rather than
		// forward: gating containment on a field both current callers happen to
		// set is a check that disappears for the next caller that does not.
		if session.RepoPath == "" {
			return fmt.Errorf("write-session: validate session reference: %w: %s is filesystem-shaped but no repo path was supplied to resolve the session store",
				agent.ErrOutsideSessionStore, session.SessionRef)
		}
		sessionDir, dirErr := e.getSessionDir(ctx, session.RepoPath)
		if dirErr != nil {
			return fmt.Errorf("write-session: open session store: %w", dirErr)
		}
		store, storeErr := agent.OpenSessionStoreAt(e, sessionDir)
		if storeErr != nil {
			return fmt.Errorf("write-session: open session store: %w", storeErr)
		}
		if err := store.ValidateExternalWriteRef(session.SessionRef); err != nil {
			return fmt.Errorf("write-session: validate session reference: %w", err)
		}
	}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./cmd/entire/cli/agent/external/ -v
```

Expected: PASS, including the pre-existing `TestWriteSession_RejectsUnsafeReferenceBeforeSubprocess` and `TestWriteSession_PreservesOpaqueRelativeReferenceWithMissingStore`.

- [ ] **Step 5: Correct the protocol doc**

In `docs/architecture/external-agent-protocol.md`, in the `session_ref` constraints paragraph, make the text describe today's behaviour per ref-shape. Do **not** describe Unit 2's possible relaxations:

```markdown
`session_ref` is agent-defined. Entire preflights it before invoking
`write-session`, and the rules differ by shape:

- **Opaque relative references** (a database key, a tenant-scoped identifier)
  are forwarded as given. Two rules still apply: the reference must not be
  rooted, and it must not lexically escape its own base. Nothing else is
  checked — no session store is consulted and no component rules are applied,
  so a non-escaping dot segment such as `tenant/../session-key` is forwarded
  unchanged.
- **Filesystem-shaped references** (absolute, or carrying a volume name) must
  contain no `.` or `..` component; must have no component carrying a volume
  separator, a Windows reserved device name, a control character, or a
  trailing period or space; must have no symlinked component or leaf; and must
  resolve inside the directory the plugin itself reported from
  `get-session-dir`. A filesystem-shaped reference supplied without a repo path
  is refused, because there is no session store to resolve it against.

An empty `session_dir` from `get-session-dir`, or a `get-session-dir` that
exits non-zero, fails the write.
```

- [ ] **Step 6: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/agent/external/external.go cmd/entire/cli/agent/external/external_test.go docs/architecture/external-agent-protocol.md
git commit -m "fix(external): containment no longer disappears without a repo path

The doc comment said the preflight was deliberately independent of
RepoPath; the store half was skipped when it was empty, so a
filesystem-shaped ref outside the store was forwarded. Refuse it, and
make the protocol doc describe what the preflight actually does."
```

---

### Task 3: Create nested store directories `0700`

Spec item 1.4. `session_store.go:325` still creates nested directories at `0o750`. For Copilot, Gemini and Pi that is the directory holding the transcript. Already-created directories are **not** chmod'd — settled.

**Files:**
- Modify: `cmd/entire/cli/agent/session_store.go:325`
- Modify: `CLAUDE.md` (the "created `0700`, not `0750`" sentence)
- Test: `cmd/entire/cli/agent/session_store_test.go`

- [ ] **Step 1: Extend the existing permission test**

Find the existing `//go:build !windows` permission test in `session_store_test.go` and add the nested assertion. If none exists, create the file `cmd/entire/cli/agent/session_store_perm_test.go`:

```go
//go:build !windows

package agent_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Not parallel: syscall.Umask is process-global.
func TestSessionStore_WriteFileCreatesDirectories0700(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)

	storeDir := filepath.Join(t.TempDir(), "store")
	store, err := agent.OpenSessionStoreAt(&storeStubAgent{dir: storeDir, resolve: joinResolve}, storeDir)
	require.NoError(t, err)

	require.NoError(t, store.WriteFile("nested/sub/session.jsonl", []byte("hi\n"), 0o600))

	for _, dir := range []string{storeDir, filepath.Join(storeDir, "nested"), filepath.Join(storeDir, "nested", "sub")} {
		info, statErr := os.Stat(dir)
		require.NoError(t, statErr, dir)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}

	info, statErr := os.Stat(filepath.Join(storeDir, "nested", "sub", "session.jsonl"))
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
```

- [ ] **Step 2: Run and confirm the nested levels fail**

```bash
go test ./cmd/entire/cli/agent/ -run TestSessionStore_WriteFileCreatesDirectories0700 -v
```

Expected: FAIL — `nested` and `nested/sub` are `0750`.

- [ ] **Step 3: Change the mode**

`cmd/entire/cli/agent/session_store.go:325`:

```go
		if err := osroot.MkdirAllNoSymlink(root, dir, 0o700); err != nil {
```

- [ ] **Step 4: Run the test**

```bash
go test ./cmd/entire/cli/agent/ -run TestSessionStore_WriteFileCreatesDirectories0700 -v
```

Expected: PASS.

- [ ] **Step 5: Correct the CLAUDE.md sentence**

The branch added "The directory is created `0700`, not `0750`: it holds session transcripts". That was true only of the store root, and only when Entire created it. Replace with:

```markdown
Directories Entire creates inside a session store are `0700`, including nested
levels — Copilot, Gemini and Pi all nest, and the nested directory is the one
holding the transcript. On the common path the AGENT creates the store root, so
Entire's `MkdirAll` is a no-op and the mode is the agent's; a directory that
already exists is not chmod'd, because Entire does not own it. Transcripts
themselves are written `0600`, so the residual exposure is directory listing —
session IDs — not content.
```

- [ ] **Step 6: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/agent/session_store.go cmd/entire/cli/agent/session_store_perm_test.go CLAUDE.md
git commit -m "fix(agent): create nested store directories 0700

The root was fixed; the nested levels that actually hold transcripts for
Copilot, Gemini and Pi were still 0750. Existing directories are left
alone deliberately — Entire does not own them."
```

---

### Task 4: Restore the `agent` attribute on the resume log context

Spec item 1.5. `resume.go:904` dropped `logging.WithAgent`. **The obvious fix is wrong**: `metadata.Agent` is a `types.AgentType` (`"Claude Code"`), `WithAgent` takes a `types.AgentName` (`"claude-code"`), so `types.AgentName(metadata.Agent)` compiles and logs the wrong string.

**Files:**
- Modify: `cmd/entire/cli/resume.go:900-955`
- Test: `cmd/entire/cli/resume_test.go`

**Interfaces:**
- Consumes: `logging.WithAgent(ctx context.Context, agentName types.AgentName) context.Context`, `strategy.ResolveAgentForResume(types.AgentType) (agent.Agent, error)`, `agent.Agent.Name() types.AgentName`
- Produces: no signature change.

- [ ] **Step 1: Write the failing test**

```go
func TestRestoreResumeSessions_LogContextCarriesAgentName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	ctx := logging.WithHandlerForTesting(t.Context(), handler)

	repo, _, _ := setupResumeTestRepo(t, tmpDir, false)
	cleanupResumeTestRepo(t, repo, tmpDir)
	ag := &recordingResumeAgent{sessionDir: filepath.Join(tmpDir, "sessions")}
	t.Cleanup(agent.SnapshotRegistryForTesting())
	agent.Register(ag.Name(), func() agent.Agent { return ag })

	cpID := id.MustCheckpointID("bcbcbcbcbcbc")
	writeCommittedResumeCheckpointWithAgent(t, repo, cpID, "safe-session", time.Now(), ag.Type())
	metadata := &strategy.CheckpointInfo{CheckpointID: cpID, SessionID: "safe-session", Agent: ag.Type()}

	var stdout, stderr bytes.Buffer
	_, _ = restoreResumeSessions(ctx, &stdout, &stderr, metadata, true)

	logged := buf.String()
	require.Contains(t, logged, `"agent":"`+string(ag.Name())+`"`,
		"resume log lines must carry the agent NAME")
	require.NotContains(t, logged, `"agent":"`+string(ag.Type())+`"`,
		"logging the AgentType is the bug this test exists for")
}
```

If `logging` exposes no `WithHandlerForTesting`, use whatever seam `logging/logger_test.go:436` uses to capture attributes and mirror it. Read that test first.

- [ ] **Step 2: Run and confirm it fails**

```bash
go test ./cmd/entire/cli/ -run TestRestoreResumeSessions_LogContextCarriesAgentName -v
```

Expected: FAIL — no `agent` attribute at all.

- [ ] **Step 3: Resolve the agent once, for the log context**

In `restoreResumeSessions`, replace the `logCtx` line:

```go
	logCtx := logging.WithComponent(ctx, "resume")
	// Resolve for the log context only. A failure here is NOT fatal: since the
	// fallback moved below, the multi-session path no longer needs an agent, and
	// an unresolvable one must still reach RestoreLogsOnly's per-session
	// resolution rather than failing the whole resume.
	//
	// metadata.Agent is a types.AgentType ("Claude Code"); WithAgent takes a
	// types.AgentName ("claude-code"). Converting between them compiles and logs
	// a value nothing else in the tree emits — resolve and use ag.Name().
	var resolvedAgent agent.Agent
	if ag, agErr := strategy.ResolveAgentForResume(metadata.Agent); agErr == nil {
		resolvedAgent = ag
		logCtx = logging.WithAgent(logCtx, ag.Name())
	}
```

Then in the fallback branch, reuse it instead of resolving again:

```go
		ag := resolvedAgent
		if ag == nil {
			resolved, err := strategy.ResolveAgentForResume(metadata.Agent)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve agent: %w", err)
			}
			ag = resolved
		}
```

- [ ] **Step 4: Run the test and the resume suite**

```bash
go test ./cmd/entire/cli/ -run 'TestRestoreResumeSessions|TestRestoreSingleSession' -v
```

Expected: PASS, and the shape-B test (unregistered agent type still exits non-zero) must still pass.

- [ ] **Step 5: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/resume.go cmd/entire/cli/resume_test.go
git commit -m "fix(resume): log the agent name, not the agent type

WithAgent takes a types.AgentName (claude-code); CheckpointInfo.Agent is
a types.AgentType (Claude Code). The conversion compiles and logs a
value nothing else emits, so resolve the agent and use ag.Name().
Resolution failure stays non-fatal on the path that no longer needs it."
```

---

### Task 5: Warn when `RestoreLogsOnly` falls back to the checkpoint's agent

Spec item 1.9. The new `point.Agent` fallback is net-positive but silent: a session with no per-session agent is written into the checkpoint agent's directory with no indication.

**Files:**
- Modify: `cmd/entire/cli/strategy/manual_commit_pending.go:395-401`
- Test: `cmd/entire/cli/strategy/restore_logs_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRestoreLogsOnly_WarnsWhenFallingBackToCheckpointAgent(t *testing.T) {
	t.Parallel()

	env := newRestoreLogsEnv(t) // follow the existing helper in this file
	cpID := env.writeTwoSessionCheckpoint(t, withPerSessionAgent(0, ""), withPerSessionAgent(1, env.agentType))

	var stdout, stderr bytes.Buffer
	restored, err := env.strategy.RestoreLogsOnly(t.Context(), &stdout, &stderr, strategy.PendingCheckpoint{
		IsLogsOnly:   true,
		CheckpointID: cpID,
		Agent:        env.agentType,
	}, true)

	require.NoError(t, err)
	require.Len(t, restored, 2, "the fallback must still restore the agentless session")
	assert.Contains(t, stderr.String(), "no agent metadata",
		"the assumption must be visible")
	assert.Contains(t, stderr.String(), string(env.agentType),
		"the warning must name the agent it assumed")
}
```

Adapt the fixture helpers to whatever `restore_logs_test.go` already provides — read it first and reuse, do not add a parallel harness.

- [ ] **Step 2: Run and confirm it fails**

```bash
go test ./cmd/entire/cli/strategy/ -run TestRestoreLogsOnly_WarnsWhenFallingBackToCheckpointAgent -v
```

Expected: FAIL on the stderr assertions.

- [ ] **Step 3: Emit the warning**

In `manual_commit_pending.go`, where the per-session agent is empty and `point.Agent` is used:

```go
		sessionAgentType := content.Metadata.Agent
		if sessionAgentType == "" {
			// Older checkpoints carry no per-session agent. Assume the
			// checkpoint's, which is what main effectively did — but say so: on a
			// mixed-agent checkpoint this writes the transcript into the wrong
			// agent's directory and prints that agent's resume command.
			sessionAgentType = point.Agent
			fmt.Fprintf(errW, "  Warning: session %d (%s) has no agent metadata; assuming %s\n",
				i, sessionID, sessionAgentType)
		}
```

- [ ] **Step 4: Run the test**

```bash
go test ./cmd/entire/cli/strategy/ -run TestRestoreLogsOnly -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/strategy/manual_commit_pending.go cmd/entire/cli/strategy/restore_logs_test.go
git commit -m "fix(strategy): say when the checkpoint's agent is assumed

The per-session agent fallback is right, but silent: on a mixed-agent
checkpoint it writes a transcript into the wrong agent's directory and
prints that agent's resume command. Name the assumption on stderr."
```

---

### Task 6: Unexport `SessionRefIsFilesystemPath`

Spec item 1.6. Exported with no caller outside its own file.

**Files:**
- Modify: `cmd/entire/cli/agent/session_store.go:201`

- [ ] **Step 1: Confirm there is no outside caller**

```bash
git grep -n --untracked --no-color -E 'SessionRefIsFilesystemPath' -- ':(glob)**/*.go'
```

Expected: only `session_store.go` (the declaration and its use inside `ValidateExternalSessionRef`). **If any other file appears, stop** — the spec's premise is wrong and this task needs re-scoping.

- [ ] **Step 2: Rename**

```go
// sessionRefIsFilesystemPath reports whether ref is unambiguously a filesystem
// path rather than an agent-defined opaque key.
func sessionRefIsFilesystemPath(ref string) bool {
	return filepath.IsAbs(ref) || filepath.VolumeName(ref) != ""
}
```

Update the one call site inside `ValidateExternalSessionRef`.

- [ ] **Step 3: Build and test**

```bash
go build ./... && go test ./cmd/entire/cli/agent/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/agent/session_store.go
git commit -m "refactor(agent): unexport sessionRefIsFilesystemPath

No caller outside its own file; it widened the package API for nothing."
```

---

### Task 7: Reject a rooted name in `Name`, and make the rule testable on Unix

Spec item 1.8. `Name("\foo")` on Windows takes the relative branch, cleans to `/foo`, and `relativeNameEscapes` returns false — so a rooted string is returned as a "name inside the store". Downstream coverage catches it, but the wart is real and currently untestable on Unix because `\` is an ordinary byte there. **Settled: parameterise the separator predicate.**

**Files:**
- Modify: `cmd/entire/cli/agent/session_store.go:160-198`
- Test: `cmd/entire/cli/agent/session_store_test.go`

**Interfaces:**
- Produces: `cleanRelativeName(p string, isSeparator func(byte) bool) string` — the added parameter is what makes the Windows rule exercisable from a Unix test. `Name` and `ValidateExternalSessionRef` pass `os.IsPathSeparator`.

- [ ] **Step 1: Write the failing test**

```go
func TestCleanRelativeName_RejectsRootedUnderEitherSeparatorRule(t *testing.T) {
	t.Parallel()

	// windowsSeparator models os.IsPathSeparator on Windows, so the rule is
	// exercisable from a Unix test run. CI cross-compiles for Windows rather
	// than running it, so without this seam the rule ships untested.
	windowsSeparator := func(c byte) bool { return c == '/' || c == '\\' }
	unixSeparator := func(c byte) bool { return c == '/' }

	assert.True(t, agent.RootedRelativeNameForTesting(`\foo`, windowsSeparator),
		`\foo is rooted on Windows`)
	assert.True(t, agent.RootedRelativeNameForTesting("/foo", windowsSeparator))
	assert.True(t, agent.RootedRelativeNameForTesting("/foo", unixSeparator))
	assert.False(t, agent.RootedRelativeNameForTesting(`\foo`, unixSeparator),
		`on Unix a backslash is an ordinary byte, so \foo is a plain name`)
	assert.False(t, agent.RootedRelativeNameForTesting("a/b", windowsSeparator))
}
```

Add the test seam in a non-test file next to the helper (`export_test.go` is the conventional alternative — use whichever this package already does):

```go
// RootedRelativeNameForTesting exposes the rooted-name rule so the Windows
// separator behaviour can be exercised from a Unix test run.
func RootedRelativeNameForTesting(p string, isSeparator func(byte) bool) bool {
	return rootedRelativeName(p, isSeparator)
}
```

- [ ] **Step 2: Run and confirm it fails to compile**

```bash
go test ./cmd/entire/cli/agent/ -run TestCleanRelativeName_RejectsRootedUnderEitherSeparatorRule -v
```

Expected: FAIL — `undefined: agent.RootedRelativeNameForTesting`.

- [ ] **Step 3: Add the rule and thread the predicate**

```go
// rootedRelativeName reports whether a non-absolute path is nonetheless rooted
// under the given separator rule. On Windows "\foo" is not absolute — it has no
// volume — but it is rooted, and Clean+ToSlash turns it into "/foo", which
// relativeNameEscapes does not treat as escaping. Returning it as a name inside
// the store is wrong even though every current downstream caller refuses it.
func rootedRelativeName(p string, isSeparator func(byte) bool) bool {
	return p != "" && isSeparator(p[0])
}
```

In `Name`, inside the relative branch, before `cleanRelativeName`:

```go
	if !filepath.IsAbs(p) && filepath.VolumeName(p) == "" {
		if rootedRelativeName(p, os.IsPathSeparator) {
			return "", fmt.Errorf("%w: %s is rooted", ErrUnsafeSessionName, p)
		}
		cleaned := cleanRelativeName(p)
		...
```

- [ ] **Step 4: Run the tests**

```bash
go test ./cmd/entire/cli/agent/... ./cmd/entire/cli/agent/external/
GOOS=windows go build ./...
```

Expected: PASS, and the Windows cross-compile stays clean.

- [ ] **Step 5: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/agent/session_store.go cmd/entire/cli/agent/session_store_test.go
git commit -m "fix(agent): Name refuses a rooted relative name

On Windows \\foo is not absolute but is rooted, and Clean+ToSlash makes
it /foo, which relativeNameEscapes does not catch. Reject it, and take
the separator predicate as a parameter so the rule is exercisable from a
Unix test run — CI cross-compiles for Windows rather than running it."
```

---

### Task 8: Run the unsafe-ID scan whenever the fallback would be taken

Spec item 1.11. The scan is gated on `restoreErr == nil && len(sessions) == 0`, but the fallback runs on `restoreErr != nil || len(sessions) == 0`. A tampered checkpoint that *also* produces a restore error therefore gets exactly the silent top-level restore the guard exists to prevent.

**Files:**
- Modify: `cmd/entire/cli/resume.go:936-946`
- Test: `cmd/entire/cli/resume_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRestoreResumeSessions_UnsafeIDReportedEvenWhenRestoreErrors(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	repo, _, _ := setupResumeTestRepo(t, tmpDir, false)
	cleanupResumeTestRepo(t, repo, tmpDir)
	ag := &recordingResumeAgent{sessionDir: filepath.Join(tmpDir, "sessions")}
	t.Cleanup(agent.SnapshotRegistryForTesting())
	agent.Register(ag.Name(), func() agent.Agent { return ag })

	// A checkpoint that both (a) makes RestoreLogsOnly return an error and
	// (b) carries an unsafe stored session ID.
	cpID := id.MustCheckpointID("cdcdcdcdcdcd")
	writeCommittedResumeCheckpointWithAgent(t, repo, cpID, "safe-session", time.Now(), ag.Type())
	tamperResumeCheckpointSessionID(t, repo, cpID, 0, ".. ")
	breakResumeCheckpointTranscript(t, repo, cpID) // make RestoreLogsOnly error

	info, err := readCheckpointInfoFromStore(t.Context(), checkpoint.NewGitStore(repo, checkpoint.DefaultV1Refs()), cpID)
	require.NoError(t, err)
	info.SessionID = "safe-session" // the top-level ID is safe; the stored one is not

	var stdout, stderr bytes.Buffer
	restored, err := restoreResumeSessions(t.Context(), &stdout, &stderr, info, false)

	require.Error(t, err, "a tampered ID must be reported, not silently fallen back past")
	assert.Contains(t, err.Error(), "unsafe checkpoint session ID")
	assert.Empty(t, restored)
	assert.Empty(t, ag.writtenSessionIDs, "nothing may be written")
}
```

`breakResumeCheckpointTranscript` may not exist — if not, write it beside `tamperResumeCheckpointSessionID`, making `RestoreLogsOnly` return an error (for example by pointing the session's transcript blob at a missing hash). Read the existing helper first and mirror its shape.

- [ ] **Step 2: Run and confirm it fails**

```bash
go test ./cmd/entire/cli/ -run TestRestoreResumeSessions_UnsafeIDReportedEvenWhenRestoreErrors -v
```

Expected: FAIL — `err` is nil or is the fallback's error, and the top-level session was restored.

- [ ] **Step 3: Widen the gate**

In `restoreResumeSessions`, change the scan's condition to match the fallback's:

```go
	// Run whenever the fallback is about to be taken, not only on the no-error
	// path: a tampered checkpoint that ALSO produces a restore error would
	// otherwise skip this scan and get exactly the silent top-level restore the
	// scan exists to prevent.
	if restoreErr != nil || len(sessions) == 0 {
```

- [ ] **Step 4: Run the whole resume suite**

```bash
go test ./cmd/entire/cli/ -run 'TestRestoreResumeSessions|TestRestoreSingleSession|TestResume' -v
```

Expected: PASS. In particular the three shape tests (A, B, C) and the legacy-fallback test must still pass — widening the gate must not suppress the fallback for safe IDs.

- [ ] **Step 5: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/resume.go cmd/entire/cli/resume_test.go
git commit -m "fix(resume): scan stored session IDs whenever the fallback runs

The scan was gated on restoreErr == nil while the fallback runs on
restoreErr != nil || len(sessions) == 0, so a tampered checkpoint that
also errored got the silent top-level restore the scan exists to stop."
```

---

### Task 9: Widen the `ResolveSessionFile` guard, and correct CLAUDE.md

Spec item 1.2. The guard's pathspec is `cmd/**/*.go`; `e2e/testutil/session_paths.go:45` is a caller outside it. CLAUDE.md:1109 asserts as repo-wide fact that only two sanctioned callers exist. Both are wrong, and both came from a review whose grep was scoped to `cmd/`.

This task lands last because the spec's own item 1.12 is a PR-description note rather than code, and it is folded in here.

**Files:**
- Modify: `cmd/entire/cli/agent/resolve_session_file_guard_test.go:57` (pathspec) and `resolveSessionFileCallers`
- Modify: `CLAUDE.md:1109`

- [ ] **Step 1: Confirm the gap, then widen the pathspec and watch the guard fail**

```bash
git grep -n --untracked --no-color -E '\.ResolveSessionFile\(' -- ':(glob)**/*.go' ':(exclude,glob)**/*_test.go'
```

Expected: three files — `agent/session_store.go`, `agent/external/capabilities.go`, `e2e/testutil/session_paths.go`.

Change the pathspec in `resolve_session_file_guard_test.go`:

```go
	out := testutil.GitGrepGuard(t, repoRoot, "-l", "-E", "--", resolveSessionFilePattern,
		"--", ":(glob)**/*.go", ":(exclude,glob)**/*_test.go")
```

- [ ] **Step 2: Run it and confirm it now fails**

```bash
go test ./cmd/entire/cli/agent/ -run TestResolveSessionFileCallersAreSanctioned -v
```

Expected: FAIL — `e2e/testutil/session_paths.go` is an unsanctioned caller. This failure is the point: it proves the widened pathspec sees what the old one could not.

- [ ] **Step 3: Sanction the harness caller with its reason**

```go
	// The e2e harness resolves a transcript path for an agent it is driving, from
	// checkpoint metadata it wrote itself, in a temp repo it built. Listed rather
	// than excluded by pathspec: new agent E2E runners live under e2e/, which is
	// exactly the copy-paste zone this guard exists for, so the exclusion must be
	// one named file rather than a directory.
	"e2e/testutil/session_paths.go": "e2e harness resolving a path for an agent it drives, from metadata it wrote itself",
```

- [ ] **Step 4: Run the guard, then plant a decoy to prove it still bites**

```bash
go test ./cmd/entire/cli/agent/ -run TestResolveSessionFileCallersAreSanctioned -v

mkdir -p cmd/entire/cli/zzdecoy
cat > cmd/entire/cli/zzdecoy/decoy.go <<'EOF'
package zzdecoy

import "github.com/entireio/cli/cmd/entire/cli/agent"

func Decoy(ag agent.Agent, dir, id string) string { return ag.ResolveSessionFile(dir, id) }
EOF
go test ./cmd/entire/cli/agent/ -run TestResolveSessionFileCallersAreSanctioned -v
rm -rf cmd/entire/cli/zzdecoy
git --no-optional-locks status --porcelain
```

Expected: PASS, then FAIL with the decoy, then PASS again after removal, and a clean status.

- [ ] **Step 5: Correct CLAUDE.md:1109**

Replace the sentence claiming only two sanctioned callers exist:

```markdown
`ResolveSessionFile` takes the session ID on trust — several agents use it as a
DIRECTORY component (Copilot: `<dir>/<id>/events.jsonl`) or return it verbatim
when absolute (Codex, Pi) — so callers must go through
`SessionStore.SessionFile`, which validates the ID first.
`TestResolveSessionFileCallersAreSanctioned` enforces this repo-wide, with an
allowlist naming each sanctioned caller and its reason: `SessionFile` itself,
`external`'s pure delegation, and the e2e harness. The guard's pathspec covers
the whole repository, not just `cmd/` — the caller it was originally written
against lives under `e2e/`.
```

- [ ] **Step 6: Commit**

```bash
mise run fmt && mise run lint
git add cmd/entire/cli/agent/resolve_session_file_guard_test.go CLAUDE.md
git commit -m "fix(agent): the ResolveSessionFile guard covers the whole repo

Its pathspec was cmd/**, and e2e/testutil/session_paths.go calls
ResolveSessionFile with an ID from checkpoint metadata. New agent E2E
runners live under e2e/, which is the copy-paste zone the guard exists
for. CLAUDE.md asserted the gap did not exist; correct it."
```

---

### Task 10: Final verification and the PR description

Spec item 1.12 is a scoping note, not code: `8a70a5ef6` deleted the `sessionIDIsSafe` guard on `ExtractModelFromTranscript`. The deletion is correct — the guard keyed on the session ID while the read takes a path — but the unconfined `os.ReadFile` it nominally covered is now bare, and a reviewer will ask.

- [ ] **Step 1: Full verification against the baseline**

```bash
cd /Users/h/Projects/entire/cli-followup
mise run fmt && mise run lint
go test ./cmd/entire/cli/ ./cmd/entire/cli/strategy/ 2>&1 | grep -cE '^\s*--- FAIL'
GOOS=windows go build ./...
```

Expected: lint clean, Windows cross-compile clean, and the FAIL count **no higher than the baseline you measured before Task 1**. If it is higher, find which task caused it — do not proceed.

- [ ] **Step 2: Write the PR description**

It must state, in its own words:

- Each of the nine code items and what it fixes.
- That `ExtractModelFromTranscript`'s guard was deleted deliberately, that it keyed on the wrong field, and that the unconfined read belongs to the `SessionRef` containment work (spec Unit 5) — so the deletion is not a silent regression.
- That already-created `0750` session directories are **not** chmod'd, and why.
- That a filesystem-shaped `session_ref` supplied without a repo path is now refused, which is a behaviour change for external plugins, with a pointer to the protocol doc.

- [ ] **Step 3: Commit the plan's completion**

```bash
git --no-optional-locks status --porcelain   # must be clean
git log --oneline 8a70a5ef6..HEAD | cat      # expect 9 commits + 2 doc commits
```

**Do not push.**

---

## Self-review

**Spec coverage.** All twelve Unit 1 items map to a task: 1.1→T1, 1.2→T9, 1.3→T2, 1.4→T3, 1.5→T4, 1.6→T6, 1.7→see below, 1.8→T7, 1.9→T5, 1.10→T2, 1.11→T8, 1.12→T10.

**Gap found and recorded:** spec item **1.7** (the backslash doc comment) has no task above. It is a comment-only change with no test, which is why it fell out. Fold it into Task 1's commit — in `validation/validators.go`, rewrite the justification for rejecting `\` so it says the cross-platform rationale (session IDs and names travel in checkpoints to Windows readers; `ValidateSessionID` has always rejected `\` unconditionally) rather than the current "a separator is rejected rather than split on", which is false on Unix where `\` is not a separator.

**Type consistency.** `cleanRelativeName` keeps its single-argument form; the new rule is a separate `rootedRelativeName(p string, isSeparator func(byte) bool) bool`, so Task 7 does not change any existing call site's arity. `ValidateExternalSessionRef` keeps `(bool, error)` throughout. `WithAgent` takes `types.AgentName` in Task 4 and nowhere else.

**Open risk.** Task 5's and Task 8's test fixtures depend on helpers (`newRestoreLogsEnv`, `breakResumeCheckpointTranscript`) that may not exist under those names. Both steps say to read the existing file and mirror it rather than inventing a parallel harness.
