# Query baselines (Playback Session Pass)

Run with a migrated database:

```
set SD_TEST_DATABASE_URL=postgres://...
go test ./internal/httpapi -run TestQueryBaselinesExplain -v
```

Plans below are from `EXPLAIN` (not `EXPLAIN ANALYZE`) on modest synthetic rows. Timing numbers are omitted unless this environment produced them.

Indexes added in `migrations/0018_query_indexes.up.sql`:

- `library_grants_user_idx` / `library_grants_role_idx` - grant filtering used on every library-scoped request
- `listen_history_user_track_played_idx` - Home continue `DISTINCT ON (track_id)` ordered by latest play
- `listen_events_user_track_started_idx` - same shape after stats cutover

Already present from earlier waves (not re-created):

- `playback_queue_session_idx` - queue snapshot items
- `listen_history_user_idx` - recap / history lists
- `listen_events_user_started_idx` - event recap
- `acquisition_intents_coalesce_idx` - intent coalesce
- `jobs_coalesce_key_active_uidx` - active `scapex.fetch` coalesce
- `duplicate_review_groups_open_idx` - open duplicate review

## Results

**NOT RUN in the agent environment until `SD_TEST_DATABASE_URL` is supplied.** Re-run the test above and paste `EXPLAIN` lines here if you need a machine-specific record.

Expected access patterns (not timings):

| Query | Expected access |
|---|---|
| Home continue | Index on `(user_id, track_id, played_at)` or `(user_id, played_at)` |
| Queue snapshot | PK lookup on `playback_sessions` + `playback_queue_session_idx` |
| Listen totals / periods | `listen_history_user_idx` or events `(user_id, started_at)` |
| Stats rebuild source | Sequential or index scan of `listen_events` then HashAggregate - acceptable for an admin rebuild |
| Acquisition-intent coalesce | `acquisition_intents_coalesce_idx` |
| Jobs coalesce | `jobs_coalesce_key_active_uidx` |
| Duplicate review open | `duplicate_review_groups_open_idx` |
| Library-grant filter | `library_grants_user_idx` + nested loop to `user_roles` |
