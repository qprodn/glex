import { services, asServiceError } from "@/services";
import type { MerchantAccount, PayApplication } from "@/services/pay/v1";
import { ModalForm, ProFormSelect, ProFormText } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Form } from "antd";
import React, { useEffect, useState } from "react";

async function listEnabledMerchants(): Promise<MerchantAccount[]> {
  const all: MerchantAccount[] = [];
  let pageToken: string | undefined;
  const seen = new Set<string>();
  for (;;) {
    const set = await services.pay.ListMerchantAccounts({
      pageSize: 100,
      pageToken,
      filter: "status=0",
      orderBy: undefined,
    });
    all.push(...(set.merchants ?? []));
    const next = set.nextPageToken || undefined;
    if (!next || seen.has(next)) {
      return all;
    }
    seen.add(next);
    pageToken = next;
  }
}

type ApplicationFormModalProps = {
  open: boolean;
  record?: PayApplication | undefined;
  onClose: (changed: boolean) => void;
};

/**
 * Pay Application form. Create binds one enabled Merchant Account. Edit
 * changes the display name and either result address; the binding stays.
 */
const ApplicationFormModal: React.FC<ApplicationFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);
  const [form] = Form.useForm();
  const [merchants, setMerchants] = useState<MerchantAccount[]>([]);

  useEffect(() => {
    if (!open) return;
    listEnabledMerchants()
      .then(setMerchants)
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  }, [open]);

  useEffect(() => {
    if (!open) return;
    if (isEdit) {
      form.setFieldsValue({
        name: record?.name,
        merchantId: record?.merchantId,
        orderNotifyUrl: record?.orderNotifyUrl ?? "",
        refundNotifyUrl: record?.refundNotifyUrl ?? "",
      });
    } else {
      form.resetFields();
    }
  }, [open, record]);

  const merchantOptions = merchants.map((m) => ({
    value: m.merchantId,
    label: `${m.name ?? ""} (${m.merchantId})`,
  }));
  if (isEdit && record?.merchantId != null && !merchantOptions.some((o) => o.value === record.merchantId)) {
    merchantOptions.unshift({
      value: record.merchantId,
      label: String(record.merchantId),
    });
  }

  return (
    <ModalForm
      title={intl.formatMessage({
        id: isEdit ? "pages.pay.application.edit" : "pages.pay.application.new",
        defaultMessage: isEdit ? "编辑支付应用" : "新建支付应用",
      })}
      open={open}
      form={form}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        const orderNotifyUrl = values.orderNotifyUrl ?? "";
        const refundNotifyUrl = values.refundNotifyUrl ?? "";
        try {
          if (!isEdit) {
            await services.pay.CreatePayApplication({
              name: values.name,
              merchantId: values.merchantId,
              orderNotifyUrl,
              refundNotifyUrl,
            });
          } else {
            await services.pay.UpdatePayApplication({
              applicationId: record?.applicationId,
              name: values.name,
              orderNotifyUrl,
              refundNotifyUrl,
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
        label={intl.formatMessage({ id: "pages.pay.application.name", defaultMessage: "显示名称" })}
        rules={[{ required: true }, { max: 64 }]}
        extra={intl.formatMessage({
          id: "pages.pay.application.name.hint",
          defaultMessage: "未撤除的支付应用之间唯一",
        })}
      />
      <ProFormSelect
        name="merchantId"
        label={intl.formatMessage({ id: "pages.pay.application.merchant", defaultMessage: "商户账号" })}
        rules={[{ required: true }]}
        disabled={isEdit}
        options={merchantOptions}
        extra={intl.formatMessage({
          id: "pages.pay.application.merchant.hint",
          defaultMessage: "创建后不可改绑",
        })}
      />
      <ProFormText
        name="orderNotifyUrl"
        label={intl.formatMessage({
          id: "pages.pay.application.orderNotifyUrl",
          defaultMessage: "支付结果地址",
        })}
        rules={[
          { max: 512 },
          {
            type: "url",
            message: intl.formatMessage({
              id: "pages.pay.application.notifyUrl.invalid",
              defaultMessage: "填写带主机名的 http 或 https 地址",
            }),
          },
        ]}
      />
      <ProFormText
        name="refundNotifyUrl"
        label={intl.formatMessage({
          id: "pages.pay.application.refundNotifyUrl",
          defaultMessage: "退款结果地址",
        })}
        rules={[
          { max: 512 },
          {
            type: "url",
            message: intl.formatMessage({
              id: "pages.pay.application.notifyUrl.invalid",
              defaultMessage: "填写带主机名的 http 或 https 地址",
            }),
          },
        ]}
      />
    </ModalForm>
  );
};

export default ApplicationFormModal;
