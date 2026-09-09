import type { DeptTreeOption } from "@/services/org/v1";
import type { User } from "@/services/user/v1";
import { DictTag } from "@/components";
import { useDict, valueEnumOf } from "@/hooks/useDict";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { buildDeptNames, toDeptTreeData } from "@/utils/deptTree";
import { downloadWorkbook } from "@/utils/fileTransfer";
import { createTokenPagedRequest } from "@/utils/pager";
import { DownloadOutlined, PlusOutlined, UploadOutlined } from "@ant-design/icons";
import {
  type ActionType,
  PageContainer,
  type ProColumns,
  type ProFormInstance,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Card, Modal, Tree, Typography } from "antd";
import React, { useRef, useState } from "react";
import ImportUsersModal from "./ImportUsersModal";
import RoleAssignDrawer from "./RoleAssignDrawer";
import UserFormModal from "./UserFormModal";

const UserList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  // Dict-driven rendering: labels and tag colors come from the dict types,
  // the single source of truth (no hardcoded fallback enum — see useDict).
  const statusOptions = useDict("sys_normal_disable");
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<User | undefined>();
  const [assignUser, setAssignUser] = useState<User | undefined>();
  const [deptTree, setDeptTree] = useState<DeptTreeOption[]>([]);
  const [selectedDept, setSelectedDept] = useState<number | undefined>();
  const [importOpen, setImportOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const searchFormRef = useRef<ProFormInstance>(undefined);

  React.useEffect(() => {
    services.dept.ListDeptTree({}).then((tree) => setDeptTree(tree.options ?? []));
  }, []);

  const deptNames = React.useMemo(() => buildDeptNames(deptTree), [deptTree]);

  // proto ListUsersRequest documents the supported AIP-160 fields; the search
  // form offers exactly those. dept_id is the separate subtree parameter —
  // clicking the left tree filters by that department and every descendant.
  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<User>(async (pageSize, pageToken, params) => {
      const set = await services.user.ListUsers({
        pageSize,
        pageToken: pageToken || undefined,
        filter: buildFilter([
          { field: "user_name", value: String(params.userName ?? "") || undefined },
          { field: "phone_number", value: String(params.phoneNumber ?? "") || undefined },
          {
            field: "status",
            value: params.status === undefined || params.status === null || params.status === ""
              ? undefined
              : Number(params.status),
          },
        ]),
        orderBy: "created_at desc",
        deptId: selectedDept ?? 0,
      });
      return { items: set.users ?? [], nextPageToken: set.nextPageToken };
    }),
    [selectedDept],
  );

  // Export carries the list page's exact filter (search form + department
  // subtree) — the backend follows the same fields (ADR 0017).
  const onExport = async () => {
    const params = (searchFormRef.current?.getFieldsValue() ?? {}) as {
      userName?: string;
      phoneNumber?: string;
      status?: number | string | null;
    };
    setExporting(true);
    try {
      await downloadWorkbook(
        "/iam/user/-/export",
        {
          filter: buildFilter([
            { field: "user_name", value: params.userName || undefined },
            { field: "phone_number", value: params.phoneNumber || undefined },
            {
              field: "status",
              value:
                params.status === undefined || params.status === null || params.status === ""
                  ? undefined
                  : Number(params.status),
            },
          ]),
          orderBy: "created_at desc",
          deptId: selectedDept ?? 0,
        },
        "用户数据.xlsx",
      );
    } finally {
      setExporting(false);
    }
  };

  const onDelete = (record: User) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.user.delete.confirm", defaultMessage: "确定删除用户「{name}」吗？" },
        { name: record.nickName ?? record.userName },
      ),
      content: intl.formatMessage({
        id: "pages.user.delete.hint",
        defaultMessage: "软删除；该用户的全部登录会话将立即吊销。",
      }),
      onOk: async () => {
        await services.user.DeleteUser({ userId: record.userId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<User>[] = [
    { title: "ID", dataIndex: "userId", width: 90, search: false },
    { title: intl.formatMessage({ id: "pages.user.userName", defaultMessage: "用户名" }), dataIndex: "userName" },
    { title: intl.formatMessage({ id: "pages.user.nickName", defaultMessage: "昵称" }), dataIndex: "nickName", search: false },
    {
      title: intl.formatMessage({ id: "pages.dept.belong", defaultMessage: "所属部门" }),
      dataIndex: "deptId",
      search: false,
      render: (_, record) => (record.deptId ? deptNames.get(record.deptId) ?? record.deptId : "-"),
    },
    { title: intl.formatMessage({ id: "pages.user.email", defaultMessage: "邮箱" }), dataIndex: "email", search: false, ellipsis: true },
    { title: intl.formatMessage({ id: "pages.user.phoneNumber", defaultMessage: "手机号" }), dataIndex: "phoneNumber" },
    {
      title: intl.formatMessage({ id: "common.gender", defaultMessage: "性别" }),
      dataIndex: "gender",
      search: false, // user ListUsers declares no gender filter field
      render: (_, record) => <DictTag typeKey="sys_user_gender" value={record.gender} />,
    },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      valueType: "select",
      valueEnum: valueEnumOf(statusOptions),
      render: (_, record) => <DictTag typeKey="sys_normal_disable" value={record.status} />,
    },
    { title: intl.formatMessage({ id: "pages.user.loginIp", defaultMessage: "登录 IP" }), dataIndex: "loginIp", search: false },
    {
      title: intl.formatMessage({ id: "pages.user.loginDate", defaultMessage: "上次登录" }),
      dataIndex: "loginDate",
      search: false,
      valueType: "dateTime",
    },
    {
      title: intl.formatMessage({ id: "common.createdAt", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      search: false,
      valueType: "dateTime",
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 220,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("system:user:edit")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="assign" accessible={access.perm("system:user:edit")}>
          <a onClick={() => setAssignUser(record)}>
            {intl.formatMessage({ id: "pages.user.assignRoles", defaultMessage: "分配角色" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("system:user:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <div style={{ display: "flex", gap: 16, alignItems: "flex-start" }}>
        <Card
          size="small"
          style={{ width: 240, flexShrink: 0 }}
          title={intl.formatMessage({ id: "pages.dept.tree", defaultMessage: "部门" })}
        >
          <Tree
            blockNode
            defaultExpandAll
            treeData={toDeptTreeData(deptTree)}
            selectedKeys={selectedDept === undefined ? [] : [selectedDept]}
            onSelect={(keys) => {
              setSelectedDept(keys.length ? Number(keys[0]) : undefined);
              actionRef.current?.reload();
            }}
          />
        </Card>
        <ProTable<User>
          headerTitle={intl.formatMessage({ id: "menu.iam.user", defaultMessage: "用户管理" })}
          rowKey="userId"
          actionRef={actionRef}
          search={{ labelWidth: "auto" }}
          columns={columns}
          request={pager}
          formRef={searchFormRef}
          toolBarRender={() => [
            <Access key="add" accessible={access.perm("system:user:add")}>
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
            <Access key="import" accessible={access.perm("system:user:import")}>
              <Button icon={<UploadOutlined />} onClick={() => setImportOpen(true)}>
                {intl.formatMessage({ id: "common.import", defaultMessage: "导入" })}
              </Button>
            </Access>,
            <Access key="export" accessible={access.perm("system:user:export")}>
              <Button icon={<DownloadOutlined />} loading={exporting} onClick={onExport}>
                {intl.formatMessage({ id: "common.export", defaultMessage: "导出" })}
              </Button>
            </Access>,
          ]}
        />
      </div>
      <UserFormModal
        open={formOpen}
        record={editing}
        deptTree={deptTree}
        onClose={(changed) => {
          setFormOpen(false);
          setEditing(undefined);
          if (changed) {
            actionRef.current?.reload();
          }
        }}
      />
      <ImportUsersModal
        open={importOpen}
        onClose={(changed) => {
          setImportOpen(false);
          if (changed) {
            actionRef.current?.reload();
          }
        }}
      />
      <RoleAssignDrawer
        user={assignUser}
        onClose={(changed) => {
          if (changed) {
            actionRef.current?.reload();
          }
          setAssignUser(undefined);
        }}
      />
    </PageContainer>
  );
};

export default UserList;
