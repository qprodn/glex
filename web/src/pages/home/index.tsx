import { useModel } from "@umijs/max";
import { Card,Descriptions,Tag,Typography } from "antd";
import React from "react";
import { useIntl } from "@umijs/max";

/**
 * Frame route (ADR 0019): the landing page is frontend-owned so a fresh
 * role's first login never lands on a blank menu. It summarizes the current
 * Authorization Snapshot — Role Keys and Functional Permissions carried over
 * from getInfo.
 */
const Home: React.FC = () => {
  const { initialState } = useModel("@@initialState");
  const intl = useIntl();
  const info = initialState?.currentUser;
  const user = info?.user;

  return (
    <Card>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        {intl.formatMessage(
          { id: "pages.home.welcome", defaultMessage: "你好，{name}，欢迎回来" },
          { name: user?.nickName ?? user?.userName ?? "" },
        )}
      </Typography.Title>
      <Descriptions
        column={1}
        items={[
          {
            key: "userName",
            label: intl.formatMessage({ id: "pages.home.userName", defaultMessage: "账号" }),
            children: user?.userName,
          },
          {
            key: "roles",
            label: intl.formatMessage({ id: "pages.home.roles", defaultMessage: "角色" }),
            children: (
              <>
                {(info?.roleKeys ?? []).map((key) => (
                  <Tag key={key} color="blue">
                    {key}
                  </Tag>
                ))}
              </>
            ),
          },
          {
            key: "permissions",
            label: intl.formatMessage({ id: "pages.home.permissions", defaultMessage: "功能权限" }),
            children: intl.formatMessage(
              {
                id: "pages.home.permissions.count",
                defaultMessage: "{count} 个权限句柄",
              },
              { count: info?.permissions?.length ?? 0 },
            ),
          },
          {
            key: "lastLogin",
            label: intl.formatMessage({ id: "pages.home.lastLogin", defaultMessage: "上次登录" }),
            children: user?.loginDate
              ? `${user.loginDate}（${user.loginIp ?? "-"}）`
              : "-",
          },
        ]}
      />
    </Card>
  );
};

export default Home;
