import type { JobLog } from "@/services/job/v1";
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

const triggerLabel = (intl: ReturnType<typeof useIntl>, trigger: JobLog["trigger"]) => {
  switch (trigger) {
    case "TRIGGER_MANUAL":
      return intl.formatMessage({ id: "pages.joblog.trigger.manual", defaultMessage: "手动" });
    case "TRIGGER_EXTERNAL":
      return intl.formatMessage({ id: "pages.joblog.trigger.external", defaultMessage: "外部" });
    default:
      return intl.formatMessage({ id: "pages.joblog.trigger.scheduled", defaultMessage: "调度" });
  }
};

/**
 * 任务日志: the run history of Scheduled Jobs (ADR 2023). Rows are written
 * once at completion — no running state, no editing; the only actions are
 * reading a record's detail and withdrawing it (soft delete, ADR 0002).
 */
const JobLogList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [detail, setDetail] = useState<JobLog | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<JobLog>(async (pageSize, pageToken, params) => {
        const jobId = Number(params.jobId);
        const trigger = params.trigger === 1 || params.trigger === 2 ? params.trigger : undefined;
        const status = params.status === 1 || params.status === 2 ? params.status : undefined;
        const set = await services.job.ListJobLogs({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "job_id", value: Number.isFinite(jobId) && jobId > 0 ? jobId : undefined },
            { field: "handler", value: String(params.handler ?? "") || undefined },
            { field: "trigger", value: trigger },
            { field: "status", value: status },
          ]),
          orderBy: "created_at desc",
        });
        return { items: set.jobLogs ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const onDelete = (record: JobLog) => {
    Modal.confirm({
      title: intl.formatMessage({
        id: "pages.joblog.delete.confirm",
        defaultMessage: "确定删除这条任务日志吗？",
      }),
      content: intl.formatMessage({
        id: "pages.joblog.delete.hint",
        defaultMessage: "删除是软删除：记录保留为撤回历史，不再出现在列表里。",
      }),
      onOk: async () => {
        await services.job.DeleteJobLog({ jobLogId: record.jobLogId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const statusTag = (status: JobLog["status"]) =>
    status === "RUN_SUCCESS" ? (
      <Tag color="success">
        {intl.formatMessage({ id: "pages.monitor.status.success", defaultMessage: "成功" })}
      </Tag>
    ) : (
      <Tag color="error">
        {intl.formatMessage({ id: "pages.monitor.status.failure", defaultMessage: "失败" })}
      </Tag>
    );

  const columns: ProColumns<JobLog>[] = [
    { title: "ID", dataIndex: "jobLogId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.joblog.jobId", defaultMessage: "任务 ID" }),
      dataIndex: "jobId",
      width: 110,
      fieldProps: { placeholder: "123" },
    },
    {
      title: intl.formatMessage({ id: "pages.job.handler", defaultMessage: "执行器" }),
      dataIndex: "handler",
      ellipsis: true,
      render: (_, record) => <Typography.Text code>{record.handler}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.joblog.trigger", defaultMessage: "触发" }),
      dataIndex: "trigger",
      width: 90,
      valueType: "select",
      fieldProps: {
        options: [
          {
            label: intl.formatMessage({ id: "pages.joblog.trigger.scheduled", defaultMessage: "调度" }),
            value: 1,
          },
          {
            label: intl.formatMessage({ id: "pages.joblog.trigger.manual", defaultMessage: "手动" }),
            value: 2,
          },
        ],
      },
      render: (_, record) => triggerLabel(intl, record.trigger),
    },
    {
      title: intl.formatMessage({ id: "pages.joblog.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 90,
      valueType: "select",
      fieldProps: {
        options: [
          {
            label: intl.formatMessage({ id: "pages.monitor.status.success", defaultMessage: "成功" }),
            value: 1,
          },
          {
            label: intl.formatMessage({ id: "pages.monitor.status.failure", defaultMessage: "失败" }),
            value: 2,
          },
        ],
      },
      render: (_, record) => statusTag(record.status),
    },
    {
      title: intl.formatMessage({ id: "pages.joblog.message", defaultMessage: "摘要" }),
      dataIndex: "message",
      search: false,
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "pages.joblog.startedAt", defaultMessage: "开始时间" }),
      dataIndex: "startedAt",
      search: false,
      valueType: "dateTime",
      width: 170,
    },
    {
      title: intl.formatMessage({ id: "pages.joblog.duration", defaultMessage: "耗时" }),
      dataIndex: "durationMs",
      search: false,
      width: 100,
      render: (_, record) => `${record.durationMs ?? 0} ms`,
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 140,
      render: (_, record) => [
        <Access key="query" accessible={access.perm("monitor:joblog:list")}>
          <a onClick={() => setDetail(record)}>
            {intl.formatMessage({ id: "pages.joblog.detail", defaultMessage: "详情" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("monitor:joblog:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<JobLog>
        headerTitle={intl.formatMessage({ id: "menu.monitor.joblog", defaultMessage: "任务日志" })}
        rowKey="jobLogId"
        search={{ labelWidth: "auto" }}
        columns={columns}
        actionRef={actionRef}
        request={pager}
        pagination={{ defaultPageSize: 20 }}
      />
      <Drawer
        title={intl.formatMessage({ id: "pages.joblog.detail", defaultMessage: "详情" })}
        width={520}
        open={!!detail}
        onClose={() => setDetail(undefined)}
        footer={
          <Space>
            <Button onClick={() => setDetail(undefined)}>
              {intl.formatMessage({ id: "common.cancel", defaultMessage: "取消" })}
            </Button>
          </Space>
        }
      >
        {detail && (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="ID">{detail.jobLogId}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.joblog.jobId", defaultMessage: "任务 ID" })}>
              {detail.jobId}
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.job.handler", defaultMessage: "执行器" })}>
              <Typography.Text code>{detail.handler}</Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.joblog.trigger", defaultMessage: "触发" })}>
              {triggerLabel(intl, detail.trigger)}
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.joblog.status", defaultMessage: "状态" })}>
              {statusTag(detail.status)}
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.joblog.startedAt", defaultMessage: "开始时间" })}>
              {detail.startedAt}
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.joblog.duration", defaultMessage: "耗时" })}>
              {detail.durationMs ?? 0} ms
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.joblog.message", defaultMessage: "摘要" })}>
              {detail.message || "-"}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>
    </PageContainer>
  );
};

export default JobLogList;
