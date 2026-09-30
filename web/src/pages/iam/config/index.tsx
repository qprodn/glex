import ConfigFormModal from "./ConfigFormModal";
import type { Config } from "@/services/param/v1";
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
import { App, Button, Modal, Tag, Typography } from "antd";
import React, { useRef, useState } from "react";

/**
 * 参数管理: Runtime Parameters list. Unlike a Dict Type key, the parameter
 * key stays editable after create (docs/modules/param.md); built-in rows
 * (seeded keys the system assumes exist) may be re-valued but never deleted —
 * the delete action is simply absent for them.
 */
const ConfigList: React.FC = () => {
  const actionRef = useRef<ActionType>(undefined);
  const access = useAccess();
  const intl = useIntl();
  const { message } = App.useApp();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Config | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<Config>(async (pageSize, pageToken, params) => {
        const set = await services.config.ListConfigs({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "config_name", value: String(params.configName ?? "") || undefined },
            { field: "config_key", value: String(params.configKey ?? "") || undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.configs ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const onDelete = (record: Config) => {
    Modal.confirm({
      title: intl.formatMessage(
        { id: "pages.config.delete.confirm", defaultMessage: "确定删除参数「{name}」吗？" },
        { name: record.configName },
      ),
      content: intl.formatMessage({
        id: "pages.config.delete.hint",
        defaultMessage: "删除后按此键引用的消费方将读到 not-found；内置参数不可删除。",
      }),
      onOk: async () => {
        await services.config.DeleteConfig({ configId: record.configId });
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      },
    });
  };

  const columns: ProColumns<Config>[] = [
    { title: "ID", dataIndex: "configId", width: 90, search: false },
    {
      title: intl.formatMessage({ id: "pages.config.name", defaultMessage: "参数名称" }),
      dataIndex: "configName",
    },
    {
      title: intl.formatMessage({ id: "pages.config.key", defaultMessage: "参数键名" }),
      dataIndex: "configKey",
      render: (_, record) => <Typography.Text code>{record.configKey}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.config.value", defaultMessage: "参数键值" }),
      dataIndex: "configValue",
      search: false,
      ellipsis: true,
    },
    {
      title: intl.formatMessage({ id: "pages.config.builtIn", defaultMessage: "内置" }),
      dataIndex: "builtIn",
      search: false,
      width: 80,
      render: (_, record) =>
        record.builtIn ? (
          <Tag color="blue">
            {intl.formatMessage({ id: "pages.config.builtIn.yes", defaultMessage: "内置" })}
          </Tag>
        ) : (
          <Tag>
            {intl.formatMessage({ id: "pages.config.builtIn.no", defaultMessage: "普通" })}
          </Tag>
        ),
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
      width: 160,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("system:config:edit")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        // Built-in rows cannot be withdrawn — the action is absent, not
        // merely disabled, so the server's CONFIG_BUILT_IN_LOCKED stays a
        // backstop rather than the UI contract.
        !record.builtIn && (
          <Access key="remove" accessible={access.perm("system:config:remove")}>
            <Typography.Link type="danger" onClick={() => onDelete(record)}>
              {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
            </Typography.Link>
          </Access>
        ),
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<Config>
        headerTitle={intl.formatMessage({ id: "menu.iam.config", defaultMessage: "参数管理" })}
        rowKey="configId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("system:config:add")}>
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
      <ConfigFormModal
        open={formOpen}
        record={editing}
        onClose={(changed) => {
          setFormOpen(false);
          if (changed) {
            actionRef.current?.reload();
          }
        }}
      />
    </PageContainer>
  );
};

export default ConfigList;
