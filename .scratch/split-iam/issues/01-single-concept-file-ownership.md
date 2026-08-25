# 01 — Prefactor: give every iam source file a single concept owner

**What to build:** Re-home the flat shared files inside the existing iam
packages so that every source file maps to exactly one future module (user /
access / authn per ADR 0016): the single error block splits by concept so each
error sits beside the code that raises it; the security-change classification
methods (which change revokes which Login Sessions) move beside the domain
objects they classify; the password-hashing helpers move beside their
consumers. Pagination/list-option helpers stay put for now. This is pure file
reorganization inside the same packages — zero behavior change — so the
physical move later becomes per-file renames that git can track.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] No source file in the iam packages mixes concerns from two future modules
- [ ] Security-change classification remains pure domain logic with unchanged behavior
- [ ] `make all` and the full test suite stay green; tests change imports/references only
