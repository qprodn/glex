import type { Menu } from "@/services/access/v1";
import { services } from "@/services";
import { registryKeys } from "@/router/registry";
import { collectUpdateMask } from "@/utils/fieldMask";
import {
  ModalForm,
  ProFormDependency,
  ProFormDigit,
  ProFormRadio,
  ProFormSelect,
  ProFormText,
  ProFormTreeSelect,
} from "@ant-design/pro-components";
import { useIntl } from "@umijs/max";
import { App } from "antd";
import React from "react";

type MenuFormModalProps = {
  open: boolean;
  record?: Menu | undefined;
  /** Preselect this parent when opened via 新增子项. */
  presetParent?: number | undefined;
  menus: Menu[];
  onClose: (changed: boolean) => void;
};

const DIRECTORY = "MENU_TYPE_DIRECTORY";
const MENU = "MENU_TYPE_MENU";
const BUTTON = "MENU_TYPE_BUTTON";

const EDITABLE_FIELDS = [
  "parentId",
  "menuName",
  "menuType",
  "orderNum",
  "icon",
  "path",
  "component",
  "queryParam",
  "isFrame",
  "isCache",
  "visible",
  "status",
  "perms",
] as const;

type ParentOption = { title: string; value: number; children?: ParentOption[] };

const toParentTreeData = (menus: Menu[] | undefined): ParentOption[] =>
  (menus ?? [])
    .filter((menu) => menu.menuType !== BUTTON)
    .map((menu) => ({
      title: menu.menuName ?? "",
      value: menu.menuId ?? 0,
      children: menu.children?.length ? toParentTreeData(menu.children) : undefined,
    }));

/**
 * Menu node editor. Fields are type-dependent (RuoYi semantics): a Directory
 * owns routing fields, a Menu additionally owns a registry component key,
 * a Button owns only its permission handle. The component dropdown offers
 * exactly the frontend registry keys (ADR 0019) — any other value would
 * render the placeholder page.
 */
