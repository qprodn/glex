import type { Menu, Role } from "@/services/access/v1";
import { services } from "@/services";
import { useIntl } from "@umijs/max";
import { App, Button, Drawer, Spin, Switch, Tree } from "antd";
import type { DataNode } from "antd/es/tree";
import React, { useEffect, useMemo, useState } from "react";

type MenuGrantDrawerProps = {
  role?: Role | undefined;
  onClose: (changed: boolean) => void;
};

const toTreeData = (menus: Menu[] | undefined): DataNode[] =>
  (menus ?? []).map((menu) => ({
    title: `${menu.menuName ?? ""}${menu.perms ? `（${menu.perms}）` : ""}`,
    key: menu.menuId ?? 0,
    children: menu.children?.length ? toTreeData(menu.children) : undefined,
  }));

/**
 * Menu Grant editor: the full menu tree (directories, menus, AND buttons —
 * buttons carry the permission handles) with the role's granted ids checked.
 * Saving is a full replacement in one transaction together with the
 * menu_check_strictly flag. antd Tree binds like RuoYi's el-tree:
 * `checkStrictly = !menu_check_strictly` — the flag on means parent/child
 * selection is linked (a granted child shows its parent half-checked and the
 * parent is not itself a grant); off means every node is ticked verbatim.
 * Only fully-checked keys are saved, so the stored grant stays
 * strictly-shaped.
 */
const MenuGrantDrawer: React.FC<MenuGrantDrawerProps> = ({ role, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [menus, setMenus] = useState<Menu[]>([]);
  const [checkedKeys, setCheckedKeys] = useState<number[]>([]);
  const [strictly, setStrictly] = useState(true);

  useEffect(() => {
    if (!role?.roleId) {
      return;
    }
    setLoading(true);
    Promise.all([
      services.menu.ListMenus({ filter: undefined, orderBy: undefined }),
      services.role.ListRoleMenus({ roleId: role.roleId }),
    ])
      .then(([tree, grant]) => {
        setMenus(tree.menus ?? []);
        setCheckedKeys(grant.menuIds ?? []);
        setStrictly(role.menuCheckStrictly ?? true);
      })
      .finally(() => setLoading(false));
  }, [role?.roleId, role?.menuCheckStrictly]);

  const treeData = useMemo(() => toTreeData(menus), [menus]);

  const onSave = async () => {
    if (!role?.roleId) {
      return;
    }
    setSaving(true);
    try {
      await services.role.UpdateRoleMenus({
        roleId: role.roleId,
        menuIds: checkedKeys,
        menuCheckStrictly: strictly,
      });
      message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
      onClose(true);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Drawer
      open={Boolean(role)}
      width={480}
      onClose={() => onClose(false)}
      title={intl.formatMessage(
        { id: "pages.role.menuGrant.title", defaultMessage: "菜单授权 — {name}" },
        { name: role?.roleName },
      )}
      extra={
        <Button type="primary" loading={saving} onClick={onSave}>
          {intl.formatMessage({ id: "common.save", defaultMessage: "保存" })}
        </Button>
      }
    >
      {loading ? (
        <Spin />
      ) : (
        <>
          <div style={{ marginBottom: 12, display: "flex", gap: 8, alignItems: "center" }}>
            <span>
              {intl.formatMessage({
                id: "pages.role.menuGrant.strictly",
                defaultMessage: "父子联动",
              })}
            </span>
            <Switch
              checked={strictly}
              onChange={(checked) => setStrictly(checked)}
              checkedChildren={intl.formatMessage({ id: "common.on", defaultMessage: "开" })}
              unCheckedChildren={intl.formatMessage({ id: "common.off", defaultMessage: "关" })}
            />
            <span style={{ color: "rgba(0,0,0,0.45)" }}>
              {intl.formatMessage({
                id: "pages.role.menuGrant.strictlyHint",
                defaultMessage: "开 = 勾选子菜单时父菜单不自动入授权",
              })}
            </span>
          </div>
          <div style={{ marginBottom: 12, color: "rgba(0,0,0,0.45)" }}>
            {intl.formatMessage({
              id: "pages.role.menuGrant.hint",
              defaultMessage: "全量替换：清空勾选即清空该角色的菜单授权；保存后需相关用户重新登录生效。",
            })}
          </div>
          <Tree
            key={role?.roleId ?? "grant"}
            checkable
            blockNode
            defaultExpandAll
            treeData={treeData}
            checkStrictly={!strictly}
            checkedKeys={checkedKeys}
            onCheck={(keys) =>
              setCheckedKeys(
                (Array.isArray(keys) ? keys : keys.checked).map(Number),
              )
            }
          />
        </>
      )}
    </Drawer>
  );
};

export default MenuGrantDrawer;
