import { services } from "@/services";
import { clearToken, LOGIN_PATH } from "@/utils/token";
import { LogoutOutlined, UserOutlined } from "@ant-design/icons";
import { history, useModel } from "@umijs/max";
import type { MenuProps } from "antd";
import { Spin } from "antd";
import { createStyles } from "antd-style";
import React from "react";
import HeaderDropdown from "../HeaderDropdown";

export type GlobalHeaderRightProps = {
  children?: React.ReactNode;
};

export const AvatarName = () => {
  const { initialState } = useModel("@@initialState");
  const { currentUser } = initialState || {};
  return <span className="anticon">{currentUser?.user?.nickName}</span>;
};

const useStyles = createStyles(({ token }) => {
  return {
    action: {
      display: "flex",
      height: "48px",
      marginLeft: "auto",
      overflow: "hidden",
      alignItems: "center",
      padding: "0 8px",
      cursor: "pointer",
      borderRadius: token.borderRadius,
      "&:hover": {
        backgroundColor: token.colorBgTextHover,
      },
    },
  };
});

export const AvatarDropdown: React.FC<GlobalHeaderRightProps> = ({
  children,
}) => {
  const { styles } = useStyles();
  const { initialState } = useModel("@@initialState");

  // Logout revokes this Login Session server-side, then drops the bearer
  // token. A full load re-runs render(), so the injected dynamic routes and
  // the session store are torn down with it.
  const loginOut = async () => {
    try {
      await services.auth.Logout({});
    } catch {
      // Idempotent server-side; a failure still logs the browser out.
    }
    clearToken();
    window.location.href = LOGIN_PATH;
  };

  const onMenuClick: MenuProps["onClick"] = (event) => {
    const { key } = event;
    if (key === "logout") {
      loginOut();
      return;
    }
    history.push(`/${key}`);
  };

  const loading = (
    <span className={styles.action}>
      <Spin size="small" style={{ marginLeft: 8, marginRight: 8 }} />
    </span>
  );

  if (!initialState) {
    return loading;
  }

  const { currentUser } = initialState;

  if (!currentUser?.user) {
    return loading;
  }

  const menuItems: MenuProps["items"] = [
    {
      key: "profile",
      icon: <UserOutlined />,
      label: "个人中心",
    },
    { type: "divider" },
    {
      key: "logout",
      icon: <LogoutOutlined />,
      label: "退出登录",
    },
  ];

  return (
    <HeaderDropdown
      menu={{
        selectedKeys: [],
        onClick: onMenuClick,
        items: menuItems,
      }}
    >
      {children}
    </HeaderDropdown>
  );
};