const MenuFormModal: React.FC<MenuFormModalProps> = ({
  open,
  record,
  presetParent,
  menus,
  onClose,
}) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const isEdit = Boolean(record);

  return (
    <ModalForm<Menu>
      title={intl.formatMessage(
        isEdit
          ? { id: "pages.menu.edit", defaultMessage: "编辑菜单" }
          : { id: "pages.menu.new", defaultMessage: "新建菜单" },
      )}
      width={560}
      open={open}
      initialValues={
        isEdit
          ? {
              parentId: record?.parentId ?? 0,
              menuType: record?.menuType ?? DIRECTORY,
              menuName: record?.menuName,
              orderNum: record?.orderNum ?? 0,
              icon: record?.icon,
              path: record?.path,
              component: record?.component,
              queryParam: record?.queryParam,
              isFrame: record?.isFrame ?? 0,
              isCache: record?.isCache ?? 0,
              visible: record?.visible ?? 0,
              status: record?.status ?? 0,
              perms: record?.perms,
            }
          : {
              parentId: presetParent ?? 0,
              menuType: DIRECTORY,
              orderNum: 0,
              isFrame: 0,
              isCache: 0,
              visible: 0,
              status: 0,
            }
      }
      modalProps={{ destroyOnHidden: true, onCancel: () => onClose(false) }}
      onFinish={async (values) => {
        const payload: Menu = { ...values, menuId: record?.menuId };
        if (!isEdit) {
          await services.menu.CreateMenu({ menu: payload });
          message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
          onClose(true);
          return true;
        }
        const mask = collectUpdateMask(record, values, EDITABLE_FIELDS);
        if (!mask) {
          message.info(intl.formatMessage({ id: "common.noChange", defaultMessage: "没有变更" }));
          onClose(false);
          return true;
        }
        await services.menu.UpdateMenu({ menu: payload, updateMask: mask });
        message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
        onClose(true);
        return true;
      }}
    >
      <ProFormRadio.Group
        name="menuType"
        label={intl.formatMessage({ id: "pages.menu.menuType", defaultMessage: "类型" })}
        options={[
          { value: DIRECTORY, label: intl.formatMessage({ id: "pages.menu.type.directory", defaultMessage: "目录" }) },
          { value: MENU, label: intl.formatMessage({ id: "pages.menu.type.menu", defaultMessage: "菜单" }) },
          { value: BUTTON, label: intl.formatMessage({ id: "pages.menu.type.button", defaultMessage: "按钮" }) },
        ]}
      />
      <ProFormTreeSelect
        name="parentId"
        label={intl.formatMessage({ id: "pages.menu.parent", defaultMessage: "上级菜单" })}
        allowClear={false}
        fieldProps={{
          treeDefaultExpandAll: true,
          treeData: [
            {
              title: intl.formatMessage({ id: "pages.menu.root", defaultMessage: "根目录" }),
              value: 0,
              children: toParentTreeData(menus),
            },
          ],
        }}
      />
      <ProFormText
        name="menuName"
        label={intl.formatMessage({ id: "pages.menu.menuName", defaultMessage: "菜单名" })}
        rules={[{ required: true }]}
      />
      <ProFormDigit
        name="orderNum"
        label={intl.formatMessage({ id: "pages.menu.orderNum", defaultMessage: "排序" })}
        fieldProps={{ precision: 0 }}
      />
      <ProFormText
        name="icon"
        label={intl.formatMessage({ id: "pages.menu.icon", defaultMessage: "图标" })}
        placeholder="user / peoples / monitor"
      />
      <ProFormDependency name={["menuType"]}>
        {({ menuType }) =>
          menuType === BUTTON ? (
            <ProFormText
              name="perms"
              label={intl.formatMessage({ id: "pages.menu.perms", defaultMessage: "权限句柄" })}
              placeholder="system:user:add"
              rules={[{ required: true }]}
            />
          ) : (
            <>
              <ProFormText
                name="path"
                label={intl.formatMessage({ id: "pages.menu.path", defaultMessage: "路由地址" })}
                placeholder={menuType === MENU ? "user" : "system"}
                rules={[{ required: true }]}
              />
              <ProFormRadio.Group
                name="isFrame"
                label={intl.formatMessage({ id: "pages.menu.isFrame", defaultMessage: "外链" })}
                options={[
                  { value: 0, label: intl.formatMessage({ id: "common.no", defaultMessage: "否" }) },
                  { value: 1, label: intl.formatMessage({ id: "common.yes", defaultMessage: "是" }) },
                ]}
              />
              {menuType === MENU && (
                <>
                  <ProFormSelect
                    name="component"
                    label={intl.formatMessage({ id: "pages.menu.component", defaultMessage: "组件" })}
                    placeholder="iam/user/index"
                    options={registryKeys.map((key) => ({ value: key, label: key }))}
                    extra={intl.formatMessage({
                      id: "pages.menu.component.hint",
                      defaultMessage: "必须是前端组件注册表的键；其他值将渲染占位页。",
                    })}
                    rules={[{ required: true }]}
                  />
                  <ProFormText
                    name="perms"
                    label={intl.formatMessage({ id: "pages.menu.perms", defaultMessage: "权限句柄" })}
                    placeholder="system:user:list"
                  />
                  <ProFormText
                    name="queryParam"
                    label={intl.formatMessage({ id: "pages.menu.queryParam", defaultMessage: "路由参数" })}
                  />
                  <ProFormRadio.Group
                    name="isCache"
                    label={intl.formatMessage({ id: "pages.menu.isCache", defaultMessage: "缓存" })}
                    options={[
                      { value: 0, label: intl.formatMessage({ id: "common.no", defaultMessage: "否" }) },
                      { value: 1, label: intl.formatMessage({ id: "common.yes", defaultMessage: "是" }) },
                    ]}
                  />
                </>
              )}
            </>
          )
        }
      </ProFormDependency>
      <ProFormRadio.Group
        name="visible"
        label={intl.formatMessage({ id: "pages.menu.visible", defaultMessage: "显示状态" })}
        options={[
          { value: 0, label: intl.formatMessage({ id: "common.show", defaultMessage: "显示" }) },
          { value: 1, label: intl.formatMessage({ id: "common.hide", defaultMessage: "隐藏" }) },
        ]}
      />
      <ProFormRadio.Group
        name="status"
        label={intl.formatMessage({ id: "common.status", defaultMessage: "状态" })}
        options={[
          { value: 0, label: intl.formatMessage({ id: "common.status.normal", defaultMessage: "正常" }) },
          { value: 1, label: intl.formatMessage({ id: "common.status.disabled", defaultMessage: "停用" }) },
        ]}
      />
    </ModalForm>
  );
};

export default MenuFormModal;
