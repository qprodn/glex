import type { Router } from "@/services/access/v1";
import type { ReactNode } from "react";

export type UmiRouteData = {
  path?: string;
  name?: string;
  icon?: ReactNode;
  redirect?: string;
  hideInMenu?: boolean;
  alwaysShow?: boolean;
  component?: React.ComponentType;
  routes?: UmiRouteData[];
};

export type TransformDeps = {
  /** Registry lookup for leaf components; missing key => placeholder. */
  resolve: (component: string) => React.ComponentType | undefined;
  /** InnerLink component; the resolved URL map is filled as a side output. */
  innerLinkComponent: React.ComponentType;
  iconFor: (name: string | undefined) => ReactNode;
};

export type TransformResult = {
  routes: UmiRouteData[];
  /** Absolute route path -> embedded URL for InnerLink pages. */
  innerLinks: Record<string, string>;
};

const joinPaths = (parent: string, child: string): string => {
  if (child.startsWith("/")) {
    return child;
  }
  return `${parent.replace(/\/$/, "")}/${child}`;
};

// Directories carry the Layout/ParentView magic components, and the backend
// always emits a children array for them (possibly empty); a menu-frame
// wrapper has children too. Anything else is a leaf.
const isContainer = (router: Router): boolean =>
  (router.children?.length ?? 0) > 0 ||
  router.component === "Layout" ||
  router.component === "ParentView";

// Backend emits RuoYi's "noRedirect" sentinel for directories; an explicit
// other value is honored as-is.
const explicitRedirect = (redirect: string | undefined): string | undefined =>
  redirect && redirect !== "noRedirect" ? redirect : undefined;

const firstLeafPath = (router: Router, parentPath: string): string | undefined => {
  if (!isContainer(router)) {
    return joinPaths(parentPath, router.path ?? "");
  }
  const base = joinPaths(parentPath, router.path ?? "");
  for (const child of router.children ?? []) {
    const found = firstLeafPath(child, base);
    if (found) {
      return found;
    }
  }
  return undefined;
};

/**
 * Pure mapping from the server's Router forest (RuoYi shape, ADR 0019) to
 * Umi runtime route data. Directories ("Layout"/"ParentView") nest their
 * children and redirect to the first reachable leaf; leaves resolve their
 * `component` string through the registry; "InnerLink" leaves remember their
 * URL keyed by absolute path for the iframe page.
 */
export function toUmiRoutes(
  routers: Router[] | undefined,
  deps: TransformDeps,
  parentPath = "",
  innerLinks: Record<string, string> = {},
): TransformResult {
  const routes: UmiRouteData[] = (routers ?? [])
    .map((router): UmiRouteData | undefined => {
      const absolute = joinPaths(parentPath, router.path ?? "");
      const meta = router.meta;

      if (isContainer(router)) {
        const childRoutes = toUmiRoutes(
          router.children,
          deps,
          absolute,
          innerLinks,
        ).routes;
        if (childRoutes.length === 0) {
          return undefined;
        }
        return {
          path: absolute,
          name: meta?.title,
          icon: deps.iconFor(meta?.icon),
          hideInMenu: router.hidden,
          alwaysShow: router.alwaysShow,
          redirect:
            explicitRedirect(router.redirect) ?? firstLeafPath(router, parentPath),
          routes: childRoutes,
        };
      }

      const componentString = router.component ?? "";
      const isInnerLink = componentString === "InnerLink";
      if (isInnerLink && meta?.link) {
        innerLinks[absolute] = meta.link;
      }
      return {
        path: absolute,
        name: meta?.title,
        icon: deps.iconFor(meta?.icon),
        hideInMenu: router.hidden,
        component: isInnerLink
          ? deps.innerLinkComponent
          : deps.resolve(componentString),
      };
    })
    .filter((route): route is UmiRouteData => Boolean(route));
  return { routes, innerLinks };
}
