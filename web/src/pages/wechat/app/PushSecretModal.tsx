import type { WechatAppPushSecret } from "@/services/wechat/v1";
import { useIntl } from "@umijs/max";
import { Alert, Modal, Typography } from "antd";
import React from "react";

type PushSecretModalProps = {
  reveal?: WechatAppPushSecret | undefined;
  onClose: () => void;
};

/**
 * The one screen that shows a generated Push Secret in full. Closing it
 * drops the plaintext; later reads show masks (ADR 0029).
 */
const PushSecretModal: React.FC<PushSecretModalProps> = ({ reveal, onClose }) => {
  const intl = useIntl();
  const app = reveal?.app;
  return (
    <Modal
      title={intl.formatMessage({ id: "pages.wechat.app.pushSecret.title", defaultMessage: "消息推送密钥" })}
      open={Boolean(reveal)}
      onOk={onClose}
      onCancel={onClose}
      cancelButtonProps={{ style: { display: "none" } }}
      okText={intl.formatMessage({ id: "pages.wechat.app.pushSecret.ack", defaultMessage: "我已抄写" })}
      destroyOnHidden
      width={640}
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 16 }}
        message={intl.formatMessage({
          id: "pages.wechat.app.pushSecret.once",
          defaultMessage: "Token 和 EncodingAESKey 只这一次给出全文。请填进该应用的微信后台并选择安全模式。关闭后只能看到掩码；要再看全文，只能重新生成，旧密钥会立即作废。",
        })}
      />
      <Typography.Paragraph copyable={{ text: reveal?.token }}>
        Token：{reveal?.token}
      </Typography.Paragraph>
      <Typography.Paragraph copyable={{ text: reveal?.encodingAesKey }}>
        EncodingAESKey：{reveal?.encodingAesKey}
      </Typography.Paragraph>
      <Typography.Paragraph copyable={{ text: app?.callbackAddress }}>
        {intl.formatMessage({ id: "pages.wechat.app.callback", defaultMessage: "回调地址" })}：{app?.callbackAddress}
      </Typography.Paragraph>
    </Modal>
  );
};

export default PushSecretModal;
