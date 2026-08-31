import type { Role } from "@/services/access/v1";
import { DictTag } from "@/components";
import { useDict, valueEnumOf } from "@/hooks/useDict";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { createTokenPagedRequest } from "@/utils/pager";
import { PlusOutlined } from "@ant-design/icons";
import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Modal, Typography } from "antd";
import React, { useRef, useState } from "react";
import MenuGrantDrawer from "./MenuGrantDrawer";
import RoleFormModal from "./RoleFormModal";
import RoleUsersDrawer from "./RoleUsersDrawer";

const RoleList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  // Dict-driven rendering: labels and tag colors come from sys_normal_disable,
  // the single source of truth (no hardcoded fallback enum — see useDict).
  const statusOptions = useDict("sys_normal_disable");
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Role | undefined>();
  const [grantRole, setGrantRole] = useState<Role | undefined>();
  const [usersRole, setUsersRole] = useState<Role | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<Role>(async (pageSize, pageToken, params) => {
      const set = await services.role.ListRoles({
        pageSize,
        pageToken: pageToken || undefined,
        filter: buildFilter([
          { field: "role_name", value: String(params.roleName ?? "") || undefined },
          { field: "role_key", value: String(params.roleKey ?? "") || undefined },
          {
            field: "status",
            value:
              params.status === undefined || params.status === null || params.status === ""
                ? undefined
                : Number(params.status),
          },
        ]),
        orderBy: "role_sort",
      });
      return { items: set.roles ?? [], nextPageToken: set.nextPageToken };
    }),
    [],
  );

  const onDelete = (record: Role) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.role.delete.confirm", defaultMessage: "确定删除角色「{name}」吗？" },
        { name: record.roleName },
      ),
      content: intl.formatMessage({
        id: "pages.role.delete.hint",
        defaultMessage: "仍 assigned 有用户的角色会被服务端拒绝（ROLE_ASSIGNED）。",
      }),
      onOk: async () => {
        await services.role.DeleteRole({ roleId: record.roleId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<Role>[] = [
    { title: "ID", dataIndex: "roleId", width: 90, search: false },
    { title: intl.formatMessage({ id: "pages.role.roleName", defaultMessage: "角色名" }), dataIndex: "roleName" },
    { title: intl.formatMessage({ id: "pages.role.roleKey", defaultMessage: "角色 Key" }), dataIndex: "roleKey" },
    { title: intl.formatMessage({ id: "pages.role.roleSort", defaultMessage: "排序" }), dataIndex: "roleSort", search: false, width: 80 },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      valueType: "select",
      valueEnum: valueEnumOf(statusOptions),
      render: (_, record) => <DictTag typeKey="sys_normal_disable" value={record.status} />,
    },
    { title: intl.formatMessage({ id: "common.remark", defaultMessage: "备注" }), dataIndex: "remark", search: false, ellipsis: true },
    {
      title: intl.formatMessage({ id: "common.createdAt", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      search: false,
      valueType: "dateTime",
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 260,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("system:role:edit")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="grant" accessible={access.perm("system:role:edit")}>
          <a onClick={() => setGrantRole(record)}>
            {intl.formatMessage({ id: "pages.role.menuGrant", defaultMessage: "菜单授权" })}
          </a>
        </Access>,
        <Access key="users" accessible={access.perm("system:role:list")}>
          <a onClick={() => setUsersRole(record)}>
            {intl.formatMessage({ id: "pages.role.members", defaultMessage: "成员" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("system:role:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<Role>
        headerTitle={intl.formatMessage({ id: "menu.iam.role", defaultMessage: "角色管理" })}
        rowKey="roleId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("system:role:add")}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(undefined);
                setFormOpen(true);
              }}
            >
              {intl.formatMessage({ id: "common.new", defaultMessage: "新建" })}
            </Button>
          </Access>,
        ]}
      />
      <RoleFormModal
        open={formOpen}
        record={editing}
        onClose={(changed) => {
          setFormOpen(false);
          if (changed) {
            actionRef.current?.reload();
          }
        }}
      />
      <MenuGrantDrawer
        role={grantRole}
        onClose={(changed) => {
          if (changed) {
            actionRef.current?.reload();
          }
          setGrantRole(undefined);
        }}
      />
      <RoleUsersDrawer role={usersRole} onClose={() => setUsersRole(undefined)} />
    </PageContainer>
  );
};

export default RoleList;
