/**
 * Functional-permission matching against the handles from `getInfo`
 * (`permissions`, e.g. `system:user:add`). A permission entry may wildcard a
 * segment (`system:*:add`) or everything (`*:*:*` — how the access module
 * projects the superadmin Role). Matching is per segment; `*` matches exactly
 * one segment, so `system:*` does not match `system:user:add`.
 */
export function matchPermission(
  handle: string,
  permissions: readonly string[] | undefined,
): boolean {
  if (!handle) {
    return false;
  }
  const want = handle.split(":");
  return (permissions ?? []).some((held) => {
    if (!held) {
      return false;
    }
    const got = held.split(":");
    if (got.length !== want.length) {
      return false;
    }
    return got.every((segment, i) => segment === "*" || segment === want[i]);
  });
}
