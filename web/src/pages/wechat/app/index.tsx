import { services, asServiceError } from "@/services";
import type { WechatApp, WechatAppPushSecret, WechatCapability } from "@/services/wechat/v1";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useIntl, useAccess } from "@umijs/max";
import { App, Button, Popconfirm, Tag, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { createTokenPagedRequest } from "@/utils/pager";
import { buildFilter } from "@/utils/aipFilter";
import AppFormModal from "./AppFormModal";
import AuthorizeModal from "./AuthorizeModal";
import PushSecretModal from "./PushSecretModal";

const TYPE_LABEL: Record<string, string> = {
  WECHAT_APP_TYPE_MINI_PROGRAM: "小程序",
  WECHAT_APP_TYPE_OFFICIAL_ACCOUNT: "公众号",
};

const KIND_LABEL: Record<string, string> = {
  WECHAT_ACCOUNT_KIND_SERVICE: "服务号",
  WECHAT_ACCOUNT_KIND_SUBSCRIPTION: "订阅号",
};

const CONNECTION_LABEL: Record<string, string> = {
  WECHAT_CONNECTION_DIRECT_HOLD: "直连",
  WECHAT_CONNECTION_PLATFORM_AUTHORIZATION: "平台授权",
};

const CAPABILITY_LABEL: Record<WechatCapability, string> = {
  WECHAT_CAPABILITY_UNSPECIFIED: "",
  WECHAT_CAPABILITY_CUSTOM_MENU: "自定义菜单",
  WECHAT_CAPABILITY_TEMPLATE_MESSAGE: "模板消息",
  WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION: "订阅通知",
  WECHAT_CAPABILITY_WEB_AUTHORIZATION: "网页授权",
  WECHAT_CAPABILITY_MINI_PROGRAM_LOGIN: "小程序登录",
  WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE: "订阅消息",
};

const platformAuthorization = (record: WechatApp) =>
  record.connection === "WECHAT_CONNECTION_PLATFORM_AUTHORIZATION";

/**
 * 微信应用: Direct Hold and Platform Authorization. The list renders masks
 * only. A Platform Authorization has no AppSecret and no Push Secret, and
 * does not offer regeneration (ADR 0029).
 */
const AppList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);
  const [formOpen, setFormOpen] = useState(false);
  const [authorizeOpen, setAuthorizeOpen] = useState(false);
  const [editing, setEditing] = useState<WechatApp | undefined>();
  const [reveal, setReveal] = useState<WechatAppPushSecret | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<WechatApp>(async (pageSize, pageToken, params) => {
        const set = await services.wechat.ListWechatApps({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "name", value: String(params.name ?? "") || undefined },
            { field: "wx_app_id", value: String(params.wxAppId ?? "") || undefined },
            { field: "app_type", value: String(params.appType ?? "") || undefined },
            { field: "account_kind", value: String(params.accountKind ?? "") || undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.apps ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const fail = (e: unknown) => message.error(asServiceError(e)?.message ?? String(e));

  const withdraw = (record: WechatApp) => {
    services.wechat
      .WithdrawWechatApp({ appId: record.appId })
      .then(() => {
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      })
      .catch(fail);
  };

  const setSuspended = (record: WechatApp, suspended: boolean) => {
    services.wechat
      .SetWechatAppSuspended({ appId: record.appId, suspended })
      .then(() => {
        message.success(
          suspended
            ? intl.formatMessage({ id: "pages.wechat.app.suspended", defaultMessage: "已停用" })
            : intl.formatMessage({ id: "pages.wechat.app.resumed", defaultMessage: "已恢复" }),
        );
        actionRef.current?.reload();
      })
      .catch(fail);
  };

  const regenerate = (record: WechatApp) => {
    services.wechat
      .RegenerateWechatAppPushSecret({ appId: record.appId })
      .then((next) => {
        setReveal(next);
        actionRef.current?.reload();
      })
      .catch(fail);
  };

  const columns: ProColumns<WechatApp>[] = [
    {
      title: "ID",
      dataIndex: "appId",
      width: 180,
      search: false,
      render: (_, r) => <Typography.Text copyable>{String(r.appId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.name", defaultMessage: "显示名称" }),
      dataIndex: "name",
    },
    {
      title: "AppID",
      dataIndex: "wxAppId",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.type", defaultMessage: "类型" }),
      dataIndex: "appType",
      valueEnum: {
        mini_program: { text: intl.formatMessage({ id: "pages.wechat.app.type.mini", defaultMessage: "小程序" }) },
        official_account: { text: intl.formatMessage({ id: "pages.wechat.app.type.official", defaultMessage: "公众号" }) },
      },
      render: (_, r) => TYPE_LABEL[r.appType ?? ""] ?? "",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.kind", defaultMessage: "公众号种类" }),
      dataIndex: "accountKind",
      valueEnum: {
        service_account: { text: intl.formatMessage({ id: "pages.wechat.app.kind.service", defaultMessage: "服务号" }) },
        subscription_account: { text: intl.formatMessage({ id: "pages.wechat.app.kind.subscription", defaultMessage: "订阅号" }) },
      },
      render: (_, r) => KIND_LABEL[r.accountKind ?? ""] ?? "",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.connection", defaultMessage: "接入方式" }),
      dataIndex: "connection",
      search: false,
      render: (_, r) => CONNECTION_LABEL[r.connection ?? ""] ?? "",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.platform", defaultMessage: "微信平台" }),
      dataIndex: "platformId",
      search: false,
      render: (_, r) =>
        platformAuthorization(r) && r.platformId ? <Typography.Text copyable>{String(r.platformId)}</Typography.Text> : "",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.capabilities", defaultMessage: "授予的能力" }),
      dataIndex: "grantedCapabilities",
      search: false,
      render: (_, r) =>
        (r.grantedCapabilities ?? [])
          .filter((cap) => CAPABILITY_LABEL[cap])
          .map((cap) => (
            <Tag key={cap}>{CAPABILITY_LABEL[cap]}</Tag>
          )),
    },
    {
      title: "AppSecret",
      dataIndex: "appSecretMask",
      search: false,
    },
    {
      title: "Token",
      dataIndex: "tokenMask",
      search: false,
    },
    {
      title: "EncodingAESKey",
      dataIndex: "encodingAesKeyMask",
      search: false,
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.app.callback", defaultMessage: "回调地址" }),
      dataIndex: "callbackAddress",
      search: false,
      ellipsis: true,
      render: (_, r) => (r.callbackAddress ? <Typography.Text copyable>{r.callbackAddress}</Typography.Text> : ""),
    },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "suspended",
      search: false,
      width: 90,
      render: (_, r) =>
        r.suspended ? (
          <Tag color="warning">{intl.formatMessage({ id: "pages.wechat.app.status.suspended", defaultMessage: "停用" })}</Tag>
        ) : (
          <Tag color="success">{intl.formatMessage({ id: "pages.wechat.app.status.live", defaultMessage: "正常" })}</Tag>
        ),
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
      width: 280,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("wechat:app:update")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="suspend" accessible={access.perm("wechat:app:update")}>
          <a onClick={() => setSuspended(record, !record.suspended)}>
            {record.suspended
              ? intl.formatMessage({ id: "pages.wechat.app.resume", defaultMessage: "恢复" })
              : intl.formatMessage({ id: "pages.wechat.app.suspend", defaultMessage: "停用" })}
          </a>
        </Access>,
        platformAuthorization(record) ? null : (
          <Access key="regenerate" accessible={access.perm("wechat:app:update")}>
            <Popconfirm
              title={intl.formatMessage({
                id: "pages.wechat.app.regenerate.confirm",
                defaultMessage: "重新生成后旧的消息推送密钥立即作废，应用本身不会撤回或停用。确定重新生成？",
              })}
              onConfirm={() => regenerate(record)}
            >
              <a>{intl.formatMessage({ id: "pages.wechat.app.regenerate", defaultMessage: "重新生成密钥" })}</a>
            </Popconfirm>
          </Access>
        ),
        <Access key="remove" accessible={access.perm("wechat:app:remove")}>
          <Popconfirm
            title={intl.formatMessage({
              id: "pages.wechat.app.withdraw.confirm",
              defaultMessage: "撤回后该应用离开列表，确定撤回？",
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
      <ProTable<WechatApp>
        headerTitle={intl.formatMessage({ id: "menu.wechat.app", defaultMessage: "微信应用" })}
        rowKey="appId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="authorize" accessible={access.perm("wechat:app:create")}>
            <Button
              onClick={() => {
                setAuthorizeOpen(true);
              }}
            >
              {intl.formatMessage({ id: "pages.wechat.app.authorize", defaultMessage: "平台授权" })}
            </Button>
          </Access>,
          <Access key="add" accessible={access.perm("wechat:app:create")}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(undefined);
                setFormOpen(true);
              }}
            >
              {intl.formatMessage({ id: "pages.wechat.app.new", defaultMessage: "直连接入" })}
            </Button>
          </Access>,
        ]}
      />
      <AppFormModal
        open={formOpen}
        record={editing}
        onClose={(changed, next) => {
          setFormOpen(false);
          setEditing(undefined);
          if (changed) actionRef.current?.reload();
          if (next) setReveal(next);
        }}
      />
      <AuthorizeModal open={authorizeOpen} onClose={() => setAuthorizeOpen(false)} />
      <PushSecretModal reveal={reveal} onClose={() => setReveal(undefined)} />
    </PageContainer>
  );
};

export default AppList;
