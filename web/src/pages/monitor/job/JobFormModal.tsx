import type { Job } from "@/services/job/v1";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDigit,
  ProFormSelect,
  ProFormSwitch,
  ProFormText,
  ProFormTextArea,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type JobFormModalProps = {
  open: boolean;
  record?: Job | undefined;
  onClose: (changed: boolean) => void;
};

// enabled moves through the table's switch (SetJobEnabled owns it, ADR
// 0023); the form carries it only at create.
const EDITABLE_FIELDS = ["name", "handler", "cron", "args", "timeoutSeconds", "remark"] as const;

// Crude arity check only — the server parses for real (robfig) and answers
// JOB_CRON_INVALID; @descriptors pass through.
const cronArity = {
  validator(_: unknown, value: string) {
    if (!value || value.startsWith("@") || value.trim().split(/\s+/).length === 5) {
      return Promise.resolve();
    }
    return Promise.reject(new Error("expect 5 fields, e.g. */5 * * * *"));
  },
};

const JobFormModal: React.FC<JobFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);

  return (
    <ModalForm<Job>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.job.edit", defaultMessage: "编辑任务" }
          : { id: "pages.job.new", defaultMessage: "新建任务" },
      )}
      width={560}
      open={open}
      initialValues={
        isEdit
          ? {
              name: record?.name,
              handler: record?.handler,
              cron: record?.cron,
              args: record?.args,
              timeoutSeconds: record?.timeoutSeconds,
              remark: record?.remark,
            }
          // timeout deliberately has no prefill: leaving it empty lets the
          // server stamp the deployment's configured default instead of
          // hardcoding 60 in a second place.
          : { enabled: true }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        if (!isEdit) {
          await services.job.CreateJob({ job: values });
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
        await services.job.UpdateJob({
          job: { ...record, ...values } as Job,
          updateMask: mask,
        });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormText
        name="name"
        label={intl.formatMessage({ id: "pages.job.name", defaultMessage: "任务名称" })}
        rules={[{ required: true }, { max: 64 }]}
      />
      <ProFormSelect
        name="handler"
        label={intl.formatMessage({ id: "pages.job.handler", defaultMessage: "执行器" })}
        rules={[{ required: true }]}
        extra={intl.formatMessage({
          id: "pages.job.handlerHint",
          defaultMessage: "来自代码注册表，保存时校验（ADR 0023）",
        })}
        request={async () => {
          const set = await services.job.ListJobExecutors({});
          return (set.executors ?? []).map((e) => ({ label: e.name, value: e.name }));
        }}
      />
      <ProFormText
        name="cron"
        label={intl.formatMessage({ id: "pages.job.cron", defaultMessage: "Cron 表达式" })}
        placeholder="*/5 * * * *"
        rules={[{ required: true }, cronArity]}
        extra={intl.formatMessage({
          id: "pages.job.cronHint",
          defaultMessage: "5 字段（分 时 日 月 周），按部署配置的统一时区求值",
        })}
      />
      <ProFormTextArea
        name="args"
        label={intl.formatMessage({ id: "pages.job.args", defaultMessage: "参数" })}
        fieldProps={{ maxLength: 4096 }}
        extra={intl.formatMessage({
          id: "pages.job.argsHint",
          defaultMessage: "原样透传给执行器",
        })}
      />
      <ProFormDigit
        name="timeoutSeconds"
        label={intl.formatMessage({ id: "pages.job.timeout", defaultMessage: "超时（秒）" })}
        min={1}
        max={3600}
        fieldProps={{ step: 1, precision: 0 }}
        rules={[{ required: true }]}
      />
      {!isEdit && (
        <ProFormSwitch
          name="enabled"
          label={intl.formatMessage({ id: "pages.job.enabled", defaultMessage: "启用" })}
          extra={intl.formatMessage({
            id: "pages.job.enabledHint",
            defaultMessage: "关闭则仅暂停调度，手动执行不受限",
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

export default JobFormModal;
