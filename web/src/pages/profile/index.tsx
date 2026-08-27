import type { OnlineSession } from "@/services/authn/v1";
import type { CurrentUser } from "@/services/user/v1";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  PageContainer,
  ProForm,
  ProFormRadio,
  ProFormText,
} from "@ant-design/pro-components";
import { useAccess, useIntl, useModel } from "@umijs/max";
import { App, Card, Popconfirm, Table, Tabs, Typography } from "antd";
import React from "react";

const PROFILE_FIELDS = ["nickName", "email", "phoneNumber", "gender"] as const;

const ProfileForm: React.FC<{ user: CurrentUser }> = ({ user }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  return (
    <ProForm<CurrentUser>
      initialValues={{
        nickName: user.nickName,
        email: user.email,
        phoneNumber: user.phoneNumber,
        gender: user.gender ?? 2,
      }}
      onFinish={async (values) => {
        const mask = collectUpdateMask(user, values, PROFILE_FIELDS);
        if (!mask) {
          message.info(intl.formatMessage({ id: "common.noChange", defaultMessage: "没有变更" }));
          return true;
        }
        await services.user.UpdateUser({
          user: { ...user, ...values, password: undefined },
          updateMask: mask,
        });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        window.location.reload();
        return true;
      }}
      submitter={{ searchConfig: { submitText: intl.formatMessage({ id: "common.save", defaultMessage: "保存" }) } }}
    >
      <ProFormText
        name="nickName"
        label={intl.formatMessage({ id: "pages.user.nickName", defaultMessage: "昵称" })}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="email"
        label={intl.formatMessage({ id: "pages.user.email", defaultMessage: "邮箱" })}
        rules={[{ type: "email" }]}
      />
      <ProFormText
        name="phoneNumber"
        label={intl.formatMessage({ id: "pages.user.phoneNumber", defaultMessage: "手机号" })}
      />
      <ProFormRadio.Group
        name="gender"
        label={intl.formatMessage({ id: "common.gender", defaultMessage: "性别" })}
        options={[
          { value: 0, label: intl.formatMessage({ id: "common.gender.male", defaultMessage: "男" }) },
          { value: 1, label: intl.formatMessage({ id: "common.gender.female", defaultMessage: "女" }) },
          { value: 2, label: intl.formatMessage({ id: "common.gender.unknown", defaultMessage: "未知" }) },
        ]}
      />
    </ProForm>
  );
};

const PasswordForm: React.FC<{ user: CurrentUser }> = ({ user }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  return (
    <ProForm<{ password: string; confirm: string }>
      onFinish={async (values) => {
        await services.user.UpdateUser({
          user: { ...user, password: values.password },
          updateMask: "password",
        });
        // Self password change preserves the current Login Session (authn
        // policy); other sessions of this user are revoked server-side.
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        return true;
      }}
      submitter={{ searchConfig: { submitText: intl.formatMessage({ id: "common.save", defaultMessage: "保存" }) } }}
    >
      <ProFormText.Password
        name="password"
        label={intl.formatMessage({ id: "common.password", defaultMessage: "新密码" })}
        rules={[{ required: true }, { min: 6 }]}
      />
      <ProFormText.Password
        name="confirm"
        label={intl.formatMessage({ id: "pages.profile.password.confirm", defaultMessage: "确认新密码" })}
        dependencies={["password"]}
        rules={[
          { required: true },
          ({ getFieldValue }) => ({
            validator: (_, value) =>
              !value || value === getFieldValue("password")
                ? Promise.resolve()
                : Promise.reject(
                    new Error(
                      intl.formatMessage({
                        id: "pages.profile.password.mismatch",
                        defaultMessage: "两次输入的密码不一致",
                      }),
                    ),
                  ),
          }),
        ]}
      />
      <Typography.Text type="secondary">
        {intl.formatMessage({
          id: "pages.profile.password.hint",
          defaultMessage: "修改密码后当前会话保留，该账号的其他登录会话将被吊销。",
        })}
      </Typography.Text>
    </ProForm>
  );
};

const MySessions: React.FC = () => {
  const intl = useIntl();
  const { message } = App.useApp();
  const [sessions, setSessions] = React.useState<OnlineSession[]>([]);
  const [loading, setLoading] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const set = await services.online.ListMyOnlineSessions({});
      setSessions(set.sessions ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  return (
    <Table<OnlineSession>
      rowKey="tokenId"
      loading={loading}
      dataSource={sessions}
      pagination={false}
      columns={[
        {
          title: "Session ID",
          dataIndex: "tokenId",
          render: (_, record) => (
            <Typography.Text code>{record.tokenId?.slice(0, 12)}</Typography.Text>
          ),
        },
        { title: intl.formatMessage({ id: "pages.monitor.deviceType", defaultMessage: "设备" }), dataIndex: "deviceType" },
        { title: "IP", dataIndex: "ipaddr" },
        { title: "Browser", dataIndex: "browser" },
        { title: "OS", dataIndex: "os" },
        {
          title: intl.formatMessage({ id: "pages.monitor.loginTime", defaultMessage: "登录时间" }),
          dataIndex: "loginTime",
        },
        {
          title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
          render: (_, record) => (
            <Popconfirm
              title={intl.formatMessage({
                id: "pages.profile.session.revoke",
                defaultMessage: "吊销该会话？",
              })}
              onConfirm={async () => {
                await services.online.DeleteMyOnlineSession({ tokenId: record.tokenId });
                message.success(intl.formatMessage({ id: "common.done", defaultMessage: "已吊销" }));
                load();
              }}
            >
              <Typography.Link type="danger">
                {intl.formatMessage({ id: "pages.profile.session.delete", defaultMessage: "下线" })}
              </Typography.Link>
            </Popconfirm>
          ),
        },
      ]}
    />
  );
};

/**
 * Personal center. The profile and password tabs ride UpdateUser, which the
 * route table gates on `system:user:edit` — users without it see only the
 * My Sessions tab (Login-only endpoints).
 */
const Profile: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { initialState } = useModel("@@initialState");
  const user = initialState?.currentUser?.user;
  const canEdit = access.perm("system:user:edit");

  if (!user) {
    return null;
  }

  return (
    <PageContainer>
      <Card>
        <Tabs
          defaultActiveKey={canEdit ? "profile" : "sessions"}
          items={[
            ...(canEdit
              ? [
                  {
                    key: "profile",
                    label: intl.formatMessage({ id: "pages.profile.tab.profile", defaultMessage: "基本资料" }),
                    children: <ProfileForm user={user} />,
                  },
                  {
                    key: "password",
                    label: intl.formatMessage({ id: "pages.profile.tab.password", defaultMessage: "修改密码" }),
                    children: <PasswordForm user={user} />,
                  },
                ]
              : []),
            {
              key: "sessions",
              label: intl.formatMessage({ id: "pages.profile.tab.sessions", defaultMessage: "我的会话" }),
              children: <MySessions />,
            },
          ]}
        />
      </Card>
    </PageContainer>
  );
};

export default Profile;
