# Withdrawing a Stored File removes the bytes; the row stays

ADR 0002 forbids hard-deleting table rows — that governs the database. The
stored bytes are not a table row. Decided 2026-08-31: withdrawing a Stored
File soft-deletes its `sys_file` row (the audit trail of who withdrew which
file survives, per ADR 0002) and immediately removes the object from storage.
Ordering: withdraw the row first, then remove the object best-effort — a
failed object removal leaves an invisible orphan (logged for an operator),
never a live record pointing at missing bytes. go-wind-admin's
hard-delete-row-then-remove-object ordering was rejected along with its hard
delete.

## Considered Options

- **Chosen:** soft-delete row + immediate object removal. Preserves audit,
  leaks no storage, needs no cleanup job.
- **Rejected:** soft-delete row + keep object (recycle-bin flavor). Storage
  leaks silently; implies a future purge job nobody asked for.
- **Rejected:** deferred/delayed object removal. Same leak, plus a scheduler.

## Consequences

- A withdrawn Stored File is unrecoverable: the record is memory, not a
  recycle bin.
- If object removal fails, the orphan surfaces in logs only; no automatic
  retry exists.
- Withdrawing the same content again later is simply a new upload — the
  Content Fingerprint never deduplicates.
