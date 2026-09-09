# Import persists its source, export streams and leaves nothing behind

Spreadsheet data import/export arrives with excelize (RuoYi-Vue-Plus
lineage; its `ruoyi-common-excel` streams both directions over the HTTP
response and persists nothing). Decided 2026-09-09, first consumer the
user module: glex adopts RuoYi's direct streaming for **export** but
routes **import** through the file module — the asymmetry is deliberate.
An import is foreign input entering the system: persisting its source
buys content-level audit (Content Fingerprint, uploader) at trivial cost
through an upload entry point that already exists. An export is derived
data leaving: persisting it bought only re-download and shareability that
no UI exposes, and cost a real permission-surface widening plus storage
growth. ADR 0008's stance is untouched — no download permission changes.

## Scope of the decision

- **Import**: frontend uploads the workbook through the file module
  (biz prefix `import/<module>/`, kept forever as the record of what was
  offered), then passes the **Stored File id** to the business endpoint —
  never the Display URL, which is a short-TTL display artifact (ADR
  0016). The module reads the bytes in-process through a new crossdomain
  `file` content-read contract, streaming into the parser; no temp
  files, no presigned fetch, no self-HTTP.
- **Export**: a hand-written HTTP route (file-transfer-route precedent)
  writes the workbook onto the response via excelize's StreamWriter,
  gated by a `system:<module>:export` functional permission. Nothing is
  stored; the oper log entry minted by the mutating-operation middleware
  is the only trace.
- **Codec**: a stateless `pkg/excelx` — struct-tag-driven rows with an
  explicit-API escape hatch, export to `io.Writer`, import via row
  iterator from `io.Reader`. It makes no capability calls: dict
  value↔label translation tables are injected by the calling
  application service (new crossdomain dict snapshot contract), per
  ADRs 0005/0010.
- **Import semantics**: all-or-nothing on one transaction — every row
  error (row, column, reason) is collected and the whole batch is
  rejected. v1 is insert-only: an existing natural key is a row error,
  no update-support toggle. Imported users get a config-driven initial
  credential; the sheet never carries passwords.
- **Limits**: v1 target 50,000 rows; import rejects beyond the cap, both
  directions stream (never a fully materialized workbook).

## Considered Options

- **Chosen:** streaming export + file-mediated import, stateless codec.
- **Rejected:** file-mediated export (upload the workbook, return the
  file id for the proxied download route). Required either relaxing
  `system:file:download` for export consumers or per-file policy inside
  the file module, plus an export-artifact storage lifecycle — for
  benefits no page exposes. Revisit if an export center / async large
  exports ever land: the codec writes to `io.Writer`, so only the
  delivery layer changes.
- **Rejected:** RuoYi's import flow too (multipart straight to the
  business endpoint). Loses the upload gate's size/MIME checks (xlsx is
  already whitelisted) and the persistent record of what was imported.
- **Rejected:** business module fetching the source by presigned URL or
  self-HTTP. TTL and `external_endpoint` semantics make a Display URL a
  browser artifact, not a server-to-server handle (ADR 0016).
- **Rejected:** partial-success import (RuoYi's listener commits the
  rows it already wrote). Back-office consistency first; a best-effort
  mode can be added later behind an explicit flag.

## Consequences

- The user module seeds `system:user:import` / `system:user:export`
  buttons; import/export are ordinary proto RPCs (file id in, file id or
  row errors out), while the import **template** is a streaming
  hand-written route — an empty-list export of the import VO, gated
  under the import permission.
- One new crossdomain contract on `file` (content read) and one on
  `dict` (label↔value snapshot); the file module's schema and permission
  model are unchanged.
- An export's audit is action-level, not content-level: the oper log
  records who exported with which filters, not the emitted bytes.
- Import sources accumulate under `import/<module>/` with no recycling —
  the system has no scheduled-job facility and v1 accepts the growth.
