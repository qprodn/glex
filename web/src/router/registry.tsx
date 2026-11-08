import { PageLoading } from "@ant-design/pro-components";
import React from "react";

/**
 * The component registry (ADR 0019): the `component` string stored on a Menu
 * record resolves through this map to a lazily-loaded page. Keys are the page
 * directory path under src/pages — the same value the menu form's component
 * dropdown offers. Adding a page = create it, register it here, then create
 * the Menu record; an unregistered string renders the explicit placeholder.
 */
const lazyPage = (loader: () => Promise<{ default: React.ComponentType }>) => {
  const Lazy = React.lazy(loader);
  const Wrapped: React.FC = () => (
    <React.Suspense fallback={<PageLoading />}>
      <Lazy />
    </React.Suspense>
  );
  return Wrapped;
};

export const componentRegistry: Record<string, React.ComponentType> = {
  "iam/user/index": lazyPage(() => import("@/pages/iam/user")),
  "iam/role/index": lazyPage(() => import("@/pages/iam/role")),
  "iam/menu/index": lazyPage(() => import("@/pages/iam/menu")),
  "iam/dict/index": lazyPage(() => import("@/pages/iam/dict")),
  "iam/dept/index": lazyPage(() => import("@/pages/iam/dept")),
  "iam/post/index": lazyPage(() => import("@/pages/iam/post")),
  "iam/config/index": lazyPage(() => import("@/pages/iam/config")),
  "monitor/online/index": lazyPage(() => import("@/pages/monitor/online")),
  "monitor/operlog/index": lazyPage(() => import("@/pages/monitor/operlog")),
  "monitor/loginlog/index": lazyPage(() => import("@/pages/monitor/loginlog")),
  "monitor/job/index": lazyPage(() => import("@/pages/monitor/job")),
  "monitor/joblog/index": lazyPage(() => import("@/pages/monitor/joblog")),
  "file/index": lazyPage(() => import("@/pages/file")),
  "pay/merchant/index": lazyPage(() => import("@/pages/pay/merchant")),
  "pay/payment/index": lazyPage(() => import("@/pages/pay/payment")),
  "pay/refund/index": lazyPage(() => import("@/pages/pay/refund")),
  "wechat/platform/index": lazyPage(() => import("@/pages/wechat/platform")),
};

/** Keys the menu form offers as its component dropdown options. */
export const registryKeys = Object.keys(componentRegistry).sort();
