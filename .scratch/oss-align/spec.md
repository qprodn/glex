# Align the file module with RuoYi-Vue-Plus OSS

Status: ready-for-agent

## What

Adopt the RuoYi-Plus OSS consumption model on top of the existing Stored
File storage, per ADR 0016 (partially superseding ADR 0008's "no
addressable URL" clause):

1. **Display URLs** — `StoredFile.url`: a short-TTL presigned GET URL,
   freshly signed at read time (default 120s), on Upload/Get/List/BatchGet.
2. **BatchGetFiles** — `GET /file/files:batchGet?file_ids=…` (RuoYi's
   `listByIds`): ids → metadata + urls; missing/withdrawn ids are skipped.
3. **Avatar chain** — `sys_user.avatar` bigint (RuoYi's
   pattern); read paths resolve `avatar_url` through the new
   `crossdomain/file.URLResolver`; unknown/withdrawn ids degrade to empty.
4. **Frontend** — shared `fileTransfer` util, reusable `ImageUpload` /
   `FileUpload` components, profile avatar upload, header avatar display.

Upload stays service-relayed; attachment download stays service-proxied
(Range + `system:file:download`). Presigning is display-only.

## Why

Browser-native display (`<img src>`) cannot carry the localStorage bearer
token, and RuoYi alignment means business tables store an id and let the
read path translate it to a fresh URL.

## Non-goals

- No `sys_oss_config`-style multi-backend management — one configured S3
  bucket (config-only `external_endpoint`, `presign_ttl_seconds`).
- No write-time avatar file_id validation (RuoYi parity; read degrades).
- No trimming StoredFile to RuoYi's three-field upload VO.
