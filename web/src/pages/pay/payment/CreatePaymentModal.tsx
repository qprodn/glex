import { services, asServiceError } from "@/services";
import type { PayApplication } from "@/services/pay/v1";
import { yuanToFen, PAYMENT_STATUS } from "@/utils/money";
import { ModalForm, ProFormDigit, ProFormSelect, ProFormText } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Button, Modal, QRCode, Typography } from "antd";
import React, { useEffect, useRef, useState } from "react";

/**
 * 发起支付: picks an enabled Pay Application, states the business
 * order number / subject / amount, then renders the channel's QR payload
 * and polls the payment until it leaves PENDING — via callback or via the
 * poll's own server-side settlement. The typed yuan converts to minor
 * units here at the edge (ADR 0019).
 */
const CreatePaymentModal: React.FC<{
  open: boolean;
  onClose: (created: boolean, codeUrl?: string, paymentId?: number) => void;
  onPaid?: () => void;
}> = ({ open, onClose, onPaid }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const [applications, setApplications] = useState<PayApplication[]>([]);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);

  useEffect(() => {
    if (!open) return;
    services.pay
      .ListPayApplications({ pageSize: 100, pageToken: undefined, filter: "status=0", orderBy: undefined })
      .then((set) => setApplications(set.applications ?? []))
      .catch(async (e) => { message.error(asServiceError(e)?.message ?? String(e)); });
  }, [open]);

  // The poll outlives this modal: after a successful order the parent
  // closes it and swaps to the QR dialog, while the poll keeps running
  // until the payment settles — only unmount retires it.
  useEffect(() => {
    return () => stopPoll();
  }, []);

  const stopPoll = () => {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = undefined;
  };

  const pollUntilSettled = (paymentId: number) => {
    let ticks = 0;
    stopPoll();
    pollRef.current = setInterval(async () => {
      ticks += 1;
      if (ticks > 90) {
        stopPoll();
        return;
      }
      try {
        const p = await services.pay.QueryPayment({ paymentId });
        if ((p.status ?? 0) !== PAYMENT_STATUS_PENDING) {
          stopPoll();
          const label = PAYMENT_STATUS[p.status ?? 0]?.label ?? String(p.status);
          message.info(intl.formatMessage({ id: "pages.pay.payment.settled", defaultMessage: "支付单状态：{status}" }, { status: label }));
          onPaid?.();
        }
      } catch {
        // transient channel errors are tolerated; the next tick retries
      }
    }, 2000);
  };

  return (
    <ModalForm<{
      applicationId: number;
      bizNo: string;
      subject: string;
      amountYuan: string;
      expireMinutes?: number;
    }>
      title={intl.formatMessage({ id: "pages.pay.payment.create", defaultMessage: "发起支付" })}
      width={480}
      open={open}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        try {
          const p = await services.pay.CreatePayment({
            applicationId: Number(values.applicationId),
            bizNo: values.bizNo,
            subject: values.subject,
            amount: yuanToFen(values.amountYuan),
            payMethod: 1,
            expireMinutes: values.expireMinutes || undefined,
          });
          message.success(intl.formatMessage({ id: "pages.pay.payment.created", defaultMessage: "下单成功，请扫码支付" }));
          const paymentId = Number(p.paymentId);
          pollUntilSettled(paymentId);
          // Hand the QR payload to the parent — it opens the QR dialog and
          // refreshes the list; the poll keeps running until settlement.
          onClose(true, p.channelPayload ?? "", paymentId);
          return true;
        } catch (e) {
          message.error(asServiceError(e)?.message ?? String(e));
          return false;
        }
      }}
    >
      <ProFormSelect
        name="applicationId"
        label={intl.formatMessage({ id: "pages.pay.payment.application", defaultMessage: "支付应用" })}
        rules={[{ required: true }]}
        options={applications.map((a) => ({ value: Number(a.applicationId), label: a.name }))}
      />
      <ProFormText
        name="bizNo"
        label={intl.formatMessage({ id: "pages.pay.payment.bizNo", defaultMessage: "业务单号" })}
        rules={[{ required: true }, { max: 64 }]}
        placeholder="ORDER-20260911-001"
      />
      <ProFormText
        name="subject"
        label={intl.formatMessage({ id: "pages.pay.payment.subject", defaultMessage: "主题" })}
        rules={[{ required: true }, { max: 256 }]}
      />
      <ProFormText
        name="amountYuan"
        label={intl.formatMessage({ id: "pages.pay.payment.amountYuan", defaultMessage: "金额（元）" })}
        rules={[{ required: true }, { pattern: /^\d+(\.\d{1,2})?$/, message: intl.formatMessage({ id: "pages.pay.payment.amount.hint", defaultMessage: "最多两位小数" }) }]}
        placeholder="1.00"
      />
      <ProFormDigit
        name="expireMinutes"
        label={intl.formatMessage({ id: "pages.pay.payment.expireMinutes", defaultMessage: "有效期（分钟）" })}
        fieldProps={{ precision: 0, min: 1 }}
        placeholder={intl.formatMessage({ id: "pages.pay.payment.expireMinutes.hint", defaultMessage: "留空默认 30 分钟" })}
      />
    </ModalForm>
  );
};

const PAYMENT_STATUS_PENDING = 1;

/** QrDialog renders the channel payload as a QR and polls for settlement. */
export const QrDialog: React.FC<{
  open: boolean;
  codeUrl: string;
  paymentId?: number;
  onClose: (changed: boolean) => void;
}> = ({ open, codeUrl, paymentId, onClose }) => {
  const intl = useIntl();
  return (
    <Modal
      open={open}
      footer={null}
      onCancel={() => onClose(false)}
      title={intl.formatMessage({ id: "pages.pay.payment.qr", defaultMessage: "扫码支付" })}
    >
      <div style={{ textAlign: "center", padding: "8px 0" }}>
        {codeUrl ? <QRCode value={codeUrl} size={224} /> : null}
        <div style={{ marginTop: 12 }}>
          <Typography.Text type="secondary">
            {intl.formatMessage({ id: "pages.pay.payment.qr.hint", defaultMessage: "支付完成后状态自动更新" })}
            {paymentId ? `（${paymentId}）` : ""}
          </Typography.Text>
        </div>
        <Button style={{ marginTop: 12 }} onClick={() => onClose(false)}>
          {intl.formatMessage({ id: "common.cancel", defaultMessage: "取消" })}
        </Button>
      </div>
    </Modal>
  );
};

export default CreatePaymentModal;
