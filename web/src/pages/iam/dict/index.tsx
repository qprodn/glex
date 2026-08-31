import DictEntryDrawer from "./DictEntryDrawer";
import DictTypeFormModal from "./DictTypeFormModal";
import type { DictType } from "@/services/dict/v1";
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

/**
 * 字典管理: Dict Types list. A type's Dict Entries are managed in the
 * entry drawer opened from the row actions — one route, drawer-based, like
 * the role module's Menu Grant drawer. The type key is immutable after
 * create (ADR 0006): the form disables it on edit, and the server ignores
 * it regardless.
 */
const DictTypeList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<DictType | undefined>();
  const [entriesType, setEntriesType] = useState<DictType | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<DictType>(async (pageSize, pageToken, params) => {
        const set = await services.dict.ListDictTypes({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "dict_name", value: String(params.dictName ?? "") || undefined },
            { field: "dict_type", value: String(params.dictType ?? "") || undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.dictTypes ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const onDelete = (record: DictType) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.dict.type.delete.confirm", defaultMessage: "确定删除字典「{name}」吗？" },
        { name: record.dictName },
      ),
      content: intl.formatMessage({
        id: "pages.dict.type.delete.hint",
        defaultMessage: "仍有字典项的类型会被服务端拒绝（DICT_TYPE_IN_USE）。",
      }),
      onOk: async () => {
        await services.dict.DeleteDictType({ dictTypeId: record.dictTypeId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<DictType>[] = [
    { title: "ID", dataIndex: "dictTypeId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.dict.type.dictName", defaultMessage: "字典名称" }),
      dataIndex: "dictName",
    },
    {
      title: intl.formatMessage({ id: "pages.dict.type.dictType", defaultMessage: "字典类型" }),
      dataIndex: "dictType",
      render: (_, record) => <Typography.Text code>{record.dictType}</Typography.Text>,
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
      width: 220,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("system:dict:edit")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="entries" accessible={access.perm("system:dict:list")}>
          <a onClick={() => setEntriesType(record)}>
            {intl.formatMessage({ id: "pages.dict.entries", defaultMessage: "字典项" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("system:dict:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<DictType>
        headerTitle={intl.formatMessage({ id: "menu.iam.dict", defaultMessage: "字典管理" })}
        rowKey="dictTypeId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("system:dict:add")}>
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
      <DictTypeFormModal
        open={formOpen}
        record={editing}
        onClose={(changed) => {
          setFormOpen(false);
          if (changed) {
            actionRef.current?.reload();
          }
        }}
      />
      <DictEntryDrawer
        dictType={entriesType}
        onClose={(changed) => {
          if (changed) {
            actionRef.current?.reload();
          }
          setEntriesType(undefined);
        }}
      />
    </PageContainer>
  );
};

export default DictTypeList;
