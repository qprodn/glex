import type { LoginLog, LoginStatus } from "@/services/audit/v1";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { createTokenPagedRequest } from "@/utils/pager";
import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Modal, Tag, Typography } from "antd";
import React, { useRef } from "react";

/**
 * 登录日志: the monitoring view over recorded sign-ins and session ends
 * (ADR 0012). authn mints the rows; they are write-once — the only action
 * is withdrawing one (soft delete, ADR 0002).
 */
const LoginLogList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<LoginLog>(async (pageSize, pageToken, params) => {
        const rawStatus = params.status as number | undefined;
        const status = rawStatus === 1 || rawStatus === 2 ? rawStatus : undefined;
        const set = await services.loginlog.ListLoginLogs({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "user_name", value: String(params.userName ?? "") || undefined },
            { field: "ipaddr", value: String(params.ipaddr ?? "") || undefined },
            { field: "status", value: status },
          ]),
          orderBy: "created_at desc",
        });
        return { items: set.loginLogs ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const onDelete = (record: LoginLog) => {
    Modal.confirm({
      title: intl.formatMessage({
        id: "pages.monitor.loginlog.delete.confirm",
        defaultMessage: "确定删除这条登录日志吗？",
      }),
      content: intl.formatMessage({
        id: "pages.monitor.loginlog.delete.hint",
        defaultMessage: "删除是软删除：记录保留为撤回历史，不再出现在列表里。",
      }),
      onOk: async () => {
        await services.loginlog.DeleteLoginLog({ loginLogId: record.loginLogId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const statusTag = (status: LoginStatus | undefined) =>
    status === "LOGIN_STATUS_FAILURE" ? (
      <Tag color="error">{intl.formatMessage({ id: "pages.monitor.status.failure", defaultMessage: "失败" })}</Tag>
    ) : (
      <Tag color="success">{intl.formatMessage({ id: "pages.monitor.status.success", defaultMessage: "成功" })}</Tag>
    );

  const columns: ProColumns<LoginLog>[] = [
    { title: "ID", dataIndex: "loginLogId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" }),
      dataIndex: "userName",
    },
    { title: "IP", dataIndex: "ipaddr" },
    {
      title: intl.formatMessage({ id: "pages.monitor.operlog.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 90,
      valueType: "select",
      fieldProps: {
        options: [
          { label: intl.formatMessage({ id: "pages.monitor.status.success", defaultMessage: "成功" }), value: 1 },
          { label: intl.formatMessage({ id: "pages.monitor.status.failure", defaultMessage: "失败" }), value: 2 },
        ],
      },
      render: (_, record) => statusTag(record.status),
    },
    {
      title: intl.formatMessage({ id: "pages.monitor.loginlog.msg", defaultMessage: "结果描述" }),
      dataIndex: "msg",
      ellipsis: true,
      search: false,
    },
    { title: "Browser", dataIndex: "browser", search: false },
    { title: "OS", dataIndex: "os", search: false },
    {
      title: intl.formatMessage({ id: "common.createdAt", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      search: false,
      valueType: "dateTime",
      width: 170,
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 90,
      render: (_, record) => [
        <Access key="remove" accessible={access.perm("monitor:loginlog:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<LoginLog>
        headerTitle={intl.formatMessage({ id: "menu.monitor.loginlog", defaultMessage: "登录日志" })}
        rowKey="loginLogId"
        search={{ labelWidth: "auto" }}
        columns={columns}
        actionRef={actionRef}
        request={pager}
        pagination={{ defaultPageSize: 20 }}
      />
    </PageContainer>
  );
};

export default LoginLogList;
