# Atomically replace an existing documentation index

Labels: `triage:ready`, `type:afk`

## What to build

Make repeated indexing safe by publishing each run to a new physical Qdrant collection and exposing it through a stable source alias. Validate that the new collection is complete before atomically switching the alias. A failed run must leave the previous searchable index untouched.

After a successful switch, remove superseded physical collections according to a bounded retention policy so abandoned snapshots do not accumulate.

## Acceptance criteria

- Each indexing run uses a unique physical collection and never writes into the currently active collection.
- The source alias changes only after all expected points are uploaded and validated.
- Embedding, upload, or validation failure deletes or marks the failed collection for cleanup and leaves the old alias target unchanged.
- Successful replacement makes new content visible without a mixed old/new state and cleans up superseded collections according to the documented retention default.
- Qdrant integration tests cover first publication, successful replacement, rollback on failure, and cleanup behavior.

## Blocked by

[Issue 004](004-index-corpus-in-qdrant.md)
