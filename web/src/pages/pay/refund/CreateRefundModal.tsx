import { services, asServiceError } from "@/services";
import { fenToYuan, yuanToFen } from "@/utils/money";
import { ModalForm, ProFormText, ProFormTextArea } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

/**
 * 发起退款 (ticket 09): states the payment number, the amount (in yuan,
 * converted to minor units here), and a reason. The server enforces the
 * cap and the payment's PAID state; a synchronous channel (Alipay 当面付)
 * answers settled immediately, WeChat reports 退款中 until the callback.
 */
const CreateRefundModal: React.FC<{
  open: boolean;
  onClose: (created: boolean) => void;
}> = ({ open, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();

  return (
    <ModalForm<{
      paymentNo: string;
      amountYuan: string;
      reason?: string;
    }>
      title={intl.formatMessage({ id: "pages.pay.refund.create", defaultMessage: "发起退款" })}
      width={480}
      open={open}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        const paymentId = Number(values.paymentNo?.trim());
        if (!paymentId || Number.isNaN(paymentId)) {
          message.error(intl.formatMessage({ id: "pages.pay.refund.badNo", defaultMessage: "请输入有效的支付单号" }));
          return false;
        }
        try {
          const rf = await services.pay.CreateRefund({
            paymentId,
            amount: yuanToFen(values.amountYuan),
            reason: values.reason ?? "",
          });
          const label = { 1: "退款中", 2: "退款成功", 3: "退款失败" }[rf.status ?? 0] ?? String(rf.status);
          message.success(intl.formatMessage({ id: "pages.pay.refund.created", defaultMessage: "退款已提交：{status}（¥{amount}）" }, { status: label, amount: fenToYuan(rf.amount) }));
          onClose(true);
          return true;
        } catch (e) {
          message.error(asServiceError(e)?.message ?? String(e));
          return false;
        }
      }}
    >
      <ProFormText
        name="paymentNo"
        label={intl.formatMessage({ id: "pages.pay.payment.no", defaultMessage: "支付单号" })}
        rules={[{ required: true }]}
        placeholder="20260911000001"
      />
      <ProFormText
        name="amountYuan"
        label={intl.formatMessage({ id: "pages.pay.payment.amountYuan", defaultMessage: "退款金额（元）" })}
        rules={[{ required: true }, { pattern: /^\d+(\.\d{1,2})?$/, message: intl.formatMessage({ id: "pages.pay.payment.amount.hint", defaultMessage: "最多两位小数" }) }]}
        placeholder="0.40"
        extra={intl.formatMessage({ id: "pages.pay.refund.cap", defaultMessage: "累计退款不能超过已付金额" })}
      />
      <ProFormTextArea
        name="reason"
        label={intl.formatMessage({ id: "pages.pay.refund.reason", defaultMessage: "退款原因" })}
        fieldProps={{ rows: 2 }}
      />
    </ModalForm>
  );
};

export default CreateRefundModal;
