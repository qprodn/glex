import { Navigate } from '@umijs/max';
import React from 'react';
import type { UmiRouteData } from './transform';

/**
 * Structural stand-in for react-router-dom's RouteObject (umi pins
 * react-router-dom internally and does not re-export the type). Extra fields
 * (name, icon, hideInMenu, alwaysShow) ride along for ProLayout.
 */
export type RouteObject = {
  path?: string;
  index?: boolean;
  element?: React.ReactNode;
  children?: RouteObject[];
  [key: string]: unknown;
};

/**
 * Adapt the menu-shaped transform output (name/icon/routes/component) into
 * react-router 6 route objects (children/element) — the shape patchClientRoutes
 * receives and the layout plugin's menu (children → routes) both consume.
 * Leaf components become elements. A directory redirect must ride on an index
 * route: as the parent element it would re-fire on every child match and
 * replace-loop history onto the redirect target.
 */
export const toReactRouterRoutes = (rs: UmiRouteData[]): RouteObject[] =>
  rs.map((route): RouteObject => {
    const common = {
      path: route.path,
      name: route.name,
      icon: route.icon,
      hideInMenu: route.hideInMenu,
      alwaysShow: route.alwaysShow,
    };
    if (route.routes?.length) {
      return {
        ...common,
        children: [
          ...(route.redirect
            ? [
                {
                  index: true,
                  element: <Navigate to={route.redirect} replace />,
                },
              ]
            : []),
          ...toReactRouterRoutes(route.routes),
        ],
      };
    }
    return {
      ...common,
      ...(route.redirect
        ? { element: <Navigate to={route.redirect} replace /> }
        : route.component
          ? { element: React.createElement(route.component) }
          : {}),
    };
  });
