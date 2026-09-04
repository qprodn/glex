import type { OperLog, OperStatus } from "@/services/audit/v1";
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
import { App, Button, Descriptions, Drawer, Modal, Space, Tag, Typography } from "antd";
import React, { useRef, useState } from "react";

/**
 * 操作日志: the monitoring view over recorded mutating operations
 * (ADR 0012). Rows are minted by the server's operlog middleware and are
 * write-once — the only action is reading a record's detail and withdrawing
 * it (soft delete, ADR 0002; RuoYi's 清空 has no equivalent).
 */
const OperLogList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [detail, setDetail] = useState<OperLog | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<OperLog>(async (pageSize, pageToken, params) => {
        const rawStatus = params.status as number | undefined;
        const status = rawStatus === 1 || rawStatus === 2 ? rawStatus : undefined;
        const set = await services.audit.ListOperLogs({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "operation", value: String(params.operation ?? "") || undefined },
            { field: "oper_user_name", value: String(params.operUserName ?? "") || undefined },
            { field: "ipaddr", value: String(params.ipaddr ?? "") || undefined },
            { field: "status", value: status },
          ]),
          orderBy: "created_at desc",
        });
        return { items: set.operLogs ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const onDelete = (record: OperLog) => {
    Modal.confirm({
      title: intl.formatMessage({
        id: "pages.monitor.operlog.delete.confirm",
        defaultMessage: "确定删除这条操作日志吗？",
      }),
      content: intl.formatMessage({
        id: "pages.monitor.operlog.delete.hint",
        defaultMessage: "删除是软删除：记录保留为撤回历史，不再出现在列表里。",
      }),
      onOk: async () => {
        await services.audit.DeleteOperLog({ operLogId: record.operLogId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const statusTag = (status: OperStatus | undefined) =>
    status === "OPER_STATUS_FAILURE" ? (
      <Tag color="error">{intl.formatMessage({ id: "pages.monitor.status.failure", defaultMessage: "失败" })}</Tag>
    ) : (
      <Tag color="success">{intl.formatMessage({ id: "pages.monitor.status.success", defaultMessage: "成功" })}</Tag>
    );

  const columns: ProColumns<OperLog>[] = [
    { title: "ID", dataIndex: "operLogId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.monitor.operlog.operation", defaultMessage: "操作" }),
      dataIndex: "operation",
      ellipsis: true,
      render: (_, record) => <Typography.Text code>{record.operation}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" }),
      dataIndex: "operUserName",
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
      title: intl.formatMessage({ id: "pages.monitor.operlog.errorMsg", defaultMessage: "错误信息" }),
      dataIndex: "errorMsg",
      search: false,
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "pages.monitor.operlog.latency", defaultMessage: "耗时" }),
      dataIndex: "latencyMs",
      search: false,
      width: 100,
      render: (_, record) => `${record.latencyMs ?? 0} ms`,
    },
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
      width: 140,
      render: (_, record) => [
        <Access key="query" accessible={access.perm("monitor:operlog:query")}>
          <a onClick={() => setDetail(record)}>
            {intl.formatMessage({ id: "pages.monitor.operlog.detail", defaultMessage: "详情" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("monitor:operlog:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  // The redacted request payload is protojson; pretty-print what parses and
  // show the raw text otherwise.
  let requestText = detail?.request ?? "";
  try {
    requestText = JSON.stringify(JSON.parse(requestText), null, 2);
  } catch {
    // keep the raw text
  }

  return (
    <PageContainer>
      <ProTable<OperLog>
        headerTitle={intl.formatMessage({ id: "menu.monitor.operlog", defaultMessage: "操作日志" })}
        rowKey="operLogId"
        search={{ labelWidth: "auto" }}
        columns={columns}
        actionRef={actionRef}
        request={pager}
        pagination={{ defaultPageSize: 20 }}
      />
      <Drawer
        title={intl.formatMessage({ id: "pages.monitor.operlog.detail", defaultMessage: "详情" })}
        width={560}
        open={!!detail}
        onClose={() => setDetail(undefined)}
        footer={
          <Space>
            <Button onClick={() => setDetail(undefined)}>
              {intl.formatMessage({ id: "common.close", defaultMessage: "关闭" })}
            </Button>
          </Space>
        }
      >
        {detail && (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="ID">{detail.operLogId}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.monitor.operlog.operation", defaultMessage: "操作" })}>
              <Typography.Text code>{detail.operation}</Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label="HTTP">
              {detail.httpMethod} {detail.ipaddr}
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" })}>
              {detail.operUserName || "-"}（ID: {detail.operUserId}）
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.monitor.operlog.status", defaultMessage: "状态" })}>
              {statusTag(detail.status)}
            </Descriptions.Item>
            {detail.reason && (
              <Descriptions.Item label="Reason">
                <Typography.Text code>{detail.reason}</Typography.Text>
              </Descriptions.Item>
            )}
            {detail.errorMsg && (
              <Descriptions.Item label={intl.formatMessage({ id: "pages.monitor.operlog.errorMsg", defaultMessage: "错误信息" })}>
                {detail.errorMsg}
              </Descriptions.Item>
            )}
            <Descriptions.Item label="Request ID">
              <Typography.Text copyable code>
                {detail.requestId}
              </Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.monitor.operlog.latency", defaultMessage: "耗时" })}>
              {detail.latencyMs ?? 0} ms
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.monitor.operlog.request", defaultMessage: "请求载荷" })}>
              <Typography.Paragraph>
                <pre style={{ margin: 0, maxWidth: 460, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                  {requestText || "-"}
                </pre>
              </Typography.Paragraph>
            </Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>
    </PageContainer>
  );
};

export default OperLogList;
