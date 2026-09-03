# 06 — web: monitor 日志 pages, registry, locales

Status: claimed

- `web/src/router/registry.tsx`: `monitor/operlog/index`,
  `monitor/loginlog/index` entries.
- `web/src/services/index.ts`: `audit` + `loginlog` generated clients.
- `web/src/pages/monitor/operlog/index.tsx`: ProTable (userName/status
  search), detail drawer with pretty-printed redacted request JSON, row +
  batch soft delete (`monitor:operlog:remove`).
- `web/src/pages/monitor/loginlog/index.tsx`: ProTable (userName/status
  search), row + batch soft delete (`monitor:loginlog:remove`).
- zh-CN / en-US locale entries (menu.monitor.* + pages.monitor.operlog/*).

## Accept

- `pnpm build` (or tsc) passes; pages reachable via the seeded 监控 menus.
