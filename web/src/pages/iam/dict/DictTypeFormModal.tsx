import type { DictType } from "@/services/dict/v1";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import { ModalForm, ProFormText, ProFormTextArea } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type DictTypeFormModalProps = {
  open: boolean;
  record?: DictType | undefined;
  onClose: (changed: boolean) => void;
};

// The key (dict_type) is deliberately absent: immutable after create
// (ADR 0006) — the server ignores it on update no matter the mask.
const EDITABLE_FIELDS = ["dictName", "remark"] as const;

const DictTypeFormModal: React.FC<DictTypeFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);

  return (
    <ModalForm<DictType>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.dict.type.edit", defaultMessage: "编辑字典" }
          : { id: "pages.dict.type.new", defaultMessage: "新建字典" },
      )}
      width={520}
      open={open}
      initialValues={
        isEdit
          ? {
              dictName: record?.dictName,
              dictType: record?.dictType,
              remark: record?.remark,
            }
          : {}
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        if (!isEdit) {
          await services.dict.CreateDictType({ dictType: values });
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
        await services.dict.UpdateDictType({
          dictType: { ...record, ...values } as DictType,
          updateMask: mask,
        });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormText
        name="dictName"
        label={intl.formatMessage({ id: "pages.dict.type.dictName", defaultMessage: "字典名称" })}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="dictType"
        label={intl.formatMessage({ id: "pages.dict.type.dictType", defaultMessage: "字典类型" })}
        placeholder="sys_normal_disable"
        disabled={isEdit}
        rules={[
          { required: true },
          // Same charset the server enforces (RuoYi DICTIONARY_TYPE).
          { pattern: /^[a-z][a-z0-9_]*$/, message: "小写字母开头，仅小写字母、数字、下划线" },
        ]}
        extra={
          isEdit
            ? intl.formatMessage({
                id: "pages.dict.type.keyImmutable",
                defaultMessage: "类型键创建后不可修改（ADR 0006）",
              })
            : intl.formatMessage({
                id: "pages.dict.type.keyHint",
                defaultMessage: "创建后不可修改，将被页面代码按此键引用",
              })
        }
      />
      <ProFormTextArea
        name="remark"
        label={intl.formatMessage({ id: "common.remark", defaultMessage: "备注" })}
      />
    </ModalForm>
  );
};

export default DictTypeFormModal;
