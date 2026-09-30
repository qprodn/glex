# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root — the domain glossary and the module map. It is the
  vocabulary authority: module names, domain terms, and the `_Avoid_` synonyms that
  must not be used.
- **`docs/adr/`** — read the ADRs that touch the area you're about to work in. Files are
  `NNNN-<slug>.md`. A decision's standing is recorded in the file itself: a superseding
  ADR names the one it replaces, and an amended ADR carries the amendment inline.

This is a single-context repo: one root `CONTEXT.md` plus one `docs/adr/`. There is no
`CONTEXT-MAP.md` and no per-module `docs/adr/`.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR 0005 (application calls domain services, never repositories) — but worth reopening because…_
