import { asServiceError } from "@/services";
import { getIntl, type RequestConfig } from "@umijs/max";
import { message as toast } from "antd";
import { LOGIN_PATH, redirectToLogin } from "@/utils/token";

// User-facing wording per error reason. A reason with no entry here falls back
// to the server message. Kept aligned with api/*/v1/error_reason.proto.
const reasonMessageId: Record<string, string> = {
  INVALID_CREDENTIALS: "pages.login.invalidCredentials",
  ACCOUNT_DISABLED: "pages.login.accountDisabled",
  LOGIN_LOCKED: "pages.login.loginLocked",
  PERMISSION_DENIED: "pages.error.permissionDenied",
  USER_NAME_CONFLICT: "pages.error.userNameConflict",
  EMAIL_CONFLICT: "pages.error.emailConflict",
  PHONE_CONFLICT: "pages.error.phoneConflict",
  ROLE_ASSIGNED: "pages.error.roleAssigned",
  MENU_HAS_CHILDREN: "pages.error.menuHasChildren",
  DICT_TYPE_CONFLICT: "pages.error.dictTypeConflict",
  DICT_TYPE_IN_USE: "pages.error.dictTypeInUse",
  DICT_ENTRY_CONFLICT: "pages.error.dictEntryConflict",
  JOB_NAME_CONFLICT: "pages.error.jobNameConflict",
  JOB_CRON_INVALID: "pages.error.jobCronInvalid",
  JOB_HANDLER_UNKNOWN: "pages.error.jobHandlerUnknown",
  JOB_BUSY: "pages.error.jobBusy",
};

/**
 * Global request error handling: every failed request is reported here, so a
 * caller only has to handle its own success path. umi re-rejects afterwards, so
 * callers that need more than a toast can still catch and inspect `reason`.
 */
export const errorConfig: RequestConfig = {
  errorConfig: {
    errorHandler: (error: any, opts: any) => {
      if (opts?.skipErrorHandler) throw error;

      const serviceError = asServiceError(error);
      if (serviceError) {
        // 401 means the bearer token is missing, malformed, expired, or the
        // session was revoked (ADR 0018). On the login page itself a 401 is
        // just a failed attempt, so don't redirect there.
        if (
          serviceError.code === 401 &&
          window.location.pathname !== LOGIN_PATH
        ) {
          redirectToLogin();
        }
        const messageId =
          serviceError.reason && reasonMessageId[serviceError.reason];
        toast.error(
          messageId
            ? getIntl().formatMessage({ id: messageId })
            : serviceError.message || "Request failed, please retry.",
        );
        return;
      }

      // No structured body: a gateway error, a timeout, or an aborted request.
      if (error.response) {
        toast.error(`Response status: ${error.response.status}`);
      } else if (error.request) {
        toast.error("None response! Please retry.");
      } else {
        toast.error("Request error, please retry.");
      }
    },
  },
};
