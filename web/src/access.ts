import type { CurrentUserInfo } from "@/services/user/v1";
import { matchPermission } from "@/utils/permission";

/**
 * @see https://umijs.org/docs/max/access#access
 */
export default function access(
  initialState: { currentUser?: CurrentUserInfo } | undefined,
) {
  const { currentUser } = initialState ?? {};
  const permissions = currentUser?.permissions;
  return {
    /** Functional-permission gate for `<Access accessible={...}>`. */
    perm: (handle: string) => matchPermission(handle, permissions),
  };
}
