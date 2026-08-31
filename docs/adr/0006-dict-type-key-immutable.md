# A dict type's key is immutable once created

RuoYi-Vue-Plus lets an admin rename a dictionary type's key (`dict_type`)
and cascades the rewrite across every `sys_dict_data` row plus both cache
groups. We deliberately did not port that (decided 2026-08-31): a Dict Type's
key is **immutable after creation**; only its display name (`dict_name`) and
remark are editable. The key is an identifier that pages and future
validations reference — it appears in frontend code, seeds, and (eventually)
validation rules, and identifiers that drift are worse than identifiers that
are annoying to change. This also lets the partial unique indexes
(`sys_dict_type.dict_type`, `sys_dict_data (dict_type, dict_value)`, each
`WHERE deleted_at IS NULL`) stay untouched by renames, and keeps the audit
trail from rewriting history on a rename.

## Considered Options

- **Chosen:** immutable key. Editing a type disables the key input; renaming
  means withdraw and recreate under the new key.
- **Rejected:** RuoYi's cascade rename. Fidelity to the reference design, but
  it rewrites every entry's `dict_type` (a bulk mutation of active rows for a
  cosmetic want), must dodge withdrawn rows sharing the target key, and — with
  any future read cache — turns every rename into a coordinated cache
  invalidation.

## Consequences

- The admin UI must disable the key field on edit (create-only input).
- Seeds and frontend references to keys (e.g. `sys_normal_disable`) are safe
  to hardcode.
- Same rule one level down: a Dict Entry's owning type key is set at creation
  and not editable afterwards — moving an entry between types is
  withdraw-and-recreate, not an update.
