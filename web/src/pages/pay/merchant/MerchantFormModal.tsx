import { services, asServiceError } from "@/services";
import type { ChannelSchema, MerchantAccount, SchemaField } from "@/services/pay/v1";
import {
  ProFormSelect,
  ProFormText,
  ProFormTextArea,
  ModalForm,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Form } from "antd";
import React, { useEffect, useState } from "react";

type MerchantFormModalProps = {
  open: boolean;
  record?: MerchantAccount | undefined;
  onClose: (changed: boolean) => void;
};

/**
 * 商户动态表单 (ticket 08, ADR 0021): the channel's config schema is the
 * single source — the form renders from it and submits a config map.
 * Secret fields show the stored masked echo as their placeholder; a blank
 * secret submit means "keep the stored value" (server semantics).
 */
const MerchantFormModal: React.FC<MerchantFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);
  const [form] = Form.useForm();
  const [schemas, setSchemas] = useState<ChannelSchema[]>([]);
  const [channel, setChannel] = useState<number | undefined>(isEdit ? record?.channel : undefined);

  useEffect(() => {
    if (!open) return;
    services.pay
      .GetChannelSchemas({})
      .then((res) => setSchemas(res.schemas ?? []))
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  }, [open]);

  useEffect(() => {
    if (!open) return;
    if (isEdit) {
      setChannel(record?.channel);
      form.setFieldsValue({
        channel: record?.channel,
        name: record?.name,
        remark: record?.remark,
      });
    } else {
      setChannel(undefined);
      form.resetFields();
    }
  }, [open, record]);

  const schema = schemas.find((s) => s.channel === channel);
  const masked = record?.configMasked ?? {};

  const renderField = (field: SchemaField) => {
    const fieldName = field.name ?? "";
    const label = field.label || fieldName;
    const rules = field.required ? [{ required: true }] : [];
    const placeholder =
      // Secret fields: the masked echo doubles as the placeholder — what
      // is set is visible, what it is never is (ADR 0018).
      field.secret && isEdit && masked[fieldName]
        ? masked[fieldName]
        : field.placeholder || undefined;
    const extra = (
      <>
        {field.secret && isEdit
          ? intl.formatMessage({ id: "pages.pay.merchant.secret.keep", defaultMessage: "留空保持原值；" })
          : ""}
        {field.help}
      </>
    );
    const name = ["config", fieldName];
    const initialValue = isEdit ? undefined : field.defaultValue || undefined;
    if (field.type === "textarea") {
      return (
        <ProFormTextArea
          key={fieldName}
          name={name}
          label={label}
          placeholder={placeholder}
          rules={rules}
          extra={extra}
          initialValue={initialValue}
          fieldProps={{ rows: 3, autoSize: { minRows: 3, maxRows: 8 } }}
        />
      );
    }
    if (field.type === "select") {
      return (
        <ProFormSelect
          key={fieldName}
          name={name}
          label={label}
          rules={rules}
          extra={extra}
          initialValue={isEdit ? undefined : field.defaultValue || undefined}
          options={(field.choices ?? []).map((c) => ({ value: c.value, label: c.label }))}
        />
      );
    }
    if (field.type === "password") {
      return (
        <ProFormText.Password
          key={fieldName}
          name={name}
          label={label}
          placeholder={placeholder}
          rules={rules}
          extra={extra}
          initialValue={initialValue}
        />
      );
    }
    return (
      <ProFormText
        key={fieldName}
        name={name}
        label={label}
        placeholder={placeholder}
        rules={rules}
        extra={extra}
        initialValue={initialValue}
      />
    );
  };

  return (
    <ModalForm
      title={
        isEdit
          ? intl.formatMessage({ id: "pages.pay.merchant.edit", defaultMessage: "编辑商户" })
          : intl.formatMessage({ id: "pages.pay.merchant.new", defaultMessage: "新建商户" })
      }
      width={640}
      form={form}
      open={open}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        const config: Record<string, string> = {};
        const raw = (values.config ?? {}) as Record<string, unknown>;
        Object.entries(raw).forEach(([k, v]) => {
          config[k] = v === undefined || v === null ? "" : String(v);
        });
        try {
          if (!isEdit) {
            await services.pay.CreateMerchantAccount({
              channel: values.channel,
              name: values.name,
              config,
              remark: values.remark ?? "",
            });
          } else {
            await services.pay.UpdateMerchantAccount({
              merchantId: record?.merchantId,
              name: values.name,
              config,
              remark: values.remark ?? "",
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
      <ProFormSelect
        name="channel"
        label={intl.formatMessage({ id: "pages.pay.channel", defaultMessage: "渠道" })}
        rules={[{ required: true }]}
        disabled={isEdit}
        options={(schemas ?? []).map((s) => ({ value: s.channel, label: s.label }))}
        fieldProps={{ onChange: (v) => setChannel(v as number) }}
        extra={intl.formatMessage({ id: "pages.pay.merchant.channel.hint", defaultMessage: "保存后不可更换渠道" })}
      />
      <ProFormText
        name="name"
        label={intl.formatMessage({ id: "pages.pay.merchant.name", defaultMessage: "显示名称" })}
        rules={[{ required: true }, { max: 64 }]}
        extra={intl.formatMessage({ id: "pages.pay.merchant.name.hint", defaultMessage: "同渠道内唯一" })}
      />
      {renderGrouped(schema, renderField)}
      <ProFormTextArea
        name="remark"
        label={intl.formatMessage({ id: "common.remark", defaultMessage: "备注" })}
        fieldProps={{ rows: 2 }}
      />
    </ModalForm>
  );
};

// renderGrouped keeps the schema's field order, inserting the group label
// as a divider when it changes.
function renderGrouped(
  schema: ChannelSchema | undefined,
  renderField: (field: SchemaField) => React.ReactNode,
): React.ReactNode[] {
  if (!schema) return [];
  let lastGroup = "";
  return (schema.fields ?? []).map((field) => {
    const nodes: React.ReactNode[] = [];
    if (field.group && field.group !== lastGroup) {
      nodes.push(
        <div
          key={`group-${field.group}`}
          style={{ fontWeight: 600, margin: "8px 0 4px", color: "rgba(0,0,0,0.65)" }}
        >
          {field.group}
        </div>,
      );
    }
    lastGroup = field.group ?? lastGroup;
    nodes.push(renderField(field));
    return nodes;
  });
}

export default MerchantFormModal;
