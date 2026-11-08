import { services, asServiceError } from "@/services";
import type { WechatAuthorization } from "@/services/wechat/v1";
import { ModalForm, ProFormSelect } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { Alert, App, Modal, Typography } from "antd";
import React, { useState } from "react";

type AuthorizeModalProps = {
  open: boolean;
  onClose: () => void;
};

/**
 * Start a Platform Authorization by choosing one ready WeChat Platform.
 * The link is what the other administrator opens. This screen does not
 * ask for an AppID or an AppSecret; the app is created from WeChat's grant.
 */
const AuthorizeModal: React.FC<AuthorizeModalProps> = ({ open, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const [link, setLink] = useState<WechatAuthorization | undefined>();

  return (
    <>
      <ModalForm
        title={intl.formatMessage({ id: "pages.wechat.app.authorize", defaultMessage: "平台授权" })}
        width={640}
        open={open}
        modalProps={{ destroyOnHidden: true, onCancel: onClose }}
        onFinish={async (values) => {
          try {
            const next = await services.wechat.StartWechatAuthorization({
              platformId: values.platformId,
            });
            setLink(next);
            onClose();
            return true;
          } catch (e) {
            message.error(asServiceError(e)?.message ?? String(e));
            return false;
          }
        }}
      >
        <ProFormSelect
          name="platformId"
          label={intl.formatMessage({ id: "pages.wechat.app.authorize.platform", defaultMessage: "微信平台" })}
          rules={[
            {
              required: true,
              message: intl.formatMessage({
                id: "pages.wechat.app.authorize.platform.required",
                defaultMessage: "请选择一条就绪的微信平台",
              }),
            },
          ]}
          request={async () => {
            const set = await services.wechat.ListAuthorizationPlatforms({});
            return (set.platforms ?? [])
              .filter((p) => p.platformId)
              .map((p) => ({
                value: p.platformId,
                label: `${p.name ?? ""}（${p.componentAppId ?? ""}）`,
              }));
          }}
          extra={intl.formatMessage({
            id: "pages.wechat.app.authorize.hint",
            defaultMessage:
              "只列出已经就绪、尚未撤回的微信平台。对方管理员打开链接完成授权。名称、类型和授予的能力以微信返回的为准，不保存对方的 AppSecret。",
          })}
        />
      </ModalForm>
      <Modal
        title={intl.formatMessage({ id: "pages.wechat.app.authorize.link", defaultMessage: "授权链接" })}
        open={Boolean(link)}
        onOk={() => setLink(undefined)}
        onCancel={() => setLink(undefined)}
        cancelButtonProps={{ style: { display: "none" } }}
        okText={intl.formatMessage({ id: "pages.wechat.app.authorize.ack", defaultMessage: "知道了" })}
        destroyOnHidden
        width={640}
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message={intl.formatMessage({
            id: "pages.wechat.app.authorize.link.hint",
            defaultMessage:
              "把这个链接交给对方管理员。授权完成后，同一条应用会出现在列表里，并固定挂在所选平台上。再次授权恢复的仍是这一条。",
          })}
        />
        <Typography.Paragraph copyable={{ text: link?.url }}>{link?.url}</Typography.Paragraph>
      </Modal>
    </>
  );
};

export default AuthorizeModal;
