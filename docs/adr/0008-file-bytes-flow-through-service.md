# File bytes flow through the service — no presigned URLs

> Partially superseded by ADR 0016: reads now return a short-TTL presigned
> **display** URL. The upload and proxied-download clauses below stand
> unchanged — this is not a blanket "no presigned URLs" rule any more.

S3 presigned PUT/GET would let browsers talk to storage directly and offload
the service — but the service never sees the bytes: uploader identity,
original filename, and Content Fingerprint could only come from forgeable
client metadata or from S3 event callbacks (pending-confirmation rows, an
auth-exempt callback endpoint, recompute, cleanup cron). go-wind-admin
implemented presigned upload and then disabled it for exactly this reason.
Decided 2026-08-31: uploads are relayed through the service and streamed to
storage while the fingerprint is computed (never fully buffered in memory);
downloads are proxied through the service (Range, disposition). No presigned
URLs, no anonymous addressable URL for anything.

## Considered Options

- **Chosen:** service-relayed upload + proxied download. Metadata is
  authoritative (content type is sniffed from bytes, overriding client
  declarations); access control rides the normal route table.
- **Rejected:** presigned upload + event confirmation. The integrity
  machinery above; rejected by our reference repo after building it.
- **Rejected:** presigned download redirect. Requires the storage endpoint to
  be browser-reachable plus internal/external host rewriting. Revisit when
  bandwidth through the service actually hurts.

## Consequences

- The service pays bandwidth/CPU per transfer, so upload size is capped by
  config (default 100 MiB).
- kratos proto generators cannot express multipart uploads or a streaming
  byte response, so the two transfer endpoints get hand-written HTTP
  handlers — the first deliberate deviation from generated transport in this
  codebase. The hand-written routes register under operation names so the
  auth route table keys on them like any generated route.
- The transfer endpoints exist on HTTP only; gRPC carries the metadata
  operations (list, get, withdraw) alone — relaying up to 100 MiB through a
  single gRPC message would mean raising the default 4 MiB limit for no
  benefit. This is a deliberate deviation from the register-every-module-on-
  both-transports house pattern.
- `sys_file` stores no public `url` column: nothing is addressable except
  through the service.
