import { services } from "@/services";
import type { WechatCapability, WechatDispatch } from "@/services/wechat/v1";
import { PageContainer, ProTable } from "@ant-design/pro-components";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { Tag } from "antd";
import React, { useRef } from "react";
import { createTokenPagedRequest } from "@/utils/pager";

const CHANNEL_LABEL: Record<string, string> = {
  WECHAT_CAPABILITY_TEMPLATE_MESSAGE: "模板消息",
  WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION: "订阅通知",
  WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE: "订阅消息",
};

/**
 * 微信下发: every send attempt, including those whose app has been
 * withdrawn. Filter by app. Acceptance is not delivery.
 */
const DispatchList: React.FC = () => {
  const intl = useIntl();
  const actionRef = useRef<ActionType>(undefined);

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<WechatDispatch>(async (pageSize, pageToken, params) => {
        const raw = String(params.appId ?? "").trim();
        const appId = raw ? Number(raw) : undefined;
        const set = await services.wechat.ListWechatDispatches({
          pageSize,
          pageToken: pageToken || undefined,
          filter: undefined,
          orderBy: undefined,
          appId: appId && !Number.isNaN(appId) ? appId : undefined,
        });
        return { items: set.dispatches ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const columns: ProColumns<WechatDispatch>[] = [
    {
      title: "应用",
      dataIndex: "appId",
      render: (_, r) => String(r.appId ?? ""),
    },
    {
      title: "openid",
      dataIndex: "openid",
      search: false,
    },
    {
      title: "模板",
      dataIndex: "libraryTemplateId",
      search: false,
    },
    {
      title: "种类",
      dataIndex: "channel",
      search: false,
      render: (_, r) => CHANNEL_LABEL[r.channel as WechatCapability] ?? "",
    },
    {
      title: "内容",
      dataIndex: "content",
      search: false,
      ellipsis: true,
    },
    {
      title: "网页 URL",
      dataIndex: "webUrl",
      search: false,
      ellipsis: true,
    },
    {
      title: "小程序页面",
      dataIndex: "miniProgramPage",
      search: false,
      ellipsis: true,
    },
    {
      title: "受理",
      dataIndex: "accepted",
      search: false,
      render: (_, r) =>
        r.accepted ? <Tag color="success">已受理</Tag> : <Tag>已拒绝</Tag>,
    },
    {
      title: "msgid",
      dataIndex: "msgid",
      search: false,
    },
    {
      title: intl.formatMessage({ id: "common.createTime", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      search: false,
      render: (_, r) => (r.createdAt ? new Date(r.createdAt).toLocaleString() : ""),
    },
  ];

  return (
    <PageContainer>
      <ProTable<WechatDispatch>
        headerTitle="微信下发"
        rowKey="dispatchId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
      />
    </PageContainer>
  );
};

export default DispatchList;
