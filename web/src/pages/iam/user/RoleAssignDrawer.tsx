import type { Role, UserRoles } from "@/services/access/v1";
import type { User } from "@/services/user/v1";
import { services } from "@/services";
import { useIntl } from "@umijs/max";
import { App, Button, Checkbox, Drawer, Spin, Tag } from "antd";
import React, { useEffect, useState } from "react";

type RoleAssignDrawerProps = {
  user?: User | undefined;
  onClose: (changed: boolean) => void;
};

/**
 * Role Assignment editor (RuoYi authRole dialog as a drawer): ListUserRoles
 * returns every active role with an assigned flag; saving is a full
 * replacement — unchecking everything clears the assignment.
 */
const RoleAssignDrawer: React.FC<RoleAssignDrawerProps> = ({ user, onClose }) => {
  const intl = useIntl();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [roles, setRoles] = useState<UserRoles["items"]>([]);
  const [checked, setChecked] = useState<number[]>([]);

  useEffect(() => {
    if (!user?.userId) {
      return;
    }
    setLoading(true);
    services.assignment
      .ListUserRoles({ userId: user.userId })
      .then((res) => {
        setRoles(res.items ?? []);
        setChecked(
          (res.items ?? []).filter((item) => item.assigned).map((item) => item.role?.roleId ?? 0),
        );
      })
      .finally(() => setLoading(false));
  }, [user?.userId]);

  const onSave = async () => {
    if (!user?.userId) {
      return;
    }
    setSaving(true);
    try {
      await services.assignment.UpdateUserRoles({
        userId: user.userId,
        roleIds: checked,
      });
      message.success(intl.formatMessage({ id: "common.saved", defaultMessage: "保存成功" }));
      onClose(true);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Drawer
      open={Boolean(user)}
      width={480}
      onClose={() => onClose(false)}
      title={intl.formatMessage(
        { id: "pages.user.assignRoles.title", defaultMessage: "分配角色 — {name}" },
        { name: user?.nickName ?? user?.userName },
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
          <div style={{ marginBottom: 12, color: "rgba(0,0,0,0.45)" }}>
            {intl.formatMessage({
              id: "pages.user.assignRoles.hint",
              defaultMessage: "全量替换：取消全部勾选将清空该用户的角色；保存后立即生效。",
            })}
          </div>
          <Checkbox.Group
            style={{ display: "flex", flexDirection: "column", gap: 8 }}
            value={checked}
            onChange={(values) => setChecked(values as number[])}
          >
            {(roles ?? []).map((item) => {
              const role: Role | undefined = item.role;
              return (
                <Checkbox key={role?.roleId} value={role?.roleId}>
                  {role?.roleName}
                  <Tag style={{ marginLeft: 8 }}>{role?.roleKey}</Tag>
                  {role?.status === 1 && (
                    <Tag color="error">
                      {intl.formatMessage({ id: "common.status.disabled", defaultMessage: "停用" })}
                    </Tag>
                  )}
                </Checkbox>
              );
            })}
          </Checkbox.Group>
        </>
      )}
    </Drawer>
  );
};

export default RoleAssignDrawer;
