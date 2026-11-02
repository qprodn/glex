import { Footer } from "@/components";
import type { LoginRequest } from "@/services/authn/v1";
import { services } from "@/services";
import { setToken } from "@/utils/token";
import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { LoginForm, ProFormText, type ProFormInstance } from "@ant-design/pro-components";
import { FormattedMessage, Helmet, SelectLang, useIntl } from "@umijs/max";
import { App } from "antd";
import { createStyles } from "antd-style";
import React, { useEffect, useRef, useState } from "react";
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
    challenge: {
      display: "flex",
      alignItems: "flex-start",
      gap: 8,
    },
    challengeInput: {
      flex: 1,
      minWidth: 0,
    },
    challengeImage: {
      marginTop: 0,
      padding: 0,
      border: `1px solid ${token.colorBorder}`,
      borderRadius: token.borderRadius,
      background: token.colorBgContainer,
      cursor: "pointer",
      height: 40,
      minWidth: 96,
      overflow: "hidden",
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
  const [challengeId, setChallengeId] = useState<string>();
  const [imageSrc, setImageSrc] = useState<string>();
  const challengeSeq = useRef(0);
  const formRef = useRef<ProFormInstance>(undefined);

  const loadChallenge = async () => {
    const seq = ++challengeSeq.current;
    try {
      const reply = await services.auth.IssueLoginChallenge({});
      if (seq !== challengeSeq.current) {
        return;
      }
      setChallengeId(reply.challengeId);
      setImageSrc(reply.imagePng ? `data:image/png;base64,${reply.imagePng}` : undefined);
      // The previous answer cannot match the picture that just arrived.
      formRef.current?.setFieldValue("challengeAnswer", "");
    } catch {
      if (seq !== challengeSeq.current) {
        return;
      }
      // The global handler reports the failure. A click on the picture retries.
      setChallengeId(undefined);
      setImageSrc(undefined);
      formRef.current?.setFieldValue("challengeAnswer", "");
    }
  };

  useEffect(() => {
    void loadChallenge();
  }, []);

  const handleSubmit = async (req: LoginRequest) => {
    if (!challengeId) {
      // The picture never arrived. Ask again instead of sending a login the
      // server can only reject as an invalid challenge.
      await loadChallenge();
      return;
    }
    try {
      const reply = await services.auth.Login({
        ...req,
        challengeId,
      });
      if (!reply.accessToken) {
        await loadChallenge();
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
      // maps `reason` to the wording for this locale. The submitted challenge
      // is already spent, so the next attempt needs a new picture.
      await loadChallenge();
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
          formRef={formRef}
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
          <div className={styles.challenge}>
            <div className={styles.challengeInput}>
              <ProFormText
                name="challengeAnswer"
                fieldProps={{
                  size: "large",
                  autoComplete: "off",
                }}
                placeholder={intl.formatMessage({
                  id: "pages.login.challenge.placeholder",
                  defaultMessage: "验证码",
                })}
                rules={[
                  {
                    required: true,
                    message: (
                      <FormattedMessage
                        id="pages.login.challenge.required"
                        defaultMessage="请输入验证码！"
                      />
                    ),
                  },
                ]}
              />
            </div>
            <button
              type="button"
              className={styles.challengeImage}
              onClick={() => {
                void loadChallenge();
              }}
              title={intl.formatMessage({
                id: "pages.login.challenge.refresh",
                defaultMessage: "看不清？点击换一张",
              })}
            >
              {imageSrc ? (
                <img
                  alt={intl.formatMessage({
                    id: "pages.login.challenge.placeholder",
                    defaultMessage: "验证码",
                  })}
                  src={imageSrc}
                  style={{ height: 40, display: "block" }}
                />
              ) : null}
            </button>
          </div>
        </LoginForm>
      </div>
      <Footer />
    </div>
  );
};

export default Login;
