import type { OnlineSession } from "@/services/authn/v1";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { createTokenPagedRequest } from "@/utils/pager";
import { PageContainer, type ProColumns, ProTable } from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Modal, Typography } from "antd";
import React from "react";

/**
 * Online Session monitor (the monitoring view over active Login Sessions —
 * GLOSSARY.md): admins list every active session and force-revoke (强退) by the
 * non-secret Session ID. There is deliberately no bulk logout.
 */
const OnlineSessionList: React.FC = () => {
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<OnlineSession>(async (pageSize, pageToken, params) => {
      const set = await services.online.ListOnlineSessions({
        pageSize,
        pageToken: pageToken || undefined,
        filter: buildFilter([
          { field: "user_name", value: String(params.userName ?? "") || undefined },
          { field: "ipaddr", value: String(params.ipaddr ?? "") || undefined },
        ]),
        orderBy: "login_time desc",
      });
      return { items: set.sessions ?? [], nextPageToken: set.nextPageToken };
    }),
    [],
  );

  const onKickout = (record: OnlineSession) => {
    Modal.confirm({
      title: intl.formatMessage(
        {
          id: "pages.monitor.kickout.confirm",
          defaultMessage: "强退「{user}」的会话？",
        },
        { user: record.userName },
      ),
      content: intl.formatMessage({
        id: "pages.monitor.kickout.hint",
        defaultMessage: "该登录会话将立即吊销，对应浏览器下一次请求会跳回登录页。",
      }),
      onOk: async () => {
        await services.online.KickoutOnlineSession({ tokenId: record.tokenId });
        message.success(intl.formatMessage({ id: "common.done", defaultMessage: "已强退" }));
      },
    });
  };

  const columns: ProColumns<OnlineSession>[] = [
    {
      title: "Session ID",
      dataIndex: "tokenId",
      width: 160,
      ellipsis: true,
      render: (_, record) => (
        <Typography.Text code copyable={{ text: record.tokenId }}>
          {record.tokenId?.slice(0, 12)}
        </Typography.Text>
      ),
    },
    { title: intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" }), dataIndex: "userName" },
    { title: intl.formatMessage({ id: "pages.monitor.deviceType", defaultMessage: "设备" }), dataIndex: "deviceType", search: false },
    { title: "IP", dataIndex: "ipaddr" },
    { title: "Browser", dataIndex: "browser", search: false },
    { title: "OS", dataIndex: "os", search: false },
    {
      title: intl.formatMessage({ id: "pages.monitor.loginTime", defaultMessage: "登录时间" }),
      dataIndex: "loginTime",
      search: false,
      valueType: "dateTime",
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 100,
      render: (_, record) => [
        <Access key="kickout" accessible={access.perm("monitor:online:forceLogout")}>
          <Typography.Link type="danger" onClick={() => onKickout(record)}>
            {intl.formatMessage({ id: "pages.monitor.kickout", defaultMessage: "强退" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<OnlineSession>
        headerTitle={intl.formatMessage({ id: "menu.monitor.online", defaultMessage: "在线会话" })}
        rowKey="tokenId"
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
      />
    </PageContainer>
  );
};

export default OnlineSessionList;
