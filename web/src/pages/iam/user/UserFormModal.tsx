import type { DeptTreeOption } from "@/services/org/v1";
import type { User } from "@/services/user/v1";
import { numericOptions, useDict } from "@/hooks/useDict";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDependency,
  ProFormRadio,
  ProFormSelect,
  ProFormText,
  ProFormTextArea,
  ProFormTreeSelect,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App, Modal } from "antd";
import React, { useState } from "react";

type UserFormModalProps = {
  open: boolean;
  record?: User | undefined;
  deptTree: DeptTreeOption[];
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
  "deptId",
] as const;

type DeptOption = { title: string; value: number; disabled?: boolean; children?: DeptOption[] };

// Disabled departments show greyed-out but stay visible (RuoYi's
// buildDeptTreeSelect).
const toDeptTreeData = (nodes: DeptTreeOption[] | undefined): DeptOption[] =>
  (nodes ?? []).map((node) => ({
    title: node.label ?? "",
    value: node.deptId ?? 0,
    disabled: node.disabled,
    children: node.children?.length ? toDeptTreeData(node.children) : undefined,
  }));

type PostOption = { label: string; value: number };

/**
 * Create + edit share one form. Edit diffs against the loaded record and
 * PATCHes only the changed paths via updateMask (empty password = unchanged).
 *
 * Department + Post Assignment (岗位分配): the post candidates follow the
 * chosen department — edit loads ListUserPosts (the stored department's
 * posts with assigned flags), a department switch re-queries with the
 * dept_id override (the not-yet-saved choice); create reads the posts of
 * the chosen department from the post list. Posts sync as full replacement,
 * and only when the selection actually changed (avoiding pointless row
 * churn, ADR 0002).
 */
const UserFormModal: React.FC<UserFormModalProps> = ({ open, record, deptTree, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const genderOptions = useDict("sys_user_gender");
  const statusOptions = useDict("sys_normal_disable");
  const isEdit = Boolean(record);
  const [postOptions, setPostOptions] = useState<PostOption[]>([]);
  // Controlled selection: postIds is not part of the User DTO, so it lives in
  // state rather than the form.
  const [selectedPosts, setSelectedPosts] = useState<number[]>([]);
  // The assignment as loaded; the save only writes posts when the selection
  // moved away from it.
  const [loadedPostIds, setLoadedPostIds] = useState<number[]>([]);

  React.useEffect(() => {
    if (!open) {
      setPostOptions([]);
      setSelectedPosts([]);
      setLoadedPostIds([]);
      return;
    }
    if (!record?.userId) {
      return;
    }
    let cancelled = false;
    services.postAssignment
      .ListUserPosts({ userId: record.userId, deptId: 0 })
      .then((posts) => {
        if (cancelled) {
          return;
        }
        setPostOptions(
          (posts.items ?? []).map((item) => ({
            label: item.post?.postName
              ? `${item.post.postName}（${item.post.postCode ?? ""}）`
              : String(item.post?.postId ?? ""),
            value: item.post?.postId ?? 0,
          })),
        );
        const assigned = (posts.items ?? []).filter((item) => item.assigned).map((item) => item.post?.postId ?? 0);
        setLoadedPostIds(assigned);
        setSelectedPosts(assigned);
      })
      .catch(() => {
        // Candidate loading is best-effort: the form still saves the user;
        // the posts simply offer no options this round.
      });
    return () => {
      cancelled = true;
    };
    // record identity is stable per open (parent resets it on close).
  }, [open, record?.userId]);

  // A department switch re-derives the candidates for the NEW department
  // (create reads the post list directly — no user exists yet; edit uses the
  // assignment dialog's dept override) and clears the stale selection.
  const onDeptChange = async (deptId: number | undefined) => {
    setSelectedPosts([]);
    if (!deptId) {
      setPostOptions([]);
      return;
    }
    const options: PostOption[] = [];
    if (record?.userId) {
      const posts = await services.postAssignment.ListUserPosts({ userId: record.userId, deptId: deptId ?? 0 });
      for (const item of posts.items ?? []) {
        if (item.post) {
          options.push({
            label: item.post.postName ? `${item.post.postName}（${item.post.postCode ?? ""}）` : String(item.post.postId),
            value: item.post.postId ?? 0,
          });
        }
      }
    } else {
      const set = await services.post.ListPosts({
        pageSize: 200,
        pageToken: "",
        deptId: 0,
        filter: buildFilter([
          { field: "dept_id", value: deptId },
          { field: "status", value: 0 },
        ]),
        orderBy: undefined,
      });
      for (const post of set.posts ?? []) {
        options.push({ label: `${post.postName}（${post.postCode ?? ""}）`, value: post.postId ?? 0 });
      }
    }
    setPostOptions(options);
  };

  const samePosts = (a: number[] | undefined, b: number[]) => {
    const left = [...(a ?? [])].sort((x, y) => x - y);
    const right = [...b].sort((x, y) => x - y);
    return left.length === right.length && left.every((v, i) => v === right[i]);
  };

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
              deptId: record?.deptId || undefined,
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
        const postIds = selectedPosts;
        if (!isEdit) {
          const created = await services.user.CreateUser({
            user: {
              ...values,
              // gender/status default above; create requires the password.
            },
          });
          // Full replacement semantics; skip the call for an empty selection
          // — a fresh user has nothing to clear.
          if (postIds.length > 0) {
            await services.postAssignment.UpdateUserPosts({ userId: created.userId, postIds });
          }
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
        const postsChanged = !samePosts(postIds, loadedPostIds);
        if (!mask && !postsChanged) {
          message.info(intl.formatMessage({ id: "common.noChange", defaultMessage: "没有变更" }));
          onClose(false);
          return true;
        }

        // Security change: disabling a user revokes ALL their Login Sessions
        // (user module SessionRevocation policy) — say so before the fact.
        const disabling = record?.status === 0 && values.status === 1;
        const submit = async () => {
          if (mask) {
            await services.user.UpdateUser({
              user: { ...record, ...after } as User,
              updateMask: mask,
            });
          }
          if (postsChanged) {
            // Full replacement: empty selection clears the assignment.
            // isEdit guarantees record here; ?. keeps the shape biome-clean either way.
            await services.postAssignment.UpdateUserPosts({ userId: record?.userId ?? 0, postIds });
          }
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
      <ProFormTreeSelect
        name="deptId"
        label={intl.formatMessage({ id: "pages.dept.deptName", defaultMessage: "所属部门" })}
        placeholder={intl.formatMessage({
          id: "pages.user.dept.none",
          defaultMessage: "不选 = 无部门",
        })}
        fieldProps={{
          treeDefaultExpandAll: true,
          treeData: toDeptTreeData(deptTree),
          onChange: (value) => onDeptChange(value as number | undefined),
        }}
      />
      <ProFormSelect
        label={intl.formatMessage({ id: "pages.user.posts", defaultMessage: "岗位" })}
        options={postOptions}
        fieldProps={{
          mode: "multiple",
          value: selectedPosts,
          onChange: (values: number[]) => setSelectedPosts(values),
          placeholder: intl.formatMessage({
            id: "pages.user.posts.placeholder",
            defaultMessage: "先选部门；岗位随部门而定",
          }),
        }}
        extra={intl.formatMessage({
          id: "pages.user.posts.hint",
          defaultMessage: "全量替换：保存后岗位即为所选集合（空 = 清空）。",
        })}
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
        options={numericOptions(genderOptions)}
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
