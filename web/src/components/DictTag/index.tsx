import { dictLabelOf, tagColorOf, useDict } from "@/hooks/useDict";
import { Tag } from "antd";
import React from "react";

type DictTagProps = {
  /** The Dict Type key (e.g. "sys_normal_disable"). */
  typeKey: string;
  /** The machine value to render (e.g. "0"). */
  value: string | number | undefined;
};

/**
 * A status tag rendered from the dict: the entry's label with its tag-style
 * color. When the dict has no such entry (fetch failed, type unknown, value
 * drifted) it degrades to the raw value in a plain tag — there is no
 * hardcoded fallback vocabulary (see useDict).
 */
const DictTag: React.FC<DictTagProps> = ({ typeKey, value }) => {
  const options = useDict(typeKey);
  const option = options.find((item) => item.value === `${value ?? ""}`);
  return (
    <Tag color={tagColorOf(option?.tagStyle)}>
      {dictLabelOf(options, value)}
    </Tag>
  );
};

export default DictTag;
