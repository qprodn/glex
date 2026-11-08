import { services, asServiceError } from "@/services";
import type { WechatApp, WechatAppMenu, WechatMenuButton } from "@/services/wechat/v1";
import { useIntl } from "@umijs/max";
import { App, Button, Drawer, Input, Radio, Space, Typography } from "antd";
import React, { useEffect, useState } from "react";

type ButtonKind = "web" | "mini" | "parent";

type DraftButton = {
  key: string;
  name: string;
  kind: ButtonKind;
  webUrl: string;
  miniProgramAppId: string;
  miniProgramPage: string;
  subButtons: DraftButton[];
};

type MenuDrawerProps = {
  app?: WechatApp;
  onClose: () => void;
};

let draftKey = 0;
const nextKey = () => {
  draftKey += 1;
  return `menu-${draftKey}`;
};

const blankLeaf = (): DraftButton => ({
  key: nextKey(),
  name: "",
  kind: "web",
  webUrl: "",
  miniProgramAppId: "",
  miniProgramPage: "",
  subButtons: [],
});

const fromButton = (button: WechatMenuButton): DraftButton => {
  const subs = (button.subButtons ?? []).map(fromButton);
  if (subs.length > 0) {
    return {
      key: nextKey(),
      name: button.name ?? "",
      kind: "parent",
      webUrl: "",
      miniProgramAppId: "",
      miniProgramPage: "",
      subButtons: subs,
    };
  }
  if (button.miniProgramAppId) {
    return {
      key: nextKey(),
      name: button.name ?? "",
      kind: "mini",
      webUrl: "",
      miniProgramAppId: button.miniProgramAppId,
      miniProgramPage: button.miniProgramPage ?? "",
      subButtons: [],
    };
  }
  return {
    key: nextKey(),
    name: button.name ?? "",
    kind: "web",
    webUrl: button.webUrl ?? "",
    miniProgramAppId: "",
    miniProgramPage: "",
    subButtons: [],
  };
};

const toButton = (button: DraftButton): WechatMenuButton => {
  if (button.kind === "parent") {
    return { name: button.name, subButtons: button.subButtons.map(toButton) };
  }
  if (button.kind === "mini") {
    return {
      name: button.name,
      miniProgramAppId: button.miniProgramAppId,
      miniProgramPage: button.miniProgramPage,
    };
  }
  return { name: button.name, webUrl: button.webUrl };
};

const sameDraft = (left: DraftButton[], right: DraftButton[]) =>
  JSON.stringify(left.map(toButton)) === JSON.stringify(right.map(toButton));

const ButtonFields: React.FC<{
  button: DraftButton;
  onChange: (next: DraftButton) => void;
  onRemove: () => void;
}> = ({ button, onChange, onRemove }) => {
  const intl = useIntl();
  return (
    <div style={{ border: "1px solid rgba(0,0,0,0.06)", borderRadius: 8, padding: 12, marginBottom: 8 }}>
      <Space direction="vertical" style={{ width: "100%" }} size={8}>
        <Input
          value={button.name}
          placeholder={intl.formatMessage({ id: "pages.wechat.menu.name", defaultMessage: "按钮名称" })}
          onChange={(event) => onChange({ ...button, name: event.target.value })}
        />
        {button.kind === "parent" ? null : (
          <>
            <Radio.Group
              value={button.kind}
              onChange={(event) => onChange({ ...button, kind: event.target.value as ButtonKind })}
            >
              <Radio value="web">
                {intl.formatMessage({ id: "pages.wechat.menu.web", defaultMessage: "打开网页" })}
              </Radio>
              <Radio value="mini">
                {intl.formatMessage({ id: "pages.wechat.menu.mini", defaultMessage: "跳转小程序" })}
              </Radio>
            </Radio.Group>
            {button.kind === "web" ? (
              <Input
                value={button.webUrl}
                placeholder="https://"
                onChange={(event) => onChange({ ...button, webUrl: event.target.value })}
              />
            ) : (
              <>
                <Input
                  value={button.miniProgramAppId}
                  placeholder={intl.formatMessage({
                    id: "pages.wechat.menu.miniAppId",
                    defaultMessage: "小程序 AppID",
                  })}
                  onChange={(event) => onChange({ ...button, miniProgramAppId: event.target.value })}
                />
                <Input
                  value={button.miniProgramPage}
                  placeholder={intl.formatMessage({
                    id: "pages.wechat.menu.miniPage",
                    defaultMessage: "小程序页面",
                  })}
                  onChange={(event) => onChange({ ...button, miniProgramPage: event.target.value })}
                />
              </>
            )}
          </>
        )}
        {button.subButtons.map((child, index) => (
          <ButtonFields
            key={child.key}
            button={child}
            onChange={(next) => {
              const subButtons = button.subButtons.slice();
              subButtons[index] = next;
              onChange({ ...button, subButtons });
            }}
            onRemove={() => {
              const subButtons = button.subButtons.filter((_, i) => i !== index);
              onChange({
                ...button,
                kind: subButtons.length > 0 ? "parent" : "web",
                subButtons,
              });
            }}
          />
        ))}
        <Space>
          <Button
            size="small"
            onClick={() =>
              onChange({
                ...button,
                kind: "parent",
                webUrl: "",
                miniProgramAppId: "",
                miniProgramPage: "",
                subButtons: [...button.subButtons, blankLeaf()],
              })
            }
          >
            {intl.formatMessage({ id: "pages.wechat.menu.addChild", defaultMessage: "添加子按钮" })}
          </Button>
          <Button size="small" danger onClick={onRemove}>
            {intl.formatMessage({ id: "common.delete", defaultMessage: "删除" })}
          </Button>
        </Space>
      </Space>
    </div>
  );
};

