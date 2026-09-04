import type { Dept } from "@/services/org/v1";
import { DictTag } from "@/components";
import { services } from "@/services";
import { PlusOutlined } from "@ant-design/icons";
import {
  PageContainer,
  type ProColumns,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Modal, Typography } from "antd";
import React, { useState } from "react";
import DeptFormModal from "./DeptFormModal";

/**
 * 部门管理: server-built tree (ADR 0014 — parent_id only, no ancestors
 * path). The parent-select options exclude the editing node and its
 * descendants (computed from the loaded tree, RuoYi's excludeDeptIds
 * without the extra endpoint).
 */
const DeptList: React.FC = () => {
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [depts, setDepts] = useState<Dept[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<number[] | undefined>();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Dept | undefined>();
  const [presetParent, setPresetParent] = useState<number | undefined>();

  const collectIds = (nodes: Dept[] | undefined): number[] =>
    (nodes ?? []).flatMap((node) => [node.deptId ?? 0, ...collectIds(node.children)]);

  const reload = async () => {
    const tree = await services.dept.ListDepts({});
    setDepts(tree.depts ?? []);
    // Controlled expansion: initialize all-open on first load; afterwards the
    // user's collapse choices survive refreshes of the data.
    setExpandedKeys((prev) => prev ?? collectIds(tree.depts));
  };

  React.useEffect(() => {
    reload();
  }, []);

  const onDelete = (record: Dept) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.dept.delete.confirm", defaultMessage: "确定删除部门「{name}」吗？" },
        { name: record.deptName },
      ),
      content: intl.formatMessage({
        id: "pages.dept.delete.hint",
        defaultMessage: "根部门、仍有下级部门/用户/岗位的删除会被服务端拒绝。",
      }),
      onOk: async () => {
        await services.dept.DeleteDept({ deptId: record.deptId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        reload();
      },
    });
  };

  const columns: ProColumns<Dept>[] = [
    {
      title: intl.formatMessage({ id: "pages.dept.deptName", defaultMessage: "部门名称" }),
      dataIndex: "deptName",
      width: 220,
    },
    {
      title: intl.formatMessage({ id: "pages.dept.orderNum", defaultMessage: "排序" }),
      dataIndex: "orderNum",
      width: 72,
    },
    {
      title: intl.formatMessage({ id: "pages.dept.leader", defaultMessage: "负责人" }),
      dataIndex: "leader",
      width: 120,
      search: false,
      // leader is a System User id recorded for display; resolving it needs a
      // per-row user read, so the raw id shows until a leader picker lands.
      render: (_, record) => (record.leader ? <Typography.Text code>{record.leader}</Typography.Text> : "-"),
    },
    {
      title: intl.formatMessage({ id: "pages.dept.phone", defaultMessage: "联系电话" }),
      dataIndex: "phone",
      width: 140,
      search: false,
    },
    {
      title: intl.formatMessage({ id: "pages.dept.email", defaultMessage: "邮箱" }),
      dataIndex: "email",
      width: 200,
      search: false,
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      width: 80,
      render: (_, record) => <DictTag typeKey="sys_normal_disable" value={record.status ?? 0} />,
    },
    {
      title: intl.formatMessage({ id: "common.createdAt", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      width: 160,
      search: false,
      valueType: "dateTime",
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 180,
      render: (_, record) => [
        <Access key="addChild" accessible={access.perm("system:dept:add")}>
          <a
            onClick={() => {
              setEditing(undefined);
              setPresetParent(record.deptId);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "pages.dept.addChild", defaultMessage: "新增子项" })}
          </a>
        </Access>,
        <Access key="edit" accessible={access.perm("system:dept:edit")}>
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
        <Access key="remove" accessible={access.perm("system:dept:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<Dept>
        headerTitle={intl.formatMessage({ id: "menu.iam.dept", defaultMessage: "部门管理" })}
        rowKey="deptId"
        search={false}
        columns={columns}
        dataSource={depts}
        pagination={false}
        expandable={{
          expandedRowKeys: expandedKeys,
          onExpandedRowsChange: (keys) => setExpandedKeys(keys.map(Number)),
        }}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("system:dept:add")}>
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
      <DeptFormModal
        open={formOpen}
        record={editing}
        presetParent={presetParent}
        depts={depts}
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

export default DeptList;
