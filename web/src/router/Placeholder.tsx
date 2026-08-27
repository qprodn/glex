import { Result } from "antd";
import React from "react";
import { useIntl } from "@umijs/max";

/**
 * Rendered when a Menu record's `component` string has no entry in the
 * registry (ADR 0019) — a page not yet built, or a typo in menu management.
 */
const Placeholder: React.FC = () => {
  const intl = useIntl();
  return (
    <Result
      status="warning"
      title={intl.formatMessage({ id: "pages.placeholder.title" })}
      subTitle={intl.formatMessage({ id: "pages.placeholder.subTitle" })}
    />
  );
};

export default Placeholder;
