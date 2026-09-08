# Presigned display URLs for Stored Files — ADR 0008 partially superseded

ADR 0008 decided that bytes flow through the service and nothing is
addressable outside it. That blocks browser-native display: an
`<img src>` cannot carry the localStorage bearer token, so every image
had to be fetched as a blob in JS first — workable for a download page,
clunky for avatars and inline previews. Decided 2026-09-08, aligning the
file module with RuoYi-Vue-Plus's OSS model: the service mints
short-TTL S3 presigned GET URLs for **display**, while uploads stay
service-relayed and attachment downloads stay service-proxied.

## Scope of the change

- **Display URLs** (展示链接): every Stored File response (`UploadFile`,
  `GetFile`, `ListFiles`, `BatchGetFiles`) carries a `url` — a presigned
  GET URL freshly signed at read time (default TTL 120s, per RuoYi's
  private-bucket behavior). URL holders can fetch the bytes from storage
  directly until the TTL lapses.
- **Uploads**: unchanged — relayed through the service, content sniffed,
  fingerprint computed (identical to RuoYi, which also relays uploads).
- **Attachment downloads**: unchanged — the proxied
  `GET /file/files/{id}/content` route keeps Range support and the
  functional-permission chain. Presigning never grants the service's
  `system:file:download` authority; it grants storage access for its TTL.

## Considered Options

- **Chosen:** service-minted presigned GET URLs, short TTL, read-time
  signing. Zero service bandwidth for display; no home-grown crypto
  (SigV4); withdrawn files stop resolving because read-time lookup hits
  the soft-deleted row first.
- **Rejected:** keep ADR 0008 whole and fetch-and-blob in JS. Works, but
  every consumer reimplements plumbing and `<img>`-style components
  (avatars) stay awkward.
- **Rejected:** service query-token (`?token=`) short links. Keeps bytes
  through the service (bandwidth cost ADR 0008's revisit clause was
  about) and requires our own signing/expiry machinery for the same
  shareability exposure.
- **Rejected:** public bucket / permanent URLs. Anonymous forever, no
  revocation window at all.

## Consequences

- A URL is bearer credentials for its TTL: it can be forwarded, and it
  appears in browser history / proxy logs pointing at storage. TTL stays
  short (default 120s) and is read-time minted, never persisted.
- The service no longer sees display reads — operlog/authz cover the
  metadata call that minted the URL, not the storage hit.
- The storage endpoint must be browser-reachable; when it differs from
  the internal endpoint, `file.s3.external_endpoint` rewrites the host
  presigning signs for.
- ADR 0008's "no anonymous addressable URL for anything" clause is
  superseded for display URLs only; its upload and proxy-download
  clauses stand. `sys_file` still stores no `url` column — the URL is
  derived, never persisted.
- The user module stores the avatar as a Stored File id (RuoYi's
  `sys_user.avatar` bigint ossId pattern) and resolves it through the
  crossdomain `file.URLResolver` contract at read time. Write paths do
  not validate the id exists (RuoYi parity): a withdrawn or unknown
  avatar degrades to the default icon rather than failing the user read.
