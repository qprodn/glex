import type { Dept } from "@/services/org/v1";
import { numericOptions, useDict } from "@/hooks/useDict";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDependency,
  ProFormDigit,
  ProFormRadio,
  ProFormText,
  ProFormTreeSelect,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type DeptFormModalProps = {
  open: boolean;
  record?: Dept | undefined;
  /** Preselect this parent when opened via 新增子项. */
  presetParent?: number | undefined;
  depts: Dept[];
  onClose: (changed: boolean) => void;
};

const EDITABLE_FIELDS = ["parentId", "deptName", "orderNum", "leader", "phone", "email", "status"] as const;

type ParentOption = { title: string; value: number; disabled?: boolean; children?: ParentOption[] };

// Parent candidates are the whole tree minus the editing node and its
// descendants — moving under one's own subtree is refused server-side
// (DEPT_PARENT_INVALID), so the options never offer it (RuoYi's
// excludeDeptIds, computed from the loaded tree instead of a second
// endpoint).
const toParentTreeData = (depts: Dept[] | undefined, excludeId: number | undefined): ParentOption[] =>
  (depts ?? [])
    .filter((dept) => dept.deptId !== excludeId)
    .map((dept) => ({
      title: dept.deptName ?? "",
      value: dept.deptId ?? 0,
      disabled: dept.status === 1,
      children: dept.children?.length ? toParentTreeData(dept.children, excludeId) : undefined,
    }));

const DeptFormModal: React.FC<DeptFormModalProps> = ({ open, record, presetParent, depts, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const statusOptions = useDict("sys_normal_disable");
  const isEdit = Boolean(record);
  // A fresh department defaults under the seeded root (the tree's first
  // root); presetParent wins when opened via 新增子项.
  const defaultParent = presetParent ?? depts[0]?.deptId ?? 0;

  return (
    <ModalForm<Dept>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.dept.edit", defaultMessage: "编辑部门" }
          : { id: "pages.dept.new", defaultMessage: "新建部门" },
      )}
      width={560}
      open={open}
      initialValues={
        isEdit
          ? {
              parentId: record?.parentId ?? 0,
              deptName: record?.deptName,
              orderNum: record?.orderNum ?? 0,
              leader: record?.leader ?? undefined,
              phone: record?.phone,
              email: record?.email,
              status: record?.status ?? 0,
            }
          : { parentId: defaultParent, orderNum: 0, status: 0 }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        const payload: Dept = { ...values, deptId: record?.deptId };
        if (!isEdit) {
          await services.dept.CreateDept({ dept: payload });
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
        await services.dept.UpdateDept({ dept: payload, updateMask: mask });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormTreeSelect
        name="parentId"
        label={intl.formatMessage({ id: "pages.dept.parent", defaultMessage: "上级部门" })}
        allowClear={false}
        fieldProps={{
          treeDefaultExpandAll: true,
          treeData: toParentTreeData(depts, record?.deptId),
        }}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="deptName"
        label={intl.formatMessage({ id: "pages.dept.deptName", defaultMessage: "部门名称" })}
        rules={[{ required: true }, { max: 30 }]}
      />
      <ProFormDigit
        name="orderNum"
        label={intl.formatMessage({ id: "pages.dept.orderNum", defaultMessage: "排序" })}
        fieldProps={{ precision: 0, min: 0 }}
      />
      <ProFormDigit
        name="leader"
        label={intl.formatMessage({ id: "pages.dept.leader", defaultMessage: "负责人ID" })}
        fieldProps={{ precision: 0 }}
        extra={intl.formatMessage({
          id: "pages.dept.leader.hint",
          defaultMessage: "System User ID（0/留空 = 未设置）；仅展示用途。",
        })}
      />
      <ProFormText
        name="phone"
        label={intl.formatMessage({ id: "pages.dept.phone", defaultMessage: "联系电话" })}
        fieldProps={{ maxLength: 11 }}
      />
      <ProFormText
        name="email"
        label={intl.formatMessage({ id: "pages.dept.email", defaultMessage: "邮箱" })}
        rules={[{ type: "email" }]}
      />
      <ProFormRadio.Group
        name="status"
        label={intl.formatMessage({ id: "common.status", defaultMessage: "状态" })}
        options={numericOptions(statusOptions)}
      />
      <ProFormDependency name={["status"]}>
        {({ status }) =>
          isEdit && record?.status === 0 && status === 1 ? (
            <div style={{ color: "rgba(0,0,0,0.45)" }}>
              {intl.formatMessage({
                id: "pages.dept.disable.hint",
                defaultMessage: "停用被拒绝的情形：部门下仍有用户，或存在未停用的子部门",
              })}
            </div>
          ) : null
        }
      </ProFormDependency>
    </ModalForm>
  );
};

export default DeptFormModal;
