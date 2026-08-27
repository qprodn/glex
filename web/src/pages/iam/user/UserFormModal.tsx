import type { User } from "@/services/user/v1";
import { services } from "@/services";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDependency,
  ProFormRadio,
  ProFormText,
  ProFormTextArea,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Modal } from "antd";
import React from "react";

type UserFormModalProps = {
  open: boolean;
  record?: User | undefined;
  onClose: (changed: boolean) => void;
};

const EDITABLE_FIELDS = [
  "nickName",
  "email",
  "phoneNumber",
  "gender",
  "status",
  "remark",
  "password",
] as const;

/**
 * Create + edit share one form. Edit diffs against the loaded record and
 * PATCHes only the changed paths via updateMask (empty password = unchanged).
 */
const UserFormModal: React.FC<UserFormModalProps> = ({ open, record, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);

  return (
    <ModalForm<User>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.user.edit", defaultMessage: "编辑用户" }
          : { id: "pages.user.new", defaultMessage: "新建用户" },
      )}
      width={520}
      open={open}
      initialValues={
        isEdit
          ? {
              userName: record?.userName,
              nickName: record?.nickName,
              email: record?.email,
              phoneNumber: record?.phoneNumber,
              gender: record?.gender ?? 2,
              status: record?.status ?? 0,
              remark: record?.remark,
            }
          : { gender: 2, status: 0 }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        if (!isEdit) {
          await services.user.CreateUser({
            user: {
              ...values,
              // gender/status default above; create requires the password.
            },
          });
          message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
          onClose(true);
          return true;
        }

        const after = {
          ...values,
          // Treat a blank password on edit as "leave unchanged": drop it from
          // the diff so the mask never contains password.
          password: values.password?.trim() ? values.password : undefined,
        };
        const mask = collectUpdateMask(record, after, EDITABLE_FIELDS);
        if (!mask) {
          message.info(intl.formatMessage({ id: "common.noChange", defaultMessage: "没有变更" }));
          onClose(false);
          return true;
        }

        // Security change: disabling a user revokes ALL their Login Sessions
        // (user module SessionRevocation policy) — say so before the fact.
        const disabling = record?.status === 0 && values.status === 1;
        const submit = async () => {
          await services.user.UpdateUser({
            user: { ...record, ...after } as User,
            updateMask: mask,
          });
          message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
          onClose(true);
        };
        if (disabling) {
          Modal.confirm({
            title: intl.formatMessage({
              id: "pages.user.disable.confirmTitle",
              defaultMessage: "停用该用户？",
            }),
            content: intl.formatMessage({
              id: "pages.user.disable.confirmContent",
              defaultMessage: "停用将强制下线该用户的全部登录会话。",
            }),
            onOk: submit,
          });
          return false;
        }
        await submit();
        return true;
      }}
    >
      <ProFormText
        name="userName"
        label={intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" })}
        disabled={isEdit}
        rules={[{ required: !isEdit }]}
      />
      <ProFormText
        name="nickName"
        label={intl.formatMessage({ id: "pages.user.nickName", defaultMessage: "昵称" })}
        rules={[{ required: true }]}
      />
      <ProFormText.Password
        name="password"
        label={intl.formatMessage({ id: "common.password", defaultMessage: "密码" })}
        placeholder={
          isEdit
            ? intl.formatMessage({
                id: "pages.user.password.keep",
                defaultMessage: "留空则不修改",
              })
            : ""
        }
        rules={[{ required: !isEdit }]}
      />
      <ProFormText
        name="email"
        label={intl.formatMessage({ id: "pages.user.email", defaultMessage: "邮箱" })}
        rules={[{ type: "email" }]}
      />
      <ProFormText
        name="phoneNumber"
        label={intl.formatMessage({ id: "pages.user.phoneNumber", defaultMessage: "手机号" })}
      />
      <ProFormRadio.Group
        name="gender"
        label={intl.formatMessage({ id: "common.gender", defaultMessage: "性别" })}
        options={[
          { value: 0, label: intl.formatMessage({ id: "common.gender.male", defaultMessage: "男" }) },
          { value: 1, label: intl.formatMessage({ id: "common.gender.female", defaultMessage: "女" }) },
          { value: 2, label: intl.formatMessage({ id: "common.gender.unknown", defaultMessage: "未知" }) },
        ]}
      />
      <ProFormRadio.Group
        name="status"
        label={intl.formatMessage({ id: "common.status", defaultMessage: "状态" })}
        options={[
          { value: 0, label: intl.formatMessage({ id: "common.status.normal", defaultMessage: "正常" }) },
          { value: 1, label: intl.formatMessage({ id: "common.status.disabled", defaultMessage: "停用" }) },
        ]}
      />
      <ProFormTextArea
        name="remark"
        label={intl.formatMessage({ id: "common.remark", defaultMessage: "备注" })}
      />
      <ProFormDependency name={["status"]}>
        {({ status }) =>
          isEdit && status === 1 ? (
            <div style={{ color: "rgba(0,0,0,0.45)" }}>
              {intl.formatMessage({
                id: "pages.user.disable.hint",
                defaultMessage: "保存为停用会强制下线该用户全部会话",
              })}
            </div>
          ) : null
        }
      </ProFormDependency>
    </ModalForm>
  );
};

export default UserFormModal;
