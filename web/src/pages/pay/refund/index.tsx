import { services, asServiceError } from "@/services";
import type { Refund } from "@/services/pay/v1";
import { CHANNEL_LABELS, REFUND_STATUS, fenToYuan } from "@/utils/money";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Tag } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { createTokenPagedRequest } from "@/utils/pager";
import { buildFilter } from "@/utils/aipFilter";
import CreateRefundModal from "./CreateRefundModal";

/**
 * 退款单列表 (ticket 09): refunds of a paid payment, together never
 * exceeding it. Status tags read the refund's own state machine; the
 * payment's accumulated sum lives on the payment row.
 */
const RefundList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);
  const [createOpen, setCreateOpen] = useState(false);

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<Refund>(async (pageSize, pageToken, params) => {
        const set = await services.pay.ListRefunds({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "payment_id", value: params.paymentId ? Number(params.paymentId) : undefined },
            { field: "status", value: params.status === "" || params.status === undefined || params.status === null ? undefined : Number(params.status) },
            { field: "channel", value: params.channel ? Number(params.channel) : undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.refunds ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const query = async (record: Refund) => {
    try {
      await services.pay.QueryRefund({ refundId: record.refundId });
      message.success(intl.formatMessage({ id: "pages.pay.refund.queried", defaultMessage: "已向渠道查询并推进状态" }));
      actionRef.current?.reload();
    } catch (e) {
      message.error(asServiceError(e)?.message ?? String(e));
    }
  };

  const columns: ProColumns<Refund>[] = [
    {
      title: intl.formatMessage({ id: "pages.pay.refund.no", defaultMessage: "退款单号" }),
      dataIndex: "refundId",
      width: 180,
      render: (_, r) => String(r.refundId),
      copyable: true,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.payment.no", defaultMessage: "支付单号" }),
      dataIndex: "paymentId",
      width: 180,
      render: (_, r) => String(r.paymentId),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.channel", defaultMessage: "渠道" }),
      dataIndex: "channel",
      width: 100,
      render: (_, r) => <Tag>{CHANNEL_LABELS[r.channel ?? 0] ?? r.channel}</Tag>,
      valueEnum: Object.fromEntries(Object.entries(CHANNEL_LABELS).map(([k, v]) => [k, { text: v }])),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.refund.amount", defaultMessage: "退款金额（元）" }),
      dataIndex: "amount",
      width: 120,
      render: (_, r) => `¥${fenToYuan(r.amount)}`,
      search: false,
    },
    { title: intl.formatMessage({ id: "pages.pay.refund.reason", defaultMessage: "原因" }), dataIndex: "reason", ellipsis: true },
    {
      title: intl.formatMessage({ id: "pages.pay.refund.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 100,
      render: (_, r) => {
        const st = REFUND_STATUS[r.status ?? 0] ?? { label: r.status, color: "default" };
        return <Tag color={st.color}>{st.label}</Tag>;
      },
      valueEnum: Object.fromEntries(Object.entries(REFUND_STATUS).map(([k, v]) => [k, { text: v.label }])),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.refund.refundedAt", defaultMessage: "退款时间" }),
      dataIndex: "refundedAt",
      width: 170,
      search: false,
      render: (_, r) => (r.refundedAt ? new Date(r.refundedAt).toLocaleString() : "—"),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 120,
      render: (_, record) => [
        <Access key="query" accessible={access.perm("pay:refund:query")}>
          <a
            onClick={() => query(record)}
            style={{ opacity: (record.status ?? 0) === 1 ? 1 : 0.45, pointerEvents: (record.status ?? 0) === 1 ? "auto" : "none" }}
          >
            {intl.formatMessage({ id: "pages.pay.payment.query", defaultMessage: "查单" })}
          </a>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<Refund>
        headerTitle={intl.formatMessage({ id: "menu.pay.refund", defaultMessage: "退款单" })}
        rowKey="refundId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="create" accessible={access.perm("pay:refund:create")}>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              {intl.formatMessage({ id: "pages.pay.refund.create", defaultMessage: "发起退款" })}
            </Button>
          </Access>,
        ]}
      />
      <CreateRefundModal
        open={createOpen}
        onClose={(created) => {
          setCreateOpen(false);
          if (created) actionRef.current?.reload();
        }}
      />
    </PageContainer>
  );
};

export default RefundList;
