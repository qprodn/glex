import { Footer } from "@/components";
import type { LoginRequest } from "@/services/authn/v1";
import { services } from "@/services";
import { setToken } from "@/utils/token";
import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { LoginForm, ProFormText } from "@ant-design/pro-components";
import { FormattedMessage, Helmet, SelectLang, useIntl } from "@umijs/max";
import { App } from "antd";
import { createStyles } from "antd-style";
import React from "react";
import Settings from "../../../../config/defaultSettings";

const useStyles = createStyles(({ token }) => {
  return {
    lang: {
      width: 42,
      height: 42,
      lineHeight: "42px",
      position: "fixed",
      right: 16,
      borderRadius: token.borderRadius,
      ":hover": {
        backgroundColor: token.colorBgTextHover,
      },
    },
    container: {
      display: "flex",
      flexDirection: "column",
      height: "100vh",
      overflow: "auto",
      backgroundImage:
        "linear-gradient(135deg, #1677ff 0%, #36cfc9 100%)",
      backgroundSize: "100% 100%",
    },
  };
});

const Lang = () => {
  const { styles } = useStyles();

  return (
    <div className={styles.lang} data-lang>
      {SelectLang && <SelectLang />}
    </div>
  );
};

const Login: React.FC = () => {
  const { styles } = useStyles();
  const { message } = App.useApp();
  const intl = useIntl();

  const handleSubmit = async (req: LoginRequest) => {
    try {
      const reply = await services.auth.Login(req);
      if (!reply.accessToken) {
        return;
      }
      // The bearer credential is handed out exactly once (ADR 0018); keep it
      // and do a full load so render() fetches getInfo + getRouters and the
      // dynamic routes are injected before the first authenticated paint.
      setToken(reply.accessToken);
      message.success(
        intl.formatMessage({ id: "pages.login.success", defaultMessage: "登录成功！" }),
      );
      const redirect = new URL(window.location.href).searchParams.get("redirect");
      // Only same-origin paths; never bounce to an absolute URL.
      const safe = redirect?.startsWith("/") && !redirect.startsWith("//") ? redirect : "/";
      window.location.href = safe;
    } catch {
      // Reporting the failure is handled globally in requestErrorConfig, which
      // maps `reason` to the wording for this locale.
    }
  };

  return (
    <div className={styles.container}>
      <Helmet>
        <title>
          {intl.formatMessage({
            id: "menu.login",
            defaultMessage: "登录页",
          })}
          {Settings.title && ` - ${Settings.title}`}
        </title>
      </Helmet>
      <Lang />
      <div
        style={{
          flex: "1",
          padding: "32px 0",
        }}
      >
        <LoginForm
          contentStyle={{
            minWidth: 280,
            maxWidth: "75vw",
          }}
          logo={<img alt="logo" src="/logo.svg" />}
          title="glex"
          subTitle={intl.formatMessage({
            id: "pages.layouts.userLayout.title",
            defaultMessage: "Identity & Access 控制台",
          })}
          onFinish={async (values) => {
            await handleSubmit(values as LoginRequest);
          }}
        >
          <ProFormText
            name="username"
            fieldProps={{
              size: "large",
              prefix: <UserOutlined />,
            }}
            placeholder={intl.formatMessage({
              id: "pages.login.username.placeholder",
              defaultMessage: "用户名",
            })}
            rules={[
              {
                required: true,
                message: (
                  <FormattedMessage id="pages.login.username.required" defaultMessage="请输入用户名!" />
                ),
              },
            ]}
          />
          <ProFormText.Password
            name="password"
            fieldProps={{
              size: "large",
              prefix: <LockOutlined />,
            }}
            placeholder={intl.formatMessage({
              id: "pages.login.password.placeholder",
              defaultMessage: "密码",
            })}
            rules={[
              {
                required: true,
                message: (
                  <FormattedMessage id="pages.login.password.required" defaultMessage="请输入密码！" />
                ),
              },
            ]}
          />
        </LoginForm>
      </div>
      <Footer />
    </div>
  );
};

export default Login;
