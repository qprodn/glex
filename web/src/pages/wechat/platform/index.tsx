import { services, asServiceError } from "@/services";
import type { WechatPlatform } from "@/services/wechat/v1";
import { PageContainer } from "@ant-design/pro-components";
import { Access, useIntl, useAccess } from "@umijs/max";
import { App, Button, Popconfirm, Typography } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import React, { useRef, useState } from "react";
import type { ActionType, ProColumns } from "@ant-design/pro-components";
import { ProTable } from "@ant-design/pro-components";
import { createTokenPagedRequest } from "@/utils/pager";
import { buildFilter } from "@/utils/aipFilter";
import PlatformFormModal from "./PlatformFormModal";

/**
 * 微信平台: the list renders masks only. The secrets the operator typed
 * are never shown again (ADR 0029).
 */
const PlatformList: React.FC = () => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const actionRef = useRef<ActionType>(undefined);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<WechatPlatform | undefined>();

  const pager = React.useMemo(
    () =>
      createTokenPagedRequest<WechatPlatform>(async (pageSize, pageToken, params) => {
        const set = await services.wechat.ListWechatPlatforms({
          pageSize,
          pageToken: pageToken || undefined,
          filter: buildFilter([
            { field: "name", value: String(params.name ?? "") || undefined },
            { field: "component_app_id", value: String(params.componentAppId ?? "") || undefined },
          ]),
          orderBy: undefined,
        });
        return { items: set.platforms ?? [], nextPageToken: set.nextPageToken };
      }),
    [],
  );

  const withdraw = (record: WechatPlatform) => {
    services.wechat
      .WithdrawWechatPlatform({ platformId: record.platformId })
      .then(() => {
        message.success(intl.formatMessage({ id: "common.deleted", defaultMessage: "删除成功" }));
        actionRef.current?.reload();
      })
      .catch(async (e) => message.error(asServiceError(e)?.message ?? String(e)));
  };

  const columns: ProColumns<WechatPlatform>[] = [
    {
      title: "ID",
      dataIndex: "platformId",
      width: 180,
      search: false,
      render: (_, r) => <Typography.Text copyable>{String(r.platformId)}</Typography.Text>,
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.platform.name", defaultMessage: "显示名称" }),
      dataIndex: "name",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.platform.appId", defaultMessage: "component AppID" }),
      dataIndex: "componentAppId",
    },
    {
      title: intl.formatMessage({ id: "pages.wechat.platform.secret", defaultMessage: "component secret" }),
      dataIndex: "componentSecretMask",
      search: false,
    },
    {
      title: "Token",
      dataIndex: "tokenMask",
      search: false,
    },
    {
      title: "EncodingAESKey",
      dataIndex: "encodingAesKeyMask",
      search: false,
    },
    {
      title: intl.formatMessage({ id: "common.createTime", defaultMessage: "创建时间" }),
      dataIndex: "createdAt",
      width: 170,
      search: false,
      render: (_, r) => (r.createdAt ? new Date(r.createdAt).toLocaleString() : ""),
    },
    {
      title: intl.formatMessage({ id: "common.option", defaultMessage: "操作" }),
      valueType: "option",
      width: 140,
      render: (_, record) => [
        <Access key="edit" accessible={access.perm("wechat:platform:update")}>
          <a
            onClick={() => {
              setEditing(record);
              setFormOpen(true);
            }}
          >
            {intl.formatMessage({ id: "common.edit", defaultMessage: "编辑" })}
          </a>
        </Access>,
        <Access key="remove" accessible={access.perm("wechat:platform:remove")}>
          <Popconfirm
            title={intl.formatMessage({
              id: "pages.wechat.platform.withdraw.confirm",
              defaultMessage: "撤回后该平台离开列表，确定撤回？",
            })}
            onConfirm={() => withdraw(record)}
          >
            <Typography.Link type="danger">
              {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
            </Typography.Link>
          </Popconfirm>
        </Access>,
      ],
    },
  ];

  return (
    <PageContainer>
      <ProTable<WechatPlatform>
        headerTitle={intl.formatMessage({ id: "menu.wechat.platform", defaultMessage: "微信平台" })}
        rowKey="platformId"
        actionRef={actionRef}
        search={{ labelWidth: "auto" }}
        columns={columns}
        request={pager}
        toolBarRender={() => [
          <Access key="add" accessible={access.perm("wechat:platform:create")}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditing(undefined);
                setFormOpen(true);
              }}
            >
              {intl.formatMessage({ id: "pages.wechat.platform.new", defaultMessage: "登记平台" })}
            </Button>
          </Access>,
        ]}
      />
      <PlatformFormModal
        open={formOpen}
        record={editing}
        onClose={(changed) => {
          setFormOpen(false);
          setEditing(undefined);
          if (changed) actionRef.current?.reload();
        }}
      />
    </PageContainer>
  );
};

export default PlatformList;
