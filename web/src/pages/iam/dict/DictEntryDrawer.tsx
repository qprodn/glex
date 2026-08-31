import DictEntryFormModal from "./DictEntryFormModal";
import type { DictEntry, DictType } from "@/services/dict/v1";
import { services } from "@/services";
import { tagStyleLabel } from "@/hooks/useDict";
import { buildFilter } from "@/utils/aipFilter";
import { createTokenPagedRequest } from "@/utils/pager";
import { PlusOutlined } from "@ant-design/icons";
import { type ActionType, type ProColumns, ProTable } from "@ant-design/pro-components";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Drawer, Modal, Typography } from "antd";
import React, { useRef, useState } from "react";

type DictEntryDrawerProps = {
  dictType?: DictType | undefined;
  onClose: (changed: boolean) => void;
};

/**
 * Dict Entry management for one Dict Type: a drawer-hosted table of the
 * type's entries (admin list, so full rows incl. ids — not the lightweight
 * options read), with create/edit/delete behind the system:dict:* perms.
 */
const DictEntryDrawer: React.FC<DictEntryDrawerProps> = ({ dictType, onClose }) => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<DictEntry | undefined>();
  const [reloadFlag, setReloadFlag] = useState(0);

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<DictEntry>(async (pageSize, pageToken, params) => {
        const set = await services.dict.ListDictEntries({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "dict_type", value: dictType?.dictType },
            { field: "dict_label", value: String(params.dictLabel ?? "") || undefined },
          ]),
          // orderBy unset: the server default (dict_sort, id) keeps ties
          // from shuffling across pages.
          orderBy: undefined,
        });
        return { items: set.entries ?? [], nextPageToken: set.nextPageToken };
      }),
    // A fresh type row must re-bind the filter; reloadFlag forces the memo
    // to rebuild after writes so the table refetches cleanly.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [dictType?.dictType, reloadFlag],
  );

  const onDelete = (record: DictEntry) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.dict.entry.delete.confirm", defaultMessage: "确定删除字典项「{label}」吗？" },
        { label: record.dictLabel },
      ),
      onOk: async () => {
        await services.dict.DeleteDictEntry({ entryId: record.entryId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        setReloadFlag((n) => n + 1);
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<DictEntry>[] = [
    { title: "ID", dataIndex: "entryId", width: 90 },
    {
      title: intl.formatMessage({ id: "pages.dict.entry.label", defaultMessage: "标签" }),
      dataIndex: "dictLabel",
    },
    {
      title: intl.formatMessage({ id: "pages.dict.entry.value", defaultMessage: "键值" }),
      dataIndex: "dictValue",
      render: (_, record) => <Typography.Text code>{record.dictValue}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.dict.entry.sort", defaultMessage: "排序" }),
      dataIndex: "dictSort",
      width: 80,
    },
    {
      title: intl.formatMessage({ id: "pages.dict.entry.tagStyle", defaultMessage: "样式" }),
      dataIndex: "tagStyle",
      width: 100,
      render: (_, record) => (
        <Typography.Text>{record.tagStyle ? tagStyleLabel(record.tagStyle) : ""}</Typography.Text>
      ),
    },
    {
      title: intl.formatMessage({ id: "pages.dict.entry.isDefault", defaultMessage: "默认" }),
      dataIndex: "isDefault",
      width: 80,
      render: (_, record) => (record.isDefault ? "✓" : "-"),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 140,
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
        <Access key="remove" accessible={access.perm("system:dict:remove")}>
          <Typography.Link type="danger" onClick={() => onDelete(record)}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Typography.Link>
        </Access>,
      ],
    },
  ];

  return (
    <Drawer
      open={Boolean(dictType)}
      width={720}
      title={intl.formatMessage(
        { id: "pages.dict.entries.title", defaultMessage: "字典项 — {name}（{key}）" },
        { name: dictType?.dictName, key: dictType?.dictType },
      )}
      onClose={() => onClose(false)}
      destroyOnHidden
    >
      <ProTable<DictEntry>
        rowKey="entryId"
        actionRef={actionRef}
        search={false}
        options={false}
        columns={columns}
        request={pager}
        pagination={{ pageSize: 10 }}
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
      <DictEntryFormModal
        open={formOpen}
        dictTypeKey={dictType?.dictType}
        record={editing}
        onClose={(changed) => {
          setFormOpen(false);
          if (changed) {
            setReloadFlag((n) => n + 1);
            actionRef.current?.reload();
          }
        }}
      />
    </Drawer>
  );
};

export default DictEntryDrawer;
