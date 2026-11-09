import { services, asServiceError } from "@/services";
import type { AdoptedTemplate, WechatApp, WechatCapability } from "@/services/wechat/v1";
import { Access, useAccess, useIntl } from "@umijs/max";
import { App, Button, Drawer, Form, Input, Select, Space, Table, Typography } from "antd";
import React, { useEffect, useState } from "react";

const CHANNEL_LABEL: Record<string, string> = {
  WECHAT_CAPABILITY_TEMPLATE_MESSAGE: "模板消息",
  WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION: "订阅通知",
  WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE: "订阅消息",
};

const channelsFor = (app: WechatApp): WechatCapability[] => {
  if (app.appType === "WECHAT_APP_TYPE_MINI_PROGRAM") {
    return ["WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE"];
  }
  if (app.accountKind === "WECHAT_ACCOUNT_KIND_SERVICE") {
    return ["WECHAT_CAPABILITY_TEMPLATE_MESSAGE", "WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION"];
  }
  return [];
};

type Props = {
  app?: WechatApp;
  onClose: () => void;
};

/**
 * 微信应用详情: adopt a public-library template and send one dispatch.
 * This screen does not invent templates and does not ask the person to
 * subscribe inside WeChat.
 */
const AppDetailDrawer: React.FC<Props> = ({ app, onClose }) => {
  const intl = useIntl();
  const access = useAccess();
  const { message } = App.useApp();
  const [templates, setTemplates] = useState<AdoptedTemplate[]>([]);
  const [adopting, setAdopting] = useState(false);
  const [sending, setSending] = useState(false);
  const [adoptForm] = Form.useForm();
  const [sendForm] = Form.useForm();

  const fail = (e: unknown) => message.error(asServiceError(e)?.message ?? String(e));

  const load = () => {
    if (!app?.appId) return;
    services.wechat
      .ListAdoptedTemplates({ appId: app.appId })
      .then((set) => setTemplates(set.templates ?? []))
      .catch(fail);
  };

  useEffect(() => {
    if (app?.appId && access.perm("wechat:dispatch:list")) {
      load();
    } else {
      setTemplates([]);
    }
    adoptForm.resetFields();
    sendForm.resetFields();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app?.appId]);

  const adopt = async () => {
    const values = await adoptForm.validateFields();
    setAdopting(true);
    try {
      await services.wechat.AdoptWechatTemplate({
        appId: app?.appId,
        libraryTemplateId: values.libraryTemplateId,
        channel: values.channel,
      });
      message.success(intl.formatMessage({ id: "pages.wechat.template.adopted", defaultMessage: "已选用" }));
      adoptForm.resetFields();
      load();
    } catch (e) {
      fail(e);
    } finally {
      setAdopting(false);
    }
  };

  const send = async () => {
    const values = await sendForm.validateFields();
    setSending(true);
    try {
      const created = await services.wechat.CreateWechatDispatch({
        appId: app?.appId,
        templateId: values.templateId,
        openid: values.openid,
        content: values.content,
        webUrl: values.webUrl || undefined,
        miniProgramAppId: values.miniProgramAppId || undefined,
        miniProgramPage: values.miniProgramPage || undefined,
      });
      message.success(
        intl.formatMessage(
          { id: "pages.wechat.dispatch.accepted", defaultMessage: "微信已受理，msgid {msgid}" },
          { msgid: created.msgid || "" },
        ),
      );
      sendForm.resetFields();
    } catch (e) {
      fail(e);
    } finally {
      setSending(false);
    }
  };

  const channelOptions = (app ? channelsFor(app) : []).map((value) => ({
    value,
    label: CHANNEL_LABEL[value],
  }));

  return (
    <Drawer
      title={app ? `${app.name} · 选用与发送` : ""}
      open={!!app}
      width={720}
      onClose={onClose}
      destroyOnClose
    >
      <Typography.Paragraph type="secondary">
        从微信公共模板库选用模板，再向一个 openid 发送。本页不自建模板，也不代替用户在微信里点订阅。
      </Typography.Paragraph>
      <Table<AdoptedTemplate>
        rowKey="templateId"
        size="small"
        pagination={false}
        dataSource={templates}
        columns={[
          { title: "公共模板", dataIndex: "libraryTemplateId" },
          { title: "名称", dataIndex: "title" },
          { title: "种类", dataIndex: "channel", render: (v) => CHANNEL_LABEL[v] ?? "" },
        ]}
      />
      <Access accessible={access.perm("wechat:dispatch:create")}>
        <Typography.Title level={5}>选用模板</Typography.Title>
        <Form form={adoptForm} layout="vertical">
          <Form.Item name="libraryTemplateId" label="公共模板 ID" rules={[{ required: true, message: "填写公共模板 ID" }]}>
            <Input />
          </Form.Item>
          <Form.Item name="channel" label="种类" rules={[{ required: true, message: "选择种类" }]}>
            <Select options={channelOptions} />
          </Form.Item>
          <Button type="primary" loading={adopting} onClick={adopt}>
            选用
          </Button>
        </Form>
        <Typography.Title level={5} style={{ marginTop: 24 }}>
          发送
        </Typography.Title>
        <Form form={sendForm} layout="vertical">
          <Form.Item name="templateId" label="已选用模板" rules={[{ required: true, message: "选择模板" }]}>
            <Select
              options={templates.map((row) => ({
                value: row.templateId,
                label: `${row.title || row.libraryTemplateId} · ${CHANNEL_LABEL[row.channel ?? ""] ?? ""}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="openid" label="openid" rules={[{ required: true, message: "填写 openid" }]}>
            <Input />
          </Form.Item>
          <Form.Item name="content" label="内容">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="webUrl" label="网页 URL">
            <Input />
          </Form.Item>
          <Form.Item name="miniProgramAppId" label="小程序 AppID">
            <Input />
          </Form.Item>
          <Form.Item name="miniProgramPage" label="小程序页面">
            <Input />
          </Form.Item>
          <Space>
            <Button type="primary" loading={sending} onClick={send}>
              发送
            </Button>
          </Space>
        </Form>
      </Access>
    </Drawer>
  );
};

export default AppDetailDrawer;
