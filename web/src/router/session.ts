import type { RouterTree } from "@/services/access/v1";
import type { CurrentUserInfo } from "@/services/user/v1";
import { services } from "@/services";
import { getToken } from "@/utils/token";
import type { UmiRouteData } from "./transform";

/**
 * Module-level session store: `render()` (app.tsx) fills it before the first
 * paint so `patchClientRoutes` — which runs earlier than React context exists —
 * can inject the server-driven routes (ADR 0019). `getInitialState` then hands
 * the same objects to theInitialState; nothing is fetched twice.
 */
export type SessionSnapshot = {
  info: CurrentUserInfo;
  routerTree: RouterTree;
};

let snapshot: SessionSnapshot | undefined;
let sessionRoutes: UmiRouteData[] = [];

export function getSession(): SessionSnapshot | undefined {
  return snapshot;
}

export function getSessionRoutes(): UmiRouteData[] {
  return sessionRoutes;
}

export function setSessionRoutes(routes: UmiRouteData[]): void {
  sessionRoutes = routes;
}

export function clearSession(): void {
  snapshot = undefined;
  sessionRoutes = [];
}

/** Fetch getInfo + getRouters once per page load; undefined when not held. */
export async function loadSession(): Promise<SessionSnapshot | undefined> {
  if (!getToken()) {
    clearSession();
    return undefined;
  }
  try {
    const [info, routerTree] = await Promise.all([
      services.user.GetInfo({}),
      services.menu.GetRouters({}),
    ]);
    snapshot = { info, routerTree };
  } catch {
    // A 401 is handled globally (redirect to login); any other failure leaves
    // the app unauthenticated-by-state and the layout guard sends to login.
    snapshot = undefined;
  }
  return snapshot;
}
