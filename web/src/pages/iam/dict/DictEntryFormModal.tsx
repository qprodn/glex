import type { DictEntry } from "@/services/dict/v1";
import { TAG_STYLES, tagStyleLabel } from "@/hooks/useDict";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDigit,
  ProFormRadio,
  ProFormText,
  ProFormTextArea,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type DictEntryFormModalProps = {
  open: boolean;
  /** The owning type key, fixed by the drawer that opened this modal. */
  dictTypeKey: string | undefined;
  record?: DictEntry | undefined;
  onClose: (changed: boolean) => void;
};

// The owning dict_type is deliberately absent: immutable after create
// (ADR 0006) — moving an entry between types is withdraw-and-recreate.
const EDITABLE_FIELDS = [
  "dictLabel",
  "dictValue",
  "dictSort",
  "tagStyle",
  "isDefault",
  "remark",
] as const;

const DictEntryFormModal: React.FC<DictEntryFormModalProps> = ({
  open,
  dictTypeKey,
  record,
  onClose,
}) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);

  return (
    <ModalForm<DictEntry>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.dict.entry.edit", defaultMessage: "编辑字典项" }
          : { id: "pages.dict.entry.new", defaultMessage: "新建字典项" },
      )}
      width={520}
      open={open}
      initialValues={
        isEdit
          ? {
              dictLabel: record?.dictLabel,
              dictValue: record?.dictValue,
              dictSort: record?.dictSort ?? 0,
              tagStyle: record?.tagStyle ?? "TAG_STYLE_DEFAULT",
              isDefault: record?.isDefault ?? false,
              remark: record?.remark,
            }
          : { dictSort: 0, tagStyle: "TAG_STYLE_DEFAULT", isDefault: false }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        if (!isEdit) {
          await services.dict.CreateDictEntry({
            entry: { ...values, dictType: dictTypeKey } as DictEntry,
          });
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
        await services.dict.UpdateDictEntry({
          entry: { ...record, ...values } as DictEntry,
          updateMask: mask,
        });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormText
        name="dictLabel"
        label={intl.formatMessage({ id: "pages.dict.entry.label", defaultMessage: "标签" })}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="dictValue"
        label={intl.formatMessage({ id: "pages.dict.entry.value", defaultMessage: "键值" })}
        rules={[{ required: true }]}
      />
      <ProFormDigit
        name="dictSort"
        label={intl.formatMessage({ id: "pages.dict.entry.sort", defaultMessage: "排序" })}
        fieldProps={{ precision: 0 }}
      />
      <ProFormRadio.Group
        name="tagStyle"
        label={intl.formatMessage({ id: "pages.dict.entry.tagStyle", defaultMessage: "样式" })}
        options={TAG_STYLES.map((style) => ({ value: style, label: tagStyleLabel(style) }))}
      />
      <ProFormRadio.Group
        name="isDefault"
        label={intl.formatMessage({ id: "pages.dict.entry.isDefault", defaultMessage: "默认" })}
        options={[
          { value: false, label: intl.formatMessage({ id: "common.no", defaultMessage: "否" }) },
          { value: true, label: intl.formatMessage({ id: "common.yes", defaultMessage: "是" }) },
        ]}
      />
      <ProFormTextArea
        name="remark"
        label={intl.formatMessage({ id: "common.remark", defaultMessage: "备注" })}
      />
    </ModalForm>
  );
};

export default DictEntryFormModal;
