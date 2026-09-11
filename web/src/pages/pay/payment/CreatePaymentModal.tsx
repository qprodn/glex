import { services, asServiceError } from "@/services";
import type { MerchantAccount } from "@/services/pay/v1";
import { yuanToFen, PAYMENT_STATUS } from "@/utils/money";
import { ModalForm, ProFormDigit, ProFormSelect, ProFormText } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Button, Modal, QRCode, Typography } from "antd";
import React, { useEffect, useRef, useState } from "react";

/**
 * 发起支付 (ticket 09): picks an enabled merchant, states the business
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
  const [merchants, setMerchants] = useState<MerchantAccount[]>([]);
  const [created, setCreated] = useState<{ paymentId: number; codeUrl: string } | undefined>();
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);

  useEffect(() => {
    if (!open) return;
    services.pay
      .ListMerchantAccounts({ pageSize: 100, pageToken: undefined, filter: "status=0", orderBy: undefined })
      .then((set) => setMerchants(set.merchants ?? []))
      .catch(async (e) => { message.error(asServiceError(e)?.message ?? String(e)); });
    return () => stopPoll();
  }, [open]);

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
      merchantId: number;
      bizNo: string;
      subject: string;
      amountYuan: string;
      expireMinutes?: number;
    }>
      title={intl.formatMessage({ id: "pages.pay.payment.create", defaultMessage: "发起支付" })}
      width={480}
      open={open && !created}
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        try {
          const p = await services.pay.CreatePayment({
            merchantId: Number(values.merchantId),
            bizNo: values.bizNo,
            subject: values.subject,
            amount: yuanToFen(values.amountYuan),
            payMethod: 1,
            expireMinutes: values.expireMinutes || undefined,
          });
          message.success(intl.formatMessage({ id: "pages.pay.payment.created", defaultMessage: "下单成功，请扫码支付" }));
          const paymentId = Number(p.paymentId);
          setCreated({ paymentId, codeUrl: p.channelPayload ?? "" });
          pollUntilSettled(paymentId);
          return true;
        } catch (e) {
          message.error(asServiceError(e)?.message ?? String(e));
          return false;
        }
      }}
    >
      <ProFormSelect
        name="merchantId"
        label={intl.formatMessage({ id: "pages.pay.merchant.name", defaultMessage: "收款商户" })}
        rules={[{ required: true }]}
        options={merchants.map((m) => ({ value: Number(m.merchantId), label: m.name }))}
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
