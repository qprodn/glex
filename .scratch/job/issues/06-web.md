# 06 web 页面 + 菜单种子

状态:todo — 依赖 05

- `web/src/pages/monitor/job/`:index.tsx(ProTable:启停开关、立即执行、
  删除)+ JobFormModal(handler 下拉来自 ListJobExecutors、cron 输入
  校验提示、timeout/args/remark)。
- `web/src/pages/monitor/joblog/`:index.tsx(列表筛选 job_id/trigger/
  status,撤下)。
- router/registry.tsx 两行;zh-CN/en-US 菜单与页面文案;错误 reason
  文案(JOB_BUSY 等);services/index.ts 一行。
- seed.sql:监控目录下 任务调度 菜单两页 + 按钮 perms(与
  auth_routes 1:1)+ superadmin 授权。
- 验收:npm run build / tsc 通过;手动 QA 建任务→手动跑→日志出现。
