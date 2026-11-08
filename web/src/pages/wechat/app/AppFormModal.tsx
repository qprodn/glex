import { services, asServiceError } from "@/services";
import type { WechatApp, WechatAppPushSecret } from "@/services/wechat/v1";
import { ModalForm, ProFormDependency, ProFormSelect, ProFormText } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Form } from "antd";
import React, { useEffect } from "react";

const MINI = "WECHAT_APP_TYPE_MINI_PROGRAM";
const OFFICIAL = "WECHAT_APP_TYPE_OFFICIAL_ACCOUNT";
const SERVICE = "WECHAT_ACCOUNT_KIND_SERVICE";
const SUBSCRIPTION = "WECHAT_ACCOUNT_KIND_SUBSCRIPTION";

type AppFormModalProps = {
  open: boolean;
  record?: WechatApp | undefined;
  onClose: (changed: boolean, reveal?: WechatAppPushSecret) => void;
};

/**
 * Connect a WeChat App by Direct Hold, or correct its name and AppSecret.
 * A mini program has no kind. An official account must be a Service
 * Account or a Subscription Account. The AppSecret is a password and is
 * never echoed back (ADR 0029).
 */
const AppFormModal: React.FC<AppFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);
  const platformAuth = record?.connection === "WECHAT_CONNECTION_PLATFORM_AUTHORIZATION";
  const [form] = Form.useForm();

  useEffect(() => {
    if (!open) return;
    if (isEdit) {
      form.setFieldsValue({
        name: record?.name,
        wxAppId: record?.wxAppId,
        appType: record?.appType,
        accountKind: record?.accountKind === "WECHAT_ACCOUNT_KIND_UNSPECIFIED" ? undefined : record?.accountKind,
        returnUrl: record?.returnUrl,
        webAuthScope:
          record?.webAuthScope && record.webAuthScope !== "WECHAT_WEB_AUTH_SCOPE_UNSPECIFIED"
            ? record.webAuthScope
            : "WECHAT_WEB_AUTH_SCOPE_OPENID",
      });
    } else {
      form.resetFields();
    }
  }, [open, record]);

  const keep = intl.formatMessage({
    id: "pages.wechat.app.secret.keep",
    defaultMessage: "留空保持原值",
  });

  return (
    <ModalForm
      title={
        isEdit
          ? intl.formatMessage({ id: "pages.wechat.app.edit", defaultMessage: "编辑微信应用" })
          : intl.formatMessage({ id: "pages.wechat.app.new", defaultMessage: "直连接入" })
      }
      width={640}
      form={form}
      open={open}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        try {
          if (!isEdit) {
            const reveal = await services.wechat.CreateWechatApp({
              name: values.name,
              wxAppId: values.wxAppId,
              appType: values.appType,
              accountKind: values.appType === OFFICIAL ? values.accountKind : undefined,
              appSecret: values.appSecret,
            });
            message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
            onClose(true, reveal);
            return true;
          }
          await services.wechat.UpdateWechatApp({
            appId: record?.appId,
            name: values.name,
            wxAppId: record?.wxAppId,
            appSecret: platformAuth ? "" : (values.appSecret ?? ""),
          });
          if (record?.accountKind === SERVICE) {
            await services.wechat.ConfigureWechatWebAuthorization({
              appId: record.appId,
              returnUrl: values.returnUrl ?? "",
              webAuthScope: values.webAuthScope || "WECHAT_WEB_AUTH_SCOPE_OPENID",
            });
          }
          message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
          onClose(true);
          return true;
        } catch (e) {
          message.error(asServiceError(e)?.message ?? String(e));
          return false;
        }
      }}
    >
      <ProFormText
        name="name"
        label={intl.formatMessage({ id: "pages.wechat.app.name", defaultMessage: "显示名称" })}
        rules={[{ required: true }, { max: 64 }]}
      />
      <ProFormSelect
        name="appType"
        label={intl.formatMessage({ id: "pages.wechat.app.type", defaultMessage: "类型" })}
        rules={[{ required: true }]}
        disabled={isEdit}
        options={[
          { value: MINI, label: intl.formatMessage({ id: "pages.wechat.app.type.mini", defaultMessage: "小程序" }) },
          { value: OFFICIAL, label: intl.formatMessage({ id: "pages.wechat.app.type.official", defaultMessage: "公众号" }) },
        ]}
      />
      <ProFormDependency name={["appType"]}>
        {({ appType }) =>
          appType === OFFICIAL ? (
            <ProFormSelect
              name="accountKind"
              label={intl.formatMessage({ id: "pages.wechat.app.kind", defaultMessage: "公众号种类" })}
              rules={[{ required: true, message: intl.formatMessage({ id: "pages.wechat.app.kind.required", defaultMessage: "直连公众号要标明服务号或订阅号" }) }]}
              disabled={isEdit}
              options={[
                { value: SERVICE, label: intl.formatMessage({ id: "pages.wechat.app.kind.service", defaultMessage: "服务号" }) },
                { value: SUBSCRIPTION, label: intl.formatMessage({ id: "pages.wechat.app.kind.subscription", defaultMessage: "订阅号" }) },
              ]}
            />
          ) : null
        }
      </ProFormDependency>
      <ProFormText
        name="wxAppId"
        label="AppID"
        rules={[{ required: true }, { max: 64 }]}
        disabled={isEdit}
        extra={intl.formatMessage({
          id: "pages.wechat.app.appId.hint",
          defaultMessage: "保存后不可修改。未撤回的应用之间，同一个 AppID 只有一条。",
        })}
      />
      {isEdit && record?.accountKind === SERVICE ? (
        <>
          <ProFormText
            name="returnUrl"
            label={intl.formatMessage({ id: "pages.wechat.app.returnUrl", defaultMessage: "网页授权返回地址" })}
            rules={[{ max: 2048 }]}
            extra={intl.formatMessage({
              id: "pages.wechat.app.returnUrl.hint",
              defaultMessage: "浏览器完成授权后只回到这个地址。请求不能另指一个。留空则网页授权不能开始。",
            })}
          />
          <ProFormSelect
            name="webAuthScope"
            label={intl.formatMessage({ id: "pages.wechat.app.webAuthScope", defaultMessage: "网页授权范围" })}
            rules={[{ required: true }]}
            options={[
              {
                value: "WECHAT_WEB_AUTH_SCOPE_OPENID",
                label: intl.formatMessage({ id: "pages.wechat.app.webAuthScope.openid", defaultMessage: "只取 openid" }),
              },
              {
                value: "WECHAT_WEB_AUTH_SCOPE_PROFILE",
                label: intl.formatMessage({
                  id: "pages.wechat.app.webAuthScope.profile",
                  defaultMessage: "同时要昵称和头像",
                }),
              },
            ]}
          />
        </>
      ) : null}
      {platformAuth ? null : (
        <ProFormText.Password
          name="appSecret"
          label="AppSecret"
          rules={isEdit ? [] : [{ required: true }]}
          placeholder={isEdit ? record?.appSecretMask : undefined}
          extra={
            isEdit
              ? keep
              : intl.formatMessage({
                  id: "pages.wechat.app.secret.hint",
                  defaultMessage: "只保存密文。页面和后续查询不回显明文。",
                })
          }
        />
      )}
    </ModalForm>
  );
};

export default AppFormModal;
