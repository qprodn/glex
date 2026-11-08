import { services, asServiceError } from "@/services";
import type { WechatPlatform } from "@/services/wechat/v1";
import { ModalForm, ProFormText } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Form } from "antd";
import React, { useEffect } from "react";

type PlatformFormModalProps = {
  open: boolean;
  record?: WechatPlatform | undefined;
  onClose: (changed: boolean) => void;
};

/**
 * Register or correct a WeChat Platform. Secret fields are passwords.
 * On edit they stay blank: a blank secret keeps the stored value, and the
 * placeholder shows the mask already on the row (ADR 0029).
 */
const PlatformFormModal: React.FC<PlatformFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);
  const [form] = Form.useForm();

  useEffect(() => {
    if (!open) return;
    if (isEdit) {
      form.setFieldsValue({
        name: record?.name,
        componentAppId: record?.componentAppId,
      });
    } else {
      form.resetFields();
    }
  }, [open, record]);

  const keep = intl.formatMessage({
    id: "pages.wechat.platform.secret.keep",
    defaultMessage: "留空保持原值",
  });

  return (
    <ModalForm
      title={
        isEdit
          ? intl.formatMessage({ id: "pages.wechat.platform.edit", defaultMessage: "编辑微信平台" })
          : intl.formatMessage({ id: "pages.wechat.platform.new", defaultMessage: "登记平台" })
      }
      width={640}
      form={form}
      open={open}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        try {
          if (!isEdit) {
            await services.wechat.CreateWechatPlatform({
              name: values.name,
              componentAppId: values.componentAppId,
              componentSecret: values.componentSecret,
              token: values.token,
              encodingAesKey: values.encodingAesKey,
            });
          } else {
            await services.wechat.UpdateWechatPlatform({
              platformId: record?.platformId,
              name: values.name,
              componentAppId: record?.componentAppId,
              componentSecret: values.componentSecret ?? "",
              token: values.token ?? "",
              encodingAesKey: values.encodingAesKey ?? "",
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
        label={intl.formatMessage({ id: "pages.wechat.platform.name", defaultMessage: "显示名称" })}
        rules={[{ required: true }, { max: 64 }]}
      />
      <ProFormText
        name="componentAppId"
        label={intl.formatMessage({ id: "pages.wechat.platform.appId", defaultMessage: "component AppID" })}
        rules={[{ required: true }, { max: 64 }]}
        disabled={isEdit}
        extra={intl.formatMessage({
          id: "pages.wechat.platform.appId.hint",
          defaultMessage: "保存后不可修改",
        })}
      />
      <ProFormText.Password
        name="componentSecret"
        label="component secret"
        rules={isEdit ? [] : [{ required: true }]}
        placeholder={isEdit ? record?.componentSecretMask : undefined}
        extra={isEdit ? keep : undefined}
      />
      <ProFormText.Password
        name="token"
        label="Token"
        rules={isEdit ? [] : [{ required: true }]}
        placeholder={isEdit ? record?.tokenMask : undefined}
        extra={isEdit ? keep : undefined}
      />
      <ProFormText.Password
        name="encodingAesKey"
        label="EncodingAESKey"
        rules={isEdit ? [] : [{ required: true }]}
        placeholder={isEdit ? record?.encodingAesKeyMask : undefined}
        extra={isEdit ? keep : undefined}
      />
    </ModalForm>
  );
};

export default PlatformFormModal;
