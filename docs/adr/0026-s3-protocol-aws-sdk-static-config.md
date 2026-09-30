# S3 protocol via the AWS SDK, one static configuration

The file module needs object storage, and every candidate deployment target
(MinIO, Aliyun OSS, Tencent COS, Cloudflare R2) speaks the S3 protocol. Decided
2026-08-31: one client built on `aws-sdk-go-v2/s3`, pointed at a statically
configured endpoint (credentials, bucket, region, path-style) in
`configs/config.yaml` — no per-cloud SDKs, and no RuoYi-style `sys_oss_config`
runtime-switchable configuration table.

## Considered Options

- **Chosen:** aws-sdk-go-v2/s3 with static config. One dependency, any
  S3-compatible endpoint, same shape as the vendored RuoYi-Vue-Plus Java
  client (also the AWS S3 SDK behind endpoint config).
- **Rejected:** minio-go (go-wind-admin's choice). MinIO-flavored; occasional
  behavioral divergence on non-MinIO S3-compatible stores.
- **Rejected:** per-cloud SDKs behind an OssFactory (RuoYi-Vue-Plus's full
  shape). One SDK per provider to maintain, plus a config CRUD module and
  admin UI — real cost for a single-tenant, single-deployment system.
- **Rejected:** runtime-switchable config table (`sys_oss_config`). YAGNI for
  now; revisit only if multi-provider switching becomes a real requirement.

## Consequences

- Switching storage providers is a config change, never code.
- Provider features outside the S3-compatibility surface are unavailable.
- Size/type policies (max size, MIME whitelist) live in the same static
  config, so changing them is a restart, not a UI toggle.
