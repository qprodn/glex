# Merchant credentials encrypted at rest — AES-256-GCM with an env-held master key

The pay module lets the back office configure Channel credentials — WeChat
Pay merchant private key, certificate serial and APIv3 key; Alipay RSA
application keys; Stripe secret key — the highest-value secrets this system
will hold. The codebase has no encryption-at-rest precedent (S3 keys sit in
plaintext `configs/config.yaml`, and that stays as is: file-storage keys are
revokeable low-value credentials, payment keys are not). Decided 2026-09-10
at pay module MVP planning:

- Credential material is persisted only as ciphertext, encrypted by the
  application before it reaches PostgreSQL: AES-256-GCM under a single master
  key, a fresh nonce per write, with a `key_version` recorded alongside the
  ciphertext so the key can later rotate or move to a KMS without a flag day.
- The master key is a 32-byte value supplied through the environment
  (`PAY_MASTER_KEY`), never through `configs/config.yaml` or any committed
  file; an absent key makes merchant configuration refuse to operate rather
  than degrade to plaintext.
- Plaintext exists only in memory while building a Channel client. The back
  office API returns masked hints (`****ab12`, "what is set") and never the
  values, and the operlog scrub set covers credential fields so writes are
  never logged.
- Considered and rejected: plaintext columns guarded by DB privileges and
  disk encryption only — logical dumps, backups and replicas carry the values
  in the clear; pgcrypto — SQL-level keys leak through statement logs and
  make the query path key-aware; deferring encryption to a later phase —
  retrofitting means re-collecting every merchant's keys and shipping a
  dual-read migration.
- Consequences: losing the master key loses all configured credentials (they
  must be re-entered); rotation is offline — introduce a new `key_version`,
  rewrite the rows, then retire the old key.
