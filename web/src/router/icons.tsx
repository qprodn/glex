import * as Icons from "@ant-design/icons";
import type { ReactNode } from "react";

/**
 * Menu records carry RuoYi-style icon names ("user", "peoples", "monitor").
 * They live in data, not code, so the set is resolved at runtime through the
 * full @ant-design/icons table; an unknown name falls back to a dot. The
 * table is heavy for a public site but acceptable for an intranet console.
 */
export function iconFor(name: string | undefined): ReactNode {
  if (!name) {
    return null;
  }
  const candidates = [
    name.charAt(0).toUpperCase() + name.slice(1),
    `${name.charAt(0).toUpperCase() + name.slice(1)}Outlined`,
  ];
  for (const candidate of candidates) {
    const Icon = (Icons as unknown as Record<string, React.ComponentType>)[
      candidate
    ];
    if (Icon) {
      return <Icon />;
    }
  }
  return <Icons.MinusOutlined />;
}
