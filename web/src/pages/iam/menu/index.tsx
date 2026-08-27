import type { Menu } from "@/services/access/v1";
import { services } from "@/services";
import { iconFor } from "@/router/icons";
import { PlusOutlined } from "@ant-design/icons";
import {
  PageContainer,
  type ProColumns,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Modal, Tag, Typography } from "antd";
import React, { useState } from "react";
import MenuFormModal from "./MenuFormModal";

const MENU_TYPE_LABEL: Record<string, { text: string; color: string }> = {
  MENU_TYPE_DIRECTORY: { text: "目录", color: "processing" },
  MENU_TYPE_MENU: { text: "菜单", color: "success" },
  MENU_TYPE_BUTTON: { text: "按钮", color: "warning" },
};

const MenuList: React.FC = () => {
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [menus, setMenus] = useState<Menu[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<number[] | undefined>();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Menu | undefined>();
  const [presetParent, setPresetParent] = useState<number | undefined>();

  const collectIds = (nodes: Menu[] | undefined): number[] =>
    (nodes ?? []).flatMap((node) => [node.menuId ?? 0, ...collectIds(node.children)]);

  const reload = async () => {
    const tree = await services.menu.ListMenus({ filter: undefined, orderBy: undefined });
    setMenus(tree.menus ?? []);
    // Controlled expansion: initialize all-open on first load; afterwards the
    // user's collapse choices survive refreshes of the data.
    setExpandedKeys((prev) => prev ?? collectIds(tree.menus));
  };

  React.useEffect(() => {
    reload();
  }, []);

  const onDelete = (record: Menu) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.menu.delete.confirm", defaultMessage: "确定删除菜单「{name}」吗？" },
        { name: record.menuName },
      ),
      content: intl.formatMessage({
        id: "pages.menu.delete.hint",
        defaultMessage: "仍有子菜单的删除会被服务端拒绝（MENU_HAS_CHILDREN）。",
      }),
      onOk: async () => {
        await services.menu.DeleteMenu({ menuId: record.menuId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        reload();
      },
    });
  };

  const typeLabel = (type: string | undefined) => {
    const meta = MENU_TYPE_LABEL[type ?? ""];
    return meta ? <Tag color={meta.color}>{meta.text}</Tag> : type;
  };

  const columns: ProColumns<Menu>[] = [
    {
      title: intl.formatMessage({ id: "pages.menu.menuName", defaultMessage: "菜单名" }),
      dataIndex: "menuName",
      width: 200,
    },
    {
      title: intl.formatMessage({ id: "pages.menu.icon", defaultMessage: "图标" }),
      dataIndex: "icon",
      width: 64,
      render: (_, record) => iconFor(record.icon),
    },
    {
      title: intl.formatMessage({ id: "pages.menu.menuType", defaultMessage: "类型" }),
      dataIndex: "menuType",
      width: 80,
      render: (_, record) => typeLabel(record.menuType),
    },
    { title: intl.formatMessage({ id: "pages.menu.orderNum", defaultMessage: "排序" }), dataIndex: "orderNum", width: 72 },
    {
      title: intl.formatMessage({ id: "pages.menu.perms", defaultMessage: "权限句柄" }),
      dataIndex: "perms",
      render: (_, record) => (
        <Typography.Text code copyable={Boolean(record.perms)}>
          {record.perms}
        </Typography.Text>
      ),
    },
    { title: intl.formatMessage({ id: "pages.menu.path", defaultMessage: "路由地址" }), dataIndex: "path", ellipsis: true },
    {
      title: intl.formatMessage({ id: "pages.menu.component", defaultMessage: "组件" }),
      dataIndex: "component",
      ellipsis: true,
      render: (_, record) =>
        record.component ? (
          <Typography.Text code>{record.component}</Typography.Text>
        ) : (
          "-"
        ),
    },
    {
      title: intl.formatMessage({ id: "pages.menu.visible", defaultMessage: "显示" }),
      dataIndex: "visible",
      width: 72,
      render: (_, record) =>
        record.visible === 0 ? (
          <Tag color="success">{intl.formatMessage({ id: "common.show", defaultMessage: "显示" })}</Tag>
        ) : (
          <Tag>{intl.formatMessage({ id: "common.hide", defaultMessage: "隐藏" })}</Tag>
        ),
    },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 72,
      render: (_, record) =>
        record.status === 0 ? (
          <Tag color="success">{intl.formatMessage({ id: "common.status.normal", defaultMessage: "正常" })}</Tag>
        ) : (
          <Tag color="error">{intl.formatMessage({ id: "common.status.disabled", defaultMessage: "停用" })}</Tag>
        ),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 180,
      render: (_, record) => [
        <Access key="addChild" accessible={access.perm("system:menu:add")}>
          <a
            onClick={() => {
              setEditing(undefined);
              setPresetParent(record.menuId);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "pages.menu.addChild", defaultMessage: "新增子项" })}
          </a>
        </Access>,
        <Access key="edit" accessible={access.perm("system:menu:edit")}>
          <a
            onClick={() => {
              setEditing(record);
              setPresetParent(undefined);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("system:menu:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<Menu>
        headerTitle={intl.formatMessage({ id: "menu.iam.menu", defaultMessage: "菜单管理" })}
        rowKey="menuId"
        search={false}
        columns={columns}
        dataSource={menus}
        pagination={false}
        expandable={{
          expandedRowKeys: expandedKeys,
          onExpandedRowsChange: (keys) => setExpandedKeys(keys.map(Number)),
        }}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("system:menu:add")}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(undefined);
                setPresetParent(undefined);
                setFormOpen(true);
              }}
            >
              {intl.formatMessage({ id: "common.new", defaultMessage: "新建" })}
            </Button>
          </Access>,
        ]}
      />
      <MenuFormModal
        open={formOpen}
        record={editing}
        presetParent={presetParent}
        menus={menus}
        onClose={(changed) => {
          setFormOpen(false);
          setPresetParent(undefined);
          if (changed) {
            reload();
          }
        }}
      />
    </PageContainer>
  );
};

export default MenuList;
