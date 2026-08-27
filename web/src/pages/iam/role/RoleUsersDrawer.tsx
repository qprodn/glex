import type { userv1_User } from "@/services/access/v1";
import type { Role } from "@/services/access/v1";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { createTokenPagedRequest } from "@/utils/pager";
import { type ProColumns, ProTable } from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { Drawer, Tag } from "antd";
import React from "react";

type RoleUsersDrawerProps = {
  role?: Role | undefined;
  onClose: () => void;
};

/**
 * Read-only member view over ListRoleUsers (the "查" side of the Role
 * Assignment link table); assignment itself is edited from user management.
 */
const RoleUsersDrawer: React.FC<RoleUsersDrawerProps> = ({ role, onClose }) => {
  const intl = useIntl();
  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<userv1_User>(async (pageSize, pageToken, params) => {
      const set = await services.assignment.ListRoleUsers({
        roleId: role?.roleId ?? 0,
        pageSize,
        pageToken: pageToken || undefined,
        filter: buildFilter([
          { field: "user_name", value: String(params.userName ?? "") || undefined },
        ]),
        orderBy: "user_id",
      });
      return { items: set.users ?? [], nextPageToken: set.nextPageToken };
    }),
    [role?.roleId],
  );

  const columns: ProColumns<userv1_User>[] = [
    { title: "ID", dataIndex: "userId", width: 90, search: false },
    { title: intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" }), dataIndex: "userName" },
    { title: intl.formatMessage({ id: "pages.user.nickName", defaultMessage: "昵称" }), dataIndex: "nickName", search: false },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      search: false,
      render: (_, record) => (
        <Tag color={record.status === 0 ? "success" : "error"}>
          {record.status === 0
            ? intl.formatMessage({ id: "common.status.normal", defaultMessage: "正常" })
            : intl.formatMessage({ id: "common.status.disabled", defaultMessage: "停用" })}
        </Tag>
      ),
    },
    {
      title: intl.formatMessage({ id: "common.createdAt", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      search: false,
      valueType: "dateTime",
    },
  ];

  return (
    <Drawer
      open={Boolean(role)}
      width={640}
      onClose={onClose}
      title={intl.formatMessage(
        { id: "pages.role.members.title", defaultMessage: "成员 — {name}" },
        { name: role?.roleName },
      )}
    >
      <ProTable<userv1_User>
        rowKey="userId"
        search={false}
        options={false}
        columns={columns}
        request={role ? pager : undefined}
        pagination={{ pageSize: 10 }}
      />
    </Drawer>
  );
};

export default RoleUsersDrawer;
