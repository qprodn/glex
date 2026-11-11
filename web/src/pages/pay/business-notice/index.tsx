import { services, asServiceError } from "@/services";
import type { BusinessNotice } from "@/services/pay/v1";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Popconfirm, Tag, Typography } from "antd";
import React, { useRef } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { NOTICE_KIND_REFUND, NOTICE_STATUS_ACCEPTED } from "@/utils/money";
import { createTokenPagedRequest } from "@/utils/pager";

/**
 * Exhausted Business Notifications. The operator sees the work queue and
 * can send one notice again. A success leaves the queue. A failure stays
 * on it.
 */
const ExhaustedNoticeList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<BusinessNotice>(async (pageSize, pageToken) => {
        const set = await services.pay.ListExhaustedBusinessNotices({
          pageSize,
          pageToken: pageToken || undefined,
          filter: undefined,
          orderBy: undefined,
        });
        return { items: set.notices ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const lastResult = (record: BusinessNotice) => {
    if (!record.hasLastAttempt) {
      return intl.formatMessage({ id: "pages.pay.businessNotice.last.none", defaultMessage: "无" });
    }
    const who = record.lastManual
      ? intl.formatMessage({ id: "pages.pay.businessNotice.last.manual", defaultMessage: "手工" })
      : intl.formatMessage({ id: "pages.pay.businessNotice.last.automatic", defaultMessage: "自动" });
    if (record.lastHttpStatus) {
      return `${who} HTTP ${record.lastHttpStatus}`;
    }
    return `${who} ${record.lastError ?? ""}`.trim();
  };

  const resend = (record: BusinessNotice) => {
    services.pay
      .ResendBusinessNotice({ id: record.id })
      .then((notice) => {
        if ((notice.status ?? 0) === NOTICE_STATUS_ACCEPTED) {
          message.success(
            intl.formatMessage({ id: "pages.pay.businessNotice.resend.accepted", defaultMessage: "已送达" }),
          );
        } else {
          message.warning(
            notice.lastError ||
              intl.formatMessage({
                id: "pages.pay.businessNotice.resend.failed",
                defaultMessage: "仍未送达，可以再发",
              }),
          );
        }
        actionRef.current?.reload();
      })
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  };

  const columns: ProColumns<BusinessNotice>[] = [
    {
      title: "ID",
      dataIndex: "id",
      width: 180,
      search: false,
      render: (_, r) => <Typography.Text copyable>{String(r.id)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.businessNotice.kind", defaultMessage: "类型" }),
      dataIndex: "kind",
      width: 90,
      search: false,
      render: (_, r) =>
        r.kind === NOTICE_KIND_REFUND ? (
          <Tag>{intl.formatMessage({ id: "pages.pay.businessNotice.kind.refund", defaultMessage: "退款" })}</Tag>
        ) : (
          <Tag color="blue">
            {intl.formatMessage({ id: "pages.pay.businessNotice.kind.payment", defaultMessage: "支付" })}
          </Tag>
        ),
    },
    {
      title: intl.formatMessage({ id: "menu.pay.application", defaultMessage: "支付应用" }),
      dataIndex: "applicationId",
      search: false,
      render: (_, r) => <Typography.Text copyable>{String(r.applicationId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.payment.no", defaultMessage: "支付单号" }),
      dataIndex: "paymentId",
      search: false,
      render: (_, r) => <Typography.Text copyable>{String(r.paymentId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.refund.no", defaultMessage: "退款单号" }),
      dataIndex: "refundId",
      search: false,
      render: (_, r) => (r.refundId ? <Typography.Text copyable>{String(r.refundId)}</Typography.Text> : ""),
    },
    {
      title: intl.formatMessage({ id: "pages.pay.businessNotice.bizNo", defaultMessage: "业务单号" }),
      dataIndex: "bizNo",
      search: false,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.businessNotice.attemptCount", defaultMessage: "自动次数" }),
      dataIndex: "attemptCount",
      width: 100,
      search: false,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.businessNotice.last", defaultMessage: "最近一次" }),
      dataIndex: "lastError",
      search: false,
      ellipsis: true,
      render: (_, r) => lastResult(r),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 100,
      render: (_, record) => [
        <Access key="resend" accessible={access.perm("pay:business-notice:resend")}>
          <Popconfirm
            title={intl.formatMessage({
              id: "pages.pay.businessNotice.resend.confirm",
              defaultMessage: "按支付应用当前的结果地址再发送一次？",
            })}
            onConfirm={() => resend(record)}
          >
            <a>{intl.formatMessage({ id: "pages.pay.businessNotice.resend", defaultMessage: "重发" })}</a>
          </Popconfirm>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<BusinessNotice>
        headerTitle={intl.formatMessage({ id: "menu.pay.business-notice", defaultMessage: "业务通知" })}
        rowKey="id"
        actionRef={actionRef}
        search={false}
        columns={columns}
        request={pager}
      />
    </PageContainer>
  );
};

export default ExhaustedNoticeList;
