import { services, asServiceError } from "@/services";
import type { PayApplication } from "@/services/pay/v1";
import { MERCHANT_STATUS } from "@/utils/money";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useIntl, useAccess } from "@umijs/max";
import { App, Button, Popconfirm, Tag, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { createTokenPagedRequest } from "@/utils/pager";
import { buildFilter } from "@/utils/aipFilter";
import ApplicationFormModal from "./ApplicationFormModal";

/**
 * Pay Application list. Operators create, rename, edit either result
 * address, disable, and withdraw. The Merchant Account binding is fixed
 * after creation.
 */
const PayApplicationList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<PayApplication | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<PayApplication>(async (pageSize, pageToken, params) => {
        const set = await services.pay.ListPayApplications({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "name", value: String(params.name ?? "") || undefined },
            {
              field: "merchant_id",
              value: params.merchantId ? Number(params.merchantId) : undefined,
            },
            {
              field: "status",
              value:
                params.status === "" || params.status === undefined || params.status === null
                  ? undefined
                  : Number(params.status),
            },
          ]),
          orderBy: undefined,
        });
        return { items: set.applications ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const toggleStatus = (record: PayApplication) => {
    const next = (record.status ?? 0) === 0 ? 1 : 0;
    services.pay
      .ChangePayApplicationStatus({ applicationId: record.applicationId, status: next })
      .then(() => {
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        actionRef.current?.reload();
      })
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  };

  const withdraw = (record: PayApplication) => {
    services.pay
      .WithdrawPayApplication({ applicationId: record.applicationId })
      .then(() => {
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      })
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  };

  const columns: ProColumns<PayApplication>[] = [
    {
      title: "ID",
      dataIndex: "applicationId",
      width: 180,
      search: false,
      render: (_, r) => <Typography.Text copyable>{String(r.applicationId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.application.name", defaultMessage: "显示名称" }),
      dataIndex: "name",
    },
    {
      title: intl.formatMessage({ id: "pages.pay.application.merchant", defaultMessage: "商户账号" }),
      dataIndex: "merchantId",
      render: (_, r) => <Typography.Text copyable>{String(r.merchantId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 90,
      render: (_, r) => {
        const st = MERCHANT_STATUS[r.status ?? 0] ?? { label: r.status, color: "default" };
        return <Tag color={st.color}>{st.label}</Tag>;
      },
      valueEnum: Object.fromEntries(Object.entries(MERCHANT_STATUS).map(([k, v]) => [k, { text: v.label }])),
    },
    {
      title: intl.formatMessage({
        id: "pages.pay.application.orderNotifyUrl",
        defaultMessage: "支付结果地址",
      }),
      dataIndex: "orderNotifyUrl",
      ellipsis: true,
      search: false,
    },
    {
      title: intl.formatMessage({
        id: "pages.pay.application.refundNotifyUrl",
        defaultMessage: "退款结果地址",
      }),
      dataIndex: "refundNotifyUrl",
      ellipsis: true,
      search: false,
    },
    {
      title: intl.formatMessage({ id: "common.createTime", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      width: 170,
      search: false,
      render: (_, r) => (r.createdAt ? new Date(r.createdAt).toLocaleString() : ""),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 200,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("pay:application:update")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="status" accessible={access.perm("pay:application:status")}>
          <a onClick={() => toggleStatus(record)}>
            {(record.status ?? 0) === 0
              ? intl.formatMessage({ id: "pages.pay.application.disable", defaultMessage: "停用" })
              : intl.formatMessage({ id: "pages.pay.application.enable", defaultMessage: "启用" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("pay:application:withdraw")}>
          <Popconfirm
            title={intl.formatMessage({
              id: "pages.pay.application.withdraw.confirm",
              defaultMessage: "撤除后离开在用列表，显示名可以再用。确定撤除？",
            })}
            onConfirm={() => withdraw(record)}
          >
            <Typography.Link type="danger">
              {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
            </Typography.Link>
          </Popconfirm>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<PayApplication>
        headerTitle={intl.formatMessage({ id: "menu.pay.application", defaultMessage: "支付应用" })}
        rowKey="applicationId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("pay:application:create")}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(undefined);
                setFormOpen(true);
              }}
            >
              {intl.formatMessage({ id: "pages.pay.application.new", defaultMessage: "新建支付应用" })}
            </Button>
          </Access>,
        ]}
      />
      <ApplicationFormModal
        open={formOpen}
        record={editing}
        onClose={(changed) => {
          setFormOpen(false);
          setEditing(undefined);
          if (changed) actionRef.current?.reload();
        }}
      />
    </PageContainer>
  );
};

export default PayApplicationList;
