import type { DeptTreeOption, Post } from "@/services/org/v1";
import { DictTag } from "@/components";
import { services } from "@/services";
import { buildFilter } from "@/utils/aipFilter";
import { buildDeptNames, toDeptTreeData } from "@/utils/deptTree";
import { createTokenPagedRequest } from "@/utils/pager";
import { PlusOutlined } from "@ant-design/icons";
import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Card, Modal, Tree, Typography } from "antd";
import React, { useRef, useState } from "react";
import PostFormModal from "./PostFormModal";

/**
 * 岗位管理: paged posts with the RuoYi left department tree — clicking a
 * node filters by that department's SUBTREE (the list's dept_id parameter
 * is subtree semantics, resolved server-side by the org module; ADR 0015 —
 * posts are department-scoped).
 */
const PostList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Post | undefined>();
  const [deptTree, setDeptTree] = useState<DeptTreeOption[]>([]);
  const [selectedDept, setSelectedDept] = useState<number | undefined>();

  React.useEffect(() => {
    services.dept.ListDeptTree({}).then((tree) => setDeptTree(tree.options ?? []));
  }, []);

  const deptNames = React.useMemo(() => buildDeptNames(deptTree), [deptTree]);

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<Post>(async (pageSize, pageToken, params) => {
        const set = await services.post.ListPosts({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "post_code", value: String(params.postCode ?? "") || undefined },
            { field: "post_name", value: String(params.postName ?? "") || undefined },
            {
              field: "status",
              value: params.status === undefined || params.status === null || params.status === ""
                ? undefined
                : Number(params.status),
            },
          ]),
          orderBy: undefined,
          // Subtree semantics: the clicked department plus every descendant.
          deptId: selectedDept ?? 0,
        });
        return { items: set.posts ?? [], nextPageToken: set.nextPageToken };
      }),
    [selectedDept],
  );

  const onDelete = (record: Post) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.post.delete.confirm", defaultMessage: "确定删除岗位「{name}」吗？" },
        { name: record.postName },
      ),
      content: intl.formatMessage({
        id: "pages.post.delete.hint",
        defaultMessage: "已分配用户的岗位会被服务端拒绝（POST_IN_USE）。",
      }),
      onOk: async () => {
        await services.post.DeletePost({ postId: record.postId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<Post>[] = [
    { title: "ID", dataIndex: "postId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.post.postCode", defaultMessage: "岗位编码" }),
      dataIndex: "postCode",
      render: (_, record) => <Typography.Text code>{record.postCode}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.post.postName", defaultMessage: "岗位名称" }),
      dataIndex: "postName",
    },
    {
      title: intl.formatMessage({ id: "pages.dept.belong", defaultMessage: "所属部门" }),
      dataIndex: "deptId",
      search: false,
      render: (_, record) => (record.deptId ? deptNames.get(record.deptId) ?? record.deptId : "-"),
    },
    {
      title: intl.formatMessage({ id: "pages.post.postSort", defaultMessage: "排序" }),
      dataIndex: "postSort",
      width: 72,
      search: false,
    },
    {
      title: intl.formatMessage({ id: "common.status", defaultMessage: "状态" }),
      dataIndex: "status",
      render: (_, record) => <DictTag typeKey="sys_normal_disable" value={record.status ?? 0} />,
    },
    {
      title: intl.formatMessage({ id: "common.remark", defaultMessage: "备注" }),
      dataIndex: "remark",
      search: false,
      ellipsis: true,
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
      width: 140,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("system:post:edit")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("system:post:remove")}>
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
        <ProTable<Post>
          headerTitle={intl.formatMessage({ id: "menu.iam.post", defaultMessage: "岗位管理" })}
          rowKey="postId"
          actionRef={actionRef}
          search={{ labelWidth: "auto" }}
          columns={columns}
          request={pager}
          toolBarRender={() => [
            <Access key="add" accessible={access.perm("system:post:add")}>
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
      </div>
      <PostFormModal
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
    </PageContainer>
  );
};

export default PostList;
