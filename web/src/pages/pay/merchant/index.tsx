import { services, asServiceError } from "@/services";
import type { MerchantAccount } from "@/services/pay/v1";
import { CHANNEL_LABELS, MERCHANT_STATUS } from "@/utils/money";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useIntl, useAccess } from "@umijs/max";
import { App, Button, Popconfirm, Tag, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { createTokenPagedRequest } from "@/utils/pager";
import { buildFilter } from "@/utils/aipFilter";
import MerchantFormModal from "./MerchantFormModal";

/**
 * 商户账号管理 (ticket 08): the list renders masked echoes only — display
 * never decrypts (ADR 0018). The form is dynamic, rendered from the
 * channel's config schema (ADR 0021); the probe button runs the
 * Connectivity Test.
 */
const MerchantList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<MerchantAccount | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<MerchantAccount>(async (pageSize, pageToken, params) => {
        const set = await services.pay.ListMerchantAccounts({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "name", value: String(params.name ?? "") || undefined },
            { field: "channel", value: params.channel ? Number(params.channel) : undefined },
            { field: "status", value: params.status === "" || params.status === undefined || params.status === null ? undefined : Number(params.status) },
          ]),
          orderBy: undefined,
        });
        return { items: set.merchants ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const toggleStatus = (record: MerchantAccount) => {
    const next = (record.status ?? 0) === 0 ? 1 : 0;
    services.pay
      .ChangeMerchantAccountStatus({ merchantId: record.merchantId, status: next })
      .then(() => {
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        actionRef.current?.reload();
      })
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  };

  const withdraw = (record: MerchantAccount) => {
    services.pay
      .WithdrawMerchantAccount({ merchantId: record.merchantId })
      .then(() => {
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      })
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  };

  const probe = async (record: MerchantAccount) => {
    const key = `probe-${record.merchantId}`;
    message.loading({ content: intl.formatMessage({ id: "pages.pay.merchant.probing", defaultMessage: "正在测试连通性…" }), key, duration: 0 });
    try {
      const result = await services.pay.ProbeMerchantAccount({ merchantId: record.merchantId });
      message.destroy(key);
      if (result.ok) {
        message.success(
          `${intl.formatMessage({ id: "pages.pay.merchant.probe.ok", defaultMessage: "连接成功" })}（${result.latencyMs}ms）`,
        );
      } else {
        message.error(result.message || intl.formatMessage({ id: "pages.pay.merchant.probe.fail", defaultMessage: "连接失败" }));
      }
    } catch (e) {
      message.destroy(key);
      message.error(asServiceError(e)?.message ?? String(e));
    }
  };

  const columns: ProColumns<MerchantAccount>[] = [
    {
      title: "ID",
      dataIndex: "merchantId",
      width: 180,
      render: (_, r) => <Typography.Text copyable>{String(r.merchantId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.pay.channel", defaultMessage: "渠道" }),
      dataIndex: "channel",
      width: 100,
      render: (_, r) => <Tag>{CHANNEL_LABELS[r.channel ?? 0] ?? r.channel}</Tag>,
      valueEnum: Object.fromEntries(Object.entries(CHANNEL_LABELS).map(([k, v]) => [k, { text: v }])),
    },
    { title: intl.formatMessage({ id: "pages.pay.merchant.name", defaultMessage: "显示名称" }), dataIndex: "name" },
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
      title: intl.formatMessage({ id: "pages.pay.merchant.remark", defaultMessage: "备注" }),
      dataIndex: "remark",
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
      width: 240,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("pay:merchant:update")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="status" accessible={access.perm("pay:merchant:update")}>
          <a onClick={() => toggleStatus(record)}>
            {(record.status ?? 0) === 0
              ? intl.formatMessage({ id: "pages.pay.merchant.disable", defaultMessage: "停用" })
              : intl.formatMessage({ id: "pages.pay.merchant.enable", defaultMessage: "启用" })}
          </a>
        </Access>,
        <Access key="probe" accessible={access.perm("pay:merchant:probe")}>
          <a onClick={() => probe(record)}>
            {intl.formatMessage({ id: "pages.pay.merchant.probe", defaultMessage: "连通测试" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("pay:merchant:remove")}>
          <Popconfirm
            title={intl.formatMessage({
              id: "pages.pay.merchant.withdraw.confirm",
              defaultMessage: "撤除后不可再发起新支付，确定撤除？",
            })}
            onConfirm={() => withdraw(record)}
          >
            <Typography.Link type="danger">{intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}</Typography.Link>
          </Popconfirm>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<MerchantAccount>
        headerTitle={intl.formatMessage({ id: "menu.pay.merchant", defaultMessage: "商户管理" })}
        rowKey="merchantId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("pay:merchant:create")}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(undefined);
                setFormOpen(true);
              }}
            >
              {intl.formatMessage({ id: "pages.pay.merchant.new", defaultMessage: "新建商户" })}
            </Button>
          </Access>,
        ]}
      />
      <MerchantFormModal
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

export default MerchantList;
