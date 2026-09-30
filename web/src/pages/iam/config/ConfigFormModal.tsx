import type { Config } from "@/services/param/v1";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import { ModalForm, ProFormSwitch, ProFormText, ProFormTextArea } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type ConfigFormModalProps = {
  open: boolean;
  record?: Config | undefined;
  onClose: (changed: boolean) => void;
};

// All business fields are updatable — the key included: a Config key stays
// editable after create (unlike a Dict Type key, ADR 0006); a rename passes
// the same uniqueness pre-check as a create. built_in is the exception: set
// at create and immutable afterwards — a PATCH-and-delete pair must not be
// able to strip the withdrawal protection, so the switch is create-only and
// the mask never carries it.
const EDITABLE_FIELDS = ["configName", "configKey", "configValue", "remark"] as const;

const ConfigFormModal: React.FC<ConfigFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);

  return (
    <ModalForm<Config>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.config.edit", defaultMessage: "编辑参数" }
          : { id: "pages.config.new", defaultMessage: "新建参数" },
      )}
      width={520}
      open={open}
      initialValues={
        isEdit
          ? {
              configName: record?.configName,
              configKey: record?.configKey,
              configValue: record?.configValue,
              builtIn: record?.builtIn,
              remark: record?.remark,
            }
          : { builtIn: false }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        if (!isEdit) {
          await services.config.CreateConfig({ config: values });
          message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
          onClose(true);
          return true;
        }
        const mask = collectUpdateMask(record, values, EDITABLE_FIELDS);
        if (!mask) {
          message.info(intl.formatMessage({ id: "common.noChange", defaultMessage: "没有变更" }));
          onClose(false);
          return true;
        }
        await services.config.UpdateConfig({
          config: { ...record, ...values } as Config,
          updateMask: mask,
        });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormText
        name="configName"
        label={intl.formatMessage({ id: "pages.config.name", defaultMessage: "参数名称" })}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="configKey"
        label={intl.formatMessage({ id: "pages.config.key", defaultMessage: "参数键名" })}
        placeholder="sys.account.registerUser"
        rules={[
          { required: true },
          // Same charset the server enforces.
          {
            pattern: /^[A-Za-z0-9][A-Za-z0-9._-]*$/,
            message: intl.formatMessage({
              id: "pages.config.keyPattern",
              defaultMessage: "字母或数字开头，仅字母、数字、点、下划线、连字符",
            }),
          },
        ]}
        extra={intl.formatMessage({
          id: "pages.config.keyHint",
          defaultMessage: "消费方代码按此键取值；改名会让旧键读到 not-found",
        })}
      />
      <ProFormText
        name="configValue"
        label={intl.formatMessage({ id: "pages.config.value", defaultMessage: "参数键值" })}
        placeholder="true / false / 20 …"
        rules={[{ required: true }]}
        extra={intl.formatMessage({
          id: "pages.config.valueHint",
          defaultMessage: "纯字符串，布尔写 true/false，解析归消费方",
        })}
      />
      {!isEdit && (
        <ProFormSwitch
          name="builtIn"
          label={intl.formatMessage({ id: "pages.config.builtIn", defaultMessage: "内置参数" })}
          extra={intl.formatMessage({
            id: "pages.config.builtInHint",
            defaultMessage: "内置参数不可删除；标记创建后不可修改",
          })}
        />
      )}
      <ProFormTextArea
        name="remark"
        label={intl.formatMessage({ id: "common.remark", defaultMessage: "备注" })}
      />
    </ModalForm>
  );
};

export default ConfigFormModal;
