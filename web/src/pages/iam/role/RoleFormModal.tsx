import type { Role } from "@/services/access/v1";
import { numericOptions, useDict } from "@/hooks/useDict";
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

type RoleFormModalProps = {
  open: boolean;
  record?: Role | undefined;
  onClose: (changed: boolean) => void;
};

const EDITABLE_FIELDS = [
  "roleName",
  "roleKey",
  "roleSort",
  "status",
  "remark",
] as const;

const RoleFormModal: React.FC<RoleFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const statusOptions = useDict("sys_normal_disable");
  const isEdit = Boolean(record);

  return (
    <ModalForm<Role>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.role.edit", defaultMessage: "编辑角色" }
          : { id: "pages.role.new", defaultMessage: "新建角色" },
      )}
      width={520}
      open={open}
      initialValues={
        isEdit
          ? {
              roleName: record?.roleName,
              roleKey: record?.roleKey,
              roleSort: record?.roleSort ?? 0,
              status: record?.status ?? 0,
              remark: record?.remark,
            }
          : { roleSort: 0, status: 0 }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        if (!isEdit) {
          await services.role.CreateRole({ role: values, menuIds: undefined });
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
        // menu_check_strictly is intentionally absent: it is written only via
        // UpdateRoleMenus (the Menu Grant drawer), never through UpdateRole.
        await services.role.UpdateRole({
          role: { ...record, ...values } as Role,
          updateMask: mask,
        });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormText
        name="roleName"
        label={intl.formatMessage({ id: "pages.role.roleName", defaultMessage: "角色名" })}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="roleKey"
        label={intl.formatMessage({ id: "pages.role.roleKey", defaultMessage: "角色 Key" })}
        rules={[{ required: true }]}
      />
      <ProFormDigit
        name="roleSort"
        label={intl.formatMessage({ id: "pages.role.roleSort", defaultMessage: "排序" })}
        fieldProps={{ precision: 0 }}
      />
      <ProFormRadio.Group
        name="status"
        label={intl.formatMessage({ id: "common.status", defaultMessage: "状态" })}
        options={numericOptions(statusOptions)}
      />
      <ProFormTextArea
        name="remark"
        label={intl.formatMessage({ id: "common.remark", defaultMessage: "备注" })}
      />
    </ModalForm>
  );
};

export default RoleFormModal;
