import type { DeptTreeOption } from "@/services/org/v1";
import type { DataNode } from "antd/es/tree";

export type DeptSelectNode = {
  title: string;
  value: number;
  disabled?: boolean;
  children?: DeptSelectNode[];
};

/**
 * The ListDeptTree consumption read (login-only) feeds every department
 * picker: the left filter trees and the form selects. These walks convert
 * the wire options into the shapes antd and ProComponents expect; all pages
 * share them so the tree renders identically everywhere (ADR 0013 — the
 * department tree is resolved in the web client, never joined server-side).
 */

/** Left-tree shape (antd Tree): label as title, id as key. */
export function toDeptTreeData(nodes: DeptTreeOption[] | undefined): DataNode[] {
  return (nodes ?? []).map((node) => ({
    title: node.label ?? "",
    key: node.deptId ?? 0,
    children: node.children?.length ? toDeptTreeData(node.children) : undefined,
  }));
}

/** Select shape (ProFormTreeSelect): disabled departments grey out but stay visible. */
export function toDeptSelectData(nodes: DeptTreeOption[] | undefined): DeptSelectNode[] {
  return (nodes ?? []).map((node) => ({
    title: node.label ?? "",
    value: node.deptId ?? 0,
    disabled: node.disabled,
    children: node.children?.length ? toDeptSelectData(node.children) : undefined,
  }));
}

/** id → name across the whole tree, for the local deptName columns. */
export function buildDeptNames(nodes: DeptTreeOption[] | undefined): Map<number, string> {
  const names = new Map<number, string>();
  const walk = (list: DeptTreeOption[] | undefined) =>
    (list ?? []).forEach((node) => {
      names.set(node.deptId ?? 0, node.label ?? "");
      walk(node.children);
    });
  walk(nodes);
  return names;
}
