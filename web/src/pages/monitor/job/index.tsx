import type { Job } from "@/services/job/v1";
import JobFormModal from "@/pages/monitor/job/JobFormModal";
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
import { App, Button, Modal, Switch, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";

/**
 * 定时任务: the back-office view over Scheduled Jobs (ADR 0023). Rows name
 * code-registered executors; firing is best-effort and never backfilled.
 * The switch pauses/resumes the schedule; 立即执行 fires one manual run —
 * its outcome lands in the 任务日志 page, not in this call's response.
 */
const JobList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [editing, setEditing] = useState<Job | undefined>();
  const [modalOpen, setModalOpen] = useState(false);

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<Job>(async (pageSize, pageToken, params) => {
        const set = await services.job.ListJobs({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "name", value: String(params.name ?? "") || undefined },
            { field: "handler", value: String(params.handler ?? "") || undefined },
          ]),
          orderBy: "job_id",
        });
        return { items: set.jobs ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const openCreate = () => {
    setEditing(undefined);
    setModalOpen(true);
  };

  const openEdit = (record: Job) => {
    setEditing(record);
    setModalOpen(true);
  };

  const toggleEnabled = async (record: Job, enabled: boolean) => {
    try {
      await services.job.SetJobEnabled({ jobId: record.jobId, enabled });
      message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
    } catch {
      // The global errorHandler already toasted; swallow so the Switch's
      // onChange does not leak an unhandled rejection (the reload below
      // restores the row's real state).
      return;
    }
    actionRef.current?.reload();
  };

  const runOnce = (record: Job) => {
    Modal.confirm({
      title: intl.formatMessage({
        id: "pages.job.run.confirm",
        defaultMessage: "立即执行一次该任务？",
      }),
      content: intl.formatMessage({
        id: "pages.job.run.hint",
        defaultMessage: "异步执行：调用立即返回，结果写入任务日志。",
      }),
      onOk: async () => {
        await services.job.RunJob({ jobId: record.jobId });
        message.info(
          intl.formatMessage({
            id: "pages.job.run.dispatched",
            defaultMessage: "已触发，结果请看任务日志",
          }),
        );
      },
    });
  };

  const onDelete = (record: Job) => {
    Modal.confirm({
      title: intl.formatMessage({
        id: "pages.job.delete.confirm",
        defaultMessage: "确定删除该任务吗？",
      }),
      content: intl.formatMessage({
        id: "pages.job.delete.hint",
        defaultMessage: "删除是软删除；进行中的运行会完成，之后不再触发。",
      }),
      onOk: async () => {
        await services.job.DeleteJob({ jobId: record.jobId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<Job>[] = [
    { title: "ID", dataIndex: "jobId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.job.name", defaultMessage: "任务名称" }),
      dataIndex: "name",
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "pages.job.handler", defaultMessage: "执行器" }),
      dataIndex: "handler",
      ellipsis: true,
      render: (_, record) => <Typography.Text code>{record.handler}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.job.cron", defaultMessage: "Cron 表达式" }),
      dataIndex: "cron",
      search: false,
      render: (_, record) => <Typography.Text code>{record.cron}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.job.enabled", defaultMessage: "启用" }),
      dataIndex: "enabled",
      width: 90,
      search: false,
      render: (_, record) => (
        <Access key="switch" accessible={access.perm("monitor:job:update")}>
          <Switch
            checked={record.enabled}
            onChange={(checked) => toggleEnabled(record, checked)}
            checkedChildren={intl.formatMessage({ id: "common.on", defaultMessage: "开" })}
            unCheckedChildren={intl.formatMessage({ id: "common.off", defaultMessage: "关" })}
          />
        </Access>
      ),
    },
    {
      title: intl.formatMessage({ id: "pages.job.timeout", defaultMessage: "超时（秒）" }),
      dataIndex: "timeoutSeconds",
      width: 90,
      search: false,
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
      width: 200,
      render: (_, record) => [
        <Access key="update" accessible={access.perm("monitor:job:update")}>
          <a onClick={() => openEdit(record)}>
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="run" accessible={access.perm("monitor:job:run")}>
          <a onClick={() => runOnce(record)}>
            {intl.formatMessage({ id: "pages.job.run", defaultMessage: "立即执行" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("monitor:job:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<Job>
        headerTitle={intl.formatMessage({ id: "menu.monitor.job", defaultMessage: "定时任务" })}
        rowKey="jobId"
        search={{ labelWidth: "auto" }}
        columns={columns}
        actionRef={actionRef}
        request={pager}
        pagination={{ defaultPageSize: 20 }}
        toolBarRender={() => [
          <Access key="create" accessible={access.perm("monitor:job:create")}>
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              {intl.formatMessage({ id: "common.new", defaultMessage: "新建" })}
            </Button>
          </Access>,
        ]}
      />
      {modalOpen && (
        <JobFormModal
          open={modalOpen}
          record={editing}
          onClose={(changed) => {
            setModalOpen(false);
            setEditing(undefined);
            if (changed) {
              actionRef.current?.reload();
            }
          }}
        />
      )}
    </PageContainer>
  );
};

export default JobList;
