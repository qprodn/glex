/**
 * @name umi 的路由配置
 * @description 只支持 path,component,routes,redirect,wrappers,name,icon 的配置
 * @doc https://umijs.org/docs/guides/routes
 *
 * ADR 0019: business pages are NOT listed here — they arrive from the server
 * (MenuService.GetRouters) and are injected by patchClientRoutes in app.tsx.
 * This file owns only the frame: login, home, personal center, 404.
 */
export default [
  {
    name: "login",
    path: "/user/login",
    layout: false,
    component: "./user/login",
  },
  {
    name: "home",
    icon: "home",
    path: "/",
    component: "./home",
  },
  {
    name: "profile",
    path: "/profile",
    icon: "user",
    component: "./profile",
  },
  {
    component: "./404",
    path: "/*",
  },
];
