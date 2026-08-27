import { AvatarDropdown, AvatarName, Footer, SelectLang } from "@/components";
import InnerLinkPage from "@/router/InnerLinkPage";
import Placeholder from "@/router/Placeholder";
import { componentRegistry } from "@/router/registry";
import {
  getSession,
  getSessionRoutes,
  loadSession,
  setSessionRoutes,
} from "@/router/session";
import { setInnerLinks } from "@/router/innerLinks";
import { iconFor } from "@/router/icons";
import { toUmiRoutes, type UmiRouteData } from "@/router/transform";
import type { CurrentUserInfo } from "@/services/user/v1";
import type { Settings as LayoutSettings } from "@ant-design/pro-components";
import { SettingDrawer } from "@ant-design/pro-components";
import type { RequestConfig, RunTimeLayoutConfig } from "@umijs/max";
import { history, Navigate } from "@umijs/max";
import type { RouteObject } from "react-router-dom";
import React from "react";
import defaultSettings from "../config/defaultSettings";
import { LOGIN_PATH } from "@/utils/token";
import { errorConfig } from "./requestErrorConfig";

const isDev = process.env.NODE_ENV === "development";
const isDevOrTest = isDev || process.env.CI;

/**
 * Delay first paint until the session (getInfo + getRouters) resolves, then
 * transform the route forest — `patchClientRoutes` runs inside oldRender and
 * reads the transformed routes from the session store (ADR 0019). A full page
 * load (login redirect, logout) re-runs the whole pipeline.
 */
export const render = (oldRender: () => void) => {
  loadSession()
    .then((snap) => {
      if (!snap) {
        return;
      }
      const { routes, innerLinks } = toUmiRoutes(
        snap.routerTree.routers,
        {
          resolve: (component) =>
            componentRegistry[component] ??
            componentRegistry[`${component}/index`] ??
            (component ? Placeholder : undefined),
          innerLinkComponent: InnerLinkPage,
          iconFor,
        },
      );
      setInnerLinks(innerLinks);
      setSessionRoutes(routes);
    })
    .finally(oldRender);
};

/**
 * Adapt the menu-shaped transform output (name/icon/routes/component) into
 * react-router 6 route objects (children/element) — the shape patchClientRoutes
 * receives and the layout plugin's menu (children → routes) both consume.
 * Directory redirects become Navigate elements; extra fields (name, icon,
 * hideInMenu, alwaysShow) ride along for ProLayout.
 */
const toReactRouterRoutes = (rs: UmiRouteData[]): RouteObject[] =>
  rs.map((route) => ({
    path: route.path,
    name: route.name,
    icon: route.icon,
    hideInMenu: route.hideInMenu,
    alwaysShow: route.alwaysShow,
    ...(route.redirect
      ? { element: <Navigate to={route.redirect} replace /> }
      : route.component
        ? { element: React.createElement(route.component) }
        : {}),
    ...(route.routes?.length
      ? { children: toReactRouterRoutes(route.routes) }
      : {}),
  })) as RouteObject[];

/** Inject the server-driven routes under the layout container. */
export function patchClientRoutes({ routes }: { routes: RouteObject[] }) {
  const dynamic = getSessionRoutes();
  if (!dynamic.length) {
    return;
  }
  // The layout plugin contributes a top-level layout route at "/" whose
  // children (home, profile, 404, and ours) render inside ProLayout.
  const layoutParent = routes.find(
    (route) => route.path === "/" && Array.isArray(route.children),
  );
  const adapted = toReactRouterRoutes(dynamic);
  if (layoutParent?.children) {
    layoutParent.children.push(...adapted);
  } else {
    routes.unshift(...adapted);
  }
}

/**
 * @see https://umijs.org/docs/api/runtime-config#getinitialstate
 * */
export async function getInitialState(): Promise<{
  settings?: Partial<LayoutSettings>;
  currentUser?: CurrentUserInfo;
  fetchSession?: () => Promise<CurrentUserInfo | undefined>;
}> {
  const fetchSession = async () =>
    (await loadSession())?.info;
  const { location } = history;
  if (location.pathname !== LOGIN_PATH) {
    // loadSession() already ran in render(); reading the store avoids a
    // second round-trip for the same page load.
    return {
      fetchSession,
      currentUser: getSession()?.info,
      settings: defaultSettings as Partial<LayoutSettings>,
    };
  }
  return {
    fetchSession,
    settings: defaultSettings as Partial<LayoutSettings>,
  };
}

// ProLayout 支持的api https://procomponents.ant.design/components/layout
export const layout: RunTimeLayoutConfig = ({
  initialState,
  setInitialState,
}) => {
  return {
    actionsRender: () => [<SelectLang key="SelectLang" />],
    avatarProps: {
      title: <AvatarName />,
      render: (_, avatarChildren) => (
        <AvatarDropdown>{avatarChildren}</AvatarDropdown>
      ),
    },
    waterMarkProps: {
      content: initialState?.currentUser?.user?.nickName,
    },
    footerRender: () => <Footer />,
    onPageChange: () => {
      const { location } = history;
      // 如果没有登录，重定向到 login
      if (!initialState?.currentUser && location.pathname !== LOGIN_PATH) {
        history.push(LOGIN_PATH);
      }
    },
    menuHeaderRender: undefined,
    // 自定义 403 页面
    // unAccessible: <div>unAccessible</div>,
    childrenRender: (children) => {
      return (
        <>
          {children}
          {isDevOrTest && (
            <SettingDrawer
              disableUrlParams
              enableDarkTheme
              settings={initialState?.settings}
              onSettingChange={(settings) => {
                setInitialState((preInitialState) => ({
                  ...preInitialState,
                  settings,
                }));
              }}
            />
          )}
        </>
      );
    },
    ...initialState?.settings,
  };
};

/**
 * @name request 配置，可以配置错误处理
 * 它基于 axios 和 ahooks 的 useRequest 提供了一套统一的网络请求和错误处理方案。
 * @doc https://umijs.org/docs/max/request#配置
 */
export const request: RequestConfig = {
  baseURL: "",
  ...errorConfig,
};
