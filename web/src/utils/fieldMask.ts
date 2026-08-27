/**
 * Diff-based field-mask collection for PATCH-style updates. The generated
 * clients send `updateMask` as a query param whose value is a comma-joined
 * list of lowerCamel field paths, and the server applies exactly those paths;
 * sending only what the form actually changed keeps concurrent edits intact
 * and avoids clobbering fields the form never owned.
 */
export function collectUpdateMask(
  before: Record<string, unknown> | undefined,
  after: Record<string, unknown>,
  candidates: readonly string[],
): string {
  const prev = before ?? {};
  return candidates
    .filter((field) => !Object.is(prev[field], after[field]))
    .join(",");
}
