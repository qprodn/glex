# 06 — web: monitor 日志 pages, registry, locales

Status: resolved

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

## Comments

- 2026-09-04: Implemented: registry entries monitor/operlog/index + monitor/loginlog/index; services.audit/loginlog clients; web/src/pages/monitor/operlog (status/operation/user/ip search, detail drawer with pretty-printed redacted request JSON, request-id copy) and monitor/loginlog (user/ip/status search) pages; zh-CN/en-US menu + pages locale entries. Note: v1 ships row delete only — the batch selection UI was dropped to stay consistent with the dict page's single-delete pattern. tsc, biome, jest (33/33), and max build all pass.
