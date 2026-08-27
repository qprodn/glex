export type FilterPart = {
  field: string;
  value?: string | number | undefined;
  /** Comparison operator; defaults to "=" (AIP-160 allows >=, <=, >, <). */
  op?: ">=" | "<=" | ">" | "<" | "=";
};

/**
 * Compose an AIP-160 filter string from the parts the search form produced,
 * e.g. buildFilter([{field:"user_name",value:"admin"},{field:"status",value:0}])
 *   => 'user_name="admin" AND status=0'.
 * Numbers are emitted bare, strings are quoted; empty values are skipped.
 * Returns undefined when nothing survives, so callers can omit the param.
 */
export function buildFilter(parts: FilterPart[]): string | undefined {
  const clauses = parts
    .filter((p) => p.value !== undefined && `${p.value}` !== "")
    .map((p) => {
      const op = p.op ?? "=";
      return typeof p.value === "number"
        ? `${p.field}${op}${p.value}`
        : `${p.field}${op}"${p.value}"`;
    });
  return clauses.length > 0 ? clauses.join(" AND ") : undefined;
}
