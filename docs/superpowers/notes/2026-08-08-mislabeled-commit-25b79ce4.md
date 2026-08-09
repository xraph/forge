# Correction: commit `25b79ce4` carries the wrong message

**Date:** 2026-08-08
**Branch:** `fix/streaming-frame-decoder`

## What is wrong

Commit `25b79ce4` has the message:

> feat(client): add MergeSpecs and SourceKind

That message is wrong. It describes work in `internal/client/` — the specification
merge primitive — and `25b79ce4` contains none of that. Its actual contents are:

```
extensions/streaming/frame_test.go
extensions/streaming/testdata/constants_fixture.go
internal/router/eventlog_id.go
internal/router/eventlog_id_test.go
```

This is streaming frame-decoder and router event-log work, belonging to a different
effort. Its original commit message is unrecoverable: the pre-amend commit is off
the branch, and the reflog records only that an amend occurred, not the message it
replaced.

The `MergeSpecs` / `SourceKind` work that the message describes is in **`3d5bbd71`**,
which is correct and complete.

## How it happened

Two sessions were working in the same clone at the same time — the same working
directory, and therefore the same git index. `git add` and `git commit` are separate
operations, so one session's `add` can stage another's files, and one session's
`commit` can consume the other's staged index. Both occurred. A `git commit --amend`
issued as recovery then landed on the other session's commit rather than its own,
replacing that commit's message.

No file content was lost. Every commit on the branch has the correct tree; only this
one commit's message describes the wrong change.

## Why it was not repaired

`25b79ce4` sits several commits deep, so rewording it means rewriting every commit
above it on a branch another session was actively committing to. That could strand
or corrupt in-flight work. Leaving an honest note was chosen over a history rewrite
under a live session.

## What changed as a result

Subsequent commits in the unified-streams-hooks-generation plan use
`git commit --only <paths>`, which builds a commit from exactly the named paths and
ignores the rest of the index, rather than `git add` followed by `git commit`.
Implementers are also instructed never to attempt `--amend`, `reset`, or `rebase`
recovery on a shared branch.
