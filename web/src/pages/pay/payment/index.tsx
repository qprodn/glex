import { services, asServiceError } from "@/services";
import type { Payment } from "@/services/pay/v1";
import { CHANNEL_LABELS, PAYMENT_STATUS, fenToYuan } from "@/utils/money";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Descriptions, Drawer, Popconfirm, Tag, Tooltip, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { createTokenPagedRequest } from "@/utils/pager";
import { buildFilter } from "@/utils/aipFilter";
import CreatePaymentModal, { QrDialog } from "./CreatePaymentModal";

/**
 * 支付单列表 (ticket 09): initiation renders the QR payload from
 * channel_payload; manual close and explicit query follow the backend
 * semantics — terminal payments are irreversible, so their buttons are
 * disabled. Amounts display in yuan at this edge only (ADR 0019).
 */
const PaymentList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);
  const [createOpen, setCreateOpen] = useState(false);
  const [detail, setDetail] = useState<Payment | undefined>();
  const [qr, setQr] = useState<{ codeUrl: string; paymentId: number } | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<Payment>(async (pageSize, pageToken, params) => {
        const set = await services.pay.ListPayments({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "biz_no", value: String(params.bizNo ?? "") || undefined },
            { field: "status", value: params.status === "" || params.status === undefined || params.status === null ? undefined : Number(params.status) },
            { field: "channel", value: params.channel ? Number(params.channel) : undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.payments ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const refresh = () => actionRef.current?.reload();

  const query = async (record: Payment) => {
    try {
      await services.pay.QueryPayment({ paymentId: record.paymentId });
      message.success(intl.formatMessage({ id: "pages.pay.payment.queried", defaultMessage: "已向渠道查询并推进状态" }));
      refresh();
    } catch (e) {
      message.error(asServiceError(e)?.message ?? String(e));
    }
  };

  const close = async (record: Payment) => {
    try {
      await services.pay.ClosePayment({ paymentId: record.paymentId });
      message.success(intl.formatMessage({ id: "pages.pay.payment.closed", defaultMessage: "关单成功" }));
      refresh();
    } catch (e) {
      message.error(asServiceError(e)?.message ?? String(e));
    }
  };

  const columns: ProColumns<Payment>[] = [
    {
      title: intl.formatMessage({ id: "pages.pay.payment.no", defaultMessage: "支付单号" }),
      dataIndex: "paymentId",
      width: 180,
      render: (_, r) => String(r.paymentId),
      copyable: true,
    },
    { title: intl.formatMessage({ id: "pages.pay.payment.bizNo", defaultMessage: "业务单号" }), dataIndex: "bizNo" },
    { title: intl.formatMessage({ id: "pages.pay.payment.subject", defaultMessage: "主题" }), dataIndex: "subject", ellipsis: true },
    {
      title: intl.formatMessage({ id: "pages.pay.channel", defaultMessage: "渠道" }),
      dataIndex: "channel",
      width: 100,
      render: (_, r) => <Tag>{CHANNEL_LABELS[r.channel ?? 0] ?? r.channel}</Tag>,
      valueEnum: Object.fromEntries(Object.entries(CHANNEL_LABELS).map(([k, v]) => [k, { text: v }])),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.payment.amount", defaultMessage: "金额（元）" }),
      dataIndex: "amount",
      width: 110,
      render: (_, r) => `¥${fenToYuan(r.amount)}`,
      search: false,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.payment.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 90,
      render: (_, r) => {
        const st = PAYMENT_STATUS[r.status ?? 0] ?? { label: r.status, color: "default" };
        return <Tag color={st.color}>{st.label}</Tag>;
      },
      valueEnum: Object.fromEntries(Object.entries(PAYMENT_STATUS).map(([k, v]) => [k, { text: v.label }])),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.payment.refunded", defaultMessage: "已退（元）" }),
      dataIndex: "refundedAmount",
      width: 110,
      search: false,
      render: (_, r) => (r.refundedAmount ? `¥${fenToYuan(r.refundedAmount)}` : "—"),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.payment.createdAt", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      width: 170,
      search: false,
      render: (_, r) => (r.createdAt ? new Date(r.createdAt).toLocaleString() : ""),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 240,
      render: (_, record) => {
        const pending = (record.status ?? 0) === 1;
        return [
          <a key="detail" onClick={() => setDetail(record)}>
            {intl.formatMessage({ id: "pages.pay.payment.detail", defaultMessage: "详情" })}
          </a>,
          <Access key="query" accessible={access.perm("pay:payment:query")}>
            <Tooltip title={pending ? undefined : intl.formatMessage({ id: "pages.pay.payment.terminal", defaultMessage: "终态单据不可操作" })}>
              <a onClick={() => query(record)} style={{ opacity: pending ? 1 : 0.45, pointerEvents: pending ? "auto" : "none" }}>
                {intl.formatMessage({ id: "pages.pay.payment.query", defaultMessage: "查单" })}
              </a>
            </Tooltip>
          </Access>,
          <Access key="close" accessible={access.perm("pay:payment:close")}>
            <Popconfirm
              title={intl.formatMessage({ id: "pages.pay.payment.close.confirm", defaultMessage: "确定向渠道关闭该支付单？" })}
              onConfirm={() => close(record)}
              disabled={!pending}
            >
              <Typography.Link type="danger" disabled={!pending}>
                {intl.formatMessage({ id: "pages.pay.payment.close", defaultMessage: "关单" })}
              </Typography.Link>
            </Popconfirm>
          </Access>,
        ];
      },
    },
  ];

  return (
    <PageContainer>
      <ProTable<Payment>
        headerTitle={intl.formatMessage({ id: "menu.pay.payment", defaultMessage: "支付单" })}
        rowKey="paymentId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="create" accessible={access.perm("pay:payment:create")}>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              {intl.formatMessage({ id: "pages.pay.payment.create", defaultMessage: "发起支付" })}
            </Button>
          </Access>,
        ]}
      />
      <CreatePaymentModal
        open={createOpen}
        onClose={(created, codeUrl, paymentId) => {
          setCreateOpen(false);
          if (created) {
            refresh();
            if (codeUrl) setQr({ codeUrl, paymentId: Number(paymentId) });
          }
        }}
        onPaid={refresh}
      />
      <QrDialog
        open={Boolean(qr)}
        codeUrl={qr?.codeUrl ?? ""}
        onClose={(changed) => {
          setQr(undefined);
          if (changed) refresh();
        }}
        paymentId={qr?.paymentId}
      />
      <Drawer
        width={520}
        open={Boolean(detail)}
        onClose={() => setDetail(undefined)}
        title={intl.formatMessage({ id: "pages.pay.payment.detail", defaultMessage: "支付单详情" })}
      >
        {detail && (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.no", defaultMessage: "支付单号" })}>{String(detail.paymentId)}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.bizNo", defaultMessage: "业务单号" })}>{detail.bizNo}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.channel", defaultMessage: "渠道" })}>{CHANNEL_LABELS[detail.channel ?? 0]}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.subject", defaultMessage: "主题" })}>{detail.subject}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.amount", defaultMessage: "金额（元）" })}>¥{fenToYuan(detail.amount)}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.currency", defaultMessage: "币种" })}>{detail.currency}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.status", defaultMessage: "状态" })}>
              {(() => {
                const st = PAYMENT_STATUS[detail.status ?? 0] ?? { label: detail.status, color: "default" };
                return <Tag color={st.color}>{st.label}</Tag>;
              })()}
            </Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.channelTradeNo", defaultMessage: "渠道交易号" })}>{detail.channelTradeNo || "—"}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.payer", defaultMessage: "付款方" })}>{detail.payerId || "—"}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.refunded", defaultMessage: "已退（元）" })}>¥{fenToYuan(detail.refundedAmount)}</Descriptions.Item>
            {detail.failReason && <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.failReason", defaultMessage: "失败原因" })}>{detail.failReason}</Descriptions.Item>}
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.expireTime", defaultMessage: "过期时间" })}>{detail.expireTime ? new Date(detail.expireTime).toLocaleString() : "—"}</Descriptions.Item>
            <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.paidAt", defaultMessage: "支付时间" })}>{detail.paidAt ? new Date(detail.paidAt).toLocaleString() : "—"}</Descriptions.Item>
            {detail.channelPayload && (
              <Descriptions.Item label={intl.formatMessage({ id: "pages.pay.payment.payload", defaultMessage: "渠道返回" })}>
                <Typography.Text copyable style={{ wordBreak: "break-all" }}>
                  {detail.channelPayload}
                </Typography.Text>
              </Descriptions.Item>
            )}
          </Descriptions>
        )}
      </Drawer>
    </PageContainer>
  );
};

export default PaymentList;