const AcceptedMenu: React.FC<{ buttons: WechatMenuButton[] | undefined }> = ({ buttons }) => {
  if (!buttons || buttons.length === 0) {
    return <Typography.Text type="secondary">微信还没有接受过菜单</Typography.Text>;
  }
  return (
    <ul style={{ margin: 0, paddingLeft: 18 }}>
      {buttons.map((button, index) => (
        <li key={`${button.name ?? ""}-${index}`}>
          {button.name}
          {button.webUrl ? ` · ${button.webUrl}` : ""}
          {button.miniProgramAppId ? ` · ${button.miniProgramAppId} ${button.miniProgramPage ?? ""}` : ""}
          {button.subButtons && button.subButtons.length > 0 ? <AcceptedMenu buttons={button.subButtons} /> : null}
        </li>
      ))}
    </ul>
  );
};

/**
 * Edit and publish one Service Account's Custom Menu. Saving stays in
 * this system. Publishing replaces the whole default menu at WeChat.
 */
const MenuDrawer: React.FC<MenuDrawerProps> = ({ app, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const [buttons, setButtons] = useState<DraftButton[]>([]);
  const [saved, setSaved] = useState<DraftButton[]>([]);
  const [accepted, setAccepted] = useState<WechatMenuButton[] | undefined>();
  const [loading, setLoading] = useState(false);

  const apply = (view: WechatAppMenu) => {
    const next = (view.saved?.buttons ?? []).map(fromButton);
    setButtons(next);
    setSaved(next);
    setAccepted(view.accepted?.buttons);
  };

  useEffect(() => {
    if (!app?.appId) return;
    let live = true;
    setLoading(true);
    services.wechat
      .GetWechatAppMenu({ appId: app.appId })
      .then((view) => {
        if (live) apply(view);
      })
      .catch((error: unknown) => message.error(asServiceError(error)?.message ?? String(error)))
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [app?.appId]);

  const persist = async () => {
    if (!app?.appId) return undefined;
    const view = await services.wechat.SaveWechatAppMenu({
      appId: app.appId,
      menu: { buttons: buttons.map(toButton) },
    });
    apply(view);
    return view;
  };

  const save = async () => {
    try {
      await persist();
      message.success(intl.formatMessage({ id: "pages.wechat.menu.saved", defaultMessage: "菜单已保存在本系统" }));
    } catch (error) {
      message.error(asServiceError(error)?.message ?? String(error));
    }
  };

  const publish = async () => {
    if (!app?.appId) return;
    try {
      if (!sameDraft(buttons, saved)) {
        await persist();
      }
      const view = await services.wechat.PublishWechatAppMenu({ appId: app.appId });
      apply(view);
      message.success(
        intl.formatMessage({ id: "pages.wechat.menu.published", defaultMessage: "已整份替换微信上的默认菜单" }),
      );
    } catch (error) {
      message.error(asServiceError(error)?.message ?? String(error));
    }
  };

  return (
    <Drawer
      title={intl.formatMessage({ id: "pages.wechat.menu.title", defaultMessage: "自定义菜单" })}
      width={640}
      open={Boolean(app)}
      destroyOnHidden
      onClose={onClose}
      extra={
        <Space>
          <Button onClick={save} loading={loading}>
            {intl.formatMessage({ id: "pages.wechat.menu.save", defaultMessage: "保存" })}
          </Button>
          <Button type="primary" onClick={publish} loading={loading}>
            {intl.formatMessage({ id: "pages.wechat.menu.publish", defaultMessage: "发布" })}
          </Button>
        </Space>
      }
    >
      <Typography.Paragraph type="secondary">
        {app?.name}{" "}
        {intl.formatMessage({
          id: "pages.wechat.menu.hint",
          defaultMessage: "保存在本系统，不会调用微信。发布才用这份已保存的菜单整份替换该服务号的默认菜单。",
        })}
      </Typography.Paragraph>
      {buttons.map((button, index) => (
        <ButtonFields
          key={button.key}
          button={button}
          onChange={(next) => {
            const copy = buttons.slice();
            copy[index] = next;
            setButtons(copy);
          }}
          onRemove={() => setButtons(buttons.filter((_, i) => i !== index))}
        />
      ))}
      <Button onClick={() => setButtons([...buttons, blankLeaf()])} style={{ marginBottom: 16 }}>
        {intl.formatMessage({ id: "pages.wechat.menu.add", defaultMessage: "添加一级按钮" })}
      </Button>
      <Typography.Title level={5}>
        {intl.formatMessage({ id: "pages.wechat.menu.accepted", defaultMessage: "微信已接受的菜单" })}
      </Typography.Title>
      <AcceptedMenu buttons={accepted} />
    </Drawer>
  );
};

export default MenuDrawer;
