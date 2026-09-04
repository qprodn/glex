import type { DeptTreeOption, Post } from "@/services/org/v1";
import { numericOptions, useDict } from "@/hooks/useDict";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDigit,
  ProFormRadio,
  ProFormText,
  ProFormTextArea,
  ProFormTreeSelect,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type PostFormModalProps = {
  open: boolean;
  record?: Post | undefined;
  deptTree: DeptTreeOption[];
  onClose: (changed: boolean) => void;
};

const EDITABLE_FIELDS = ["deptId", "postCode", "postName", "postSort", "status", "remark"] as const;

type DeptOption = { title: string; value: number; disabled?: boolean; children?: DeptOption[] };

// Disabled departments show greyed-out but stay visible (RuoYi's
// buildDeptTreeSelect): a disabled parent can still be read, just not chosen.
const toDeptTreeData = (nodes: DeptTreeOption[] | undefined): DeptOption[] =>
  (nodes ?? []).map((node) => ({
    title: node.label ?? "",
    value: node.deptId ?? 0,
    disabled: node.disabled,
    children: node.children?.length ? toDeptTreeData(node.children) : undefined,
  }));

/**
 * 岗位编辑器. Posts are department-scoped (ADR 0015): the department is
 * required, post_name is unique within it, post_code globally — both
 * uniqueness rules are enforced server-side.
 */
const PostFormModal: React.FC<PostFormModalProps> = ({ open, record, deptTree, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const statusOptions = useDict("sys_normal_disable");
  const isEdit = Boolean(record);

  return (
    <ModalForm<Post>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.post.edit", defaultMessage: "编辑岗位" }
          : { id: "pages.post.new", defaultMessage: "新建岗位" },
      )}
      width={520}
      open={open}
      initialValues={
        isEdit
          ? {
              deptId: record?.deptId,
              postCode: record?.postCode,
              postName: record?.postName,
              postSort: record?.postSort ?? 0,
              status: record?.status ?? 0,
              remark: record?.remark,
            }
          : { postSort: 0, status: 0 }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        const payload: Post = { ...values, postId: record?.postId };
        if (!isEdit) {
          await services.post.CreatePost({ post: payload });
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
        await services.post.UpdatePost({ post: payload, updateMask: mask });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormTreeSelect
        name="deptId"
        label={intl.formatMessage({ id: "pages.dept.deptName", defaultMessage: "所属部门" })}
        fieldProps={{
          treeDefaultExpandAll: true,
          treeData: toDeptTreeData(deptTree),
        }}
        rules={[{ required: true }]}
      />
      <ProFormText
        name="postCode"
        label={intl.formatMessage({ id: "pages.post.postCode", defaultMessage: "岗位编码" })}
        placeholder="fe-dev"
        rules={[{ required: true }, { max: 64 }]}
        extra={intl.formatMessage({
          id: "pages.post.postCode.hint",
          defaultMessage: "全局唯一",
        })}
      />
      <ProFormText
        name="postName"
        label={intl.formatMessage({ id: "pages.post.postName", defaultMessage: "岗位名称" })}
        rules={[{ required: true }, { max: 50 }]}
        extra={intl.formatMessage({
          id: "pages.post.postName.hint",
          defaultMessage: "同一部门内唯一",
        })}
      />
      <ProFormDigit
        name="postSort"
        label={intl.formatMessage({ id: "pages.post.postSort", defaultMessage: "排序" })}
        fieldProps={{ precision: 0, min: 0 }}
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

export default PostFormModal;
