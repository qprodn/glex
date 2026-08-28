import React from 'react';
import { toReactRouterRoutes } from './reactRoutes';
import type { UmiRouteData } from './transform';

const Page = () => null;

const directory: UmiRouteData = {
  path: '/system',
  name: '系统管理',
  redirect: '/system/user',
  routes: [
    { path: '/system/user', name: '用户管理', component: Page },
    { path: '/system/role', name: '角色管理', component: Page },
  ],
};

describe('toReactRouterRoutes', () => {
  it('rides a directory redirect on an index route, never on the parent element', () => {
    const [adapted] = toReactRouterRoutes([directory]);

    // A <Navigate> as the parent element re-fires on every child match and
    // replace-loops history onto the redirect target.
    expect(adapted.element).toBeUndefined();
    expect(adapted.children?.[0]).toMatchObject({ index: true });
    const indexElement = adapted.children?.[0]?.element as React.ReactElement<{
      to: string;
    }>;
    expect(indexElement.props.to).toBe('/system/user');
  });

  it('maps leaf components to route elements and keeps menu metadata', () => {
    const [adapted] = toReactRouterRoutes([directory]);
    const leaves = adapted.children?.slice(-2);
    expect(leaves).toHaveLength(2);
    for (const leaf of leaves ?? []) {
      expect(leaf.path).toBeDefined();
      expect(leaf.element).toEqual(React.createElement(Page));
      expect(leaf.name).toBeDefined();
    }
  });

  it('maps a bare leaf to an element route', () => {
    const [leaf] = toReactRouterRoutes([
      { path: '/monitor/online', name: '在线用户', component: Page },
    ]);
    expect(leaf).toEqual({
      path: '/monitor/online',
      name: '在线用户',
      icon: undefined,
      hideInMenu: undefined,
      alwaysShow: undefined,
      element: React.createElement(Page),
    });
  });
});
