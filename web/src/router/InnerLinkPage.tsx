import { useLocation } from "@umijs/max";
import React from "react";
import { getInnerLink } from "./innerLinks";

/**
 * InnerLink menus (RuoYi 内链): a menu whose path is an http(s) URL renders
 * embedded instead of leaving the console. The URL travels in RouterMeta.link
 * and is stashed by the route transform keyed by absolute path.
 */
const InnerLinkPage: React.FC = () => {
  const { pathname } = useLocation();
  const url = getInnerLink(pathname);
  if (!url) {
    return <iframe title="inner-link" src="about:blank" style={{ border: 0, width: "100%", height: "100%" }} />;
  }
  return (
    <iframe
      title="inner-link"
      src={url}
      style={{ border: 0, width: "100%", height: "100%", minHeight: "calc(100vh - 180px)" }}
    />
  );
};

export default InnerLinkPage;
