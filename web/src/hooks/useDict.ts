import type { DictEntryOption, TagStyle } from "@/services/dict/v1";
import { services } from "@/services";
import { useEffect, useState } from "react";

/**
 * Dict consumption hook: fetch one Dict Type's options (value/label/tag
 * style/default) and cache them module-side for the session — per type, with
 * in-flight promise dedup so ten components asking for the same type share
 * one request. A reload re-fetches; there is no server-side read cache
 * (see the dict module).
 *
 * Degradation contract: a failed fetch or an unknown type key yields an
 * EMPTY list, never a hardcoded fallback — the dict is the single source of
 * truth, and a missing entry renders as its raw value (see dictLabelOf).
 */

const cache = new Map<string, DictEntryOption[]>();
const inflight = new Map<string, Promise<DictEntryOption[]>>();

function load(typeKey: string): Promise<DictEntryOption[]> {
  const cached = cache.get(typeKey);
  if (cached) {
    return Promise.resolve(cached);
  }
  let pending = inflight.get(typeKey);
  if (!pending) {
    pending = services.dict
      .ListDictEntriesByType({ dictType: typeKey })
      .then((set) => {
        const options = set.options ?? [];
        cache.set(typeKey, options);
        return options;
      })
      .catch((err: unknown) => {
        // Empty options + the raw-value rendering downstream is the whole
        // degradation story; no silent fallback vocabulary exists.
        console.warn(`[useDict] failed to load dict "${typeKey}"`, err);
        return [] as DictEntryOption[];
      })
      .finally(() => {
        inflight.delete(typeKey);
      });
    inflight.set(typeKey, pending);
  }
  return pending;
}

/** The options of one Dict Type, ordered by dict_sort (server-side). */
export function useDict(typeKey: string): DictEntryOption[] {
  const [options, setOptions] = useState<DictEntryOption[]>(
    () => cache.get(typeKey) ?? [],
  );
  useEffect(() => {
    let alive = true;
    load(typeKey).then((opts) => {
      if (alive) {
        setOptions(opts);
      }
    });
    return () => {
      alive = false;
    };
  }, [typeKey]);
  return options;
}

/** Look up an option by machine value. */
export function dictOptionOf(
  options: DictEntryOption[],
  value: string | number | undefined,
): DictEntryOption | undefined {
  const key = `${value ?? ""}`;
  return options.find((option) => option.value === key);
}

/**
 * The display label for a machine value; the RAW value itself when the dict
 * has no such entry (fetch failed, type unknown, value drifted) — the only
 * honest rendering, and the reason no fallback enum exists.
 */
export function dictLabelOf(
  options: DictEntryOption[],
  value: string | number | undefined,
): string {
  const option = dictOptionOf(options, value);
  return option?.label || `${value ?? ""}`;
}

/**
 * Map the wire tag-style preset (RuoYi list_class) to an antd Tag color.
 * RuoYi-faithful: primary=blue, danger=red, warning=orange, success=green,
 * info=gray; the plain preset renders with no color (antd's outlined
 * default) — visually close to info's filled gray, like element-plus's own
 * subtle ''-vs-info difference.
 */
export function tagColorOf(style: TagStyle | undefined): string | undefined {
  switch (style) {
    case "TAG_STYLE_PRIMARY":
      return "processing";
    case "TAG_STYLE_SUCCESS":
      return "success";
    case "TAG_STYLE_WARNING":
      return "warning";
    case "TAG_STYLE_DANGER":
      return "error";
    case "TAG_STYLE_INFO":
      return "default";
    default:
      return undefined;
  }
}

/** The wire values every tag-style select offers (proto TagStyle). */
export const TAG_STYLES: TagStyle[] = [
  "TAG_STYLE_DEFAULT",
  "TAG_STYLE_PRIMARY",
  "TAG_STYLE_SUCCESS",
  "TAG_STYLE_WARNING",
  "TAG_STYLE_DANGER",
  "TAG_STYLE_INFO",
];

/** Label for a tag-style preset in selects ( untranslated preset names). */
export function tagStyleLabel(style: TagStyle): string {
  return style.replace("TAG_STYLE_", "").toLowerCase();
}

/**
 * Options re-keyed as numbers for int-backed fields (e.g. status 0/1,
 * gender 0/1/2). Non-integer values are dropped — wire such a dict only to
 * string-valued fields.
 */
export function numericOptions(
  options: DictEntryOption[],
): { value: number; label: string }[] {
  return options
    .filter((option) => option.value !== undefined && Number.isInteger(Number(option.value)))
    .map((option) => ({ value: Number(option.value), label: option.label ?? option.value ?? "" }));
}

/** ProTable search valueEnum for an int-backed field, from dict options. */
export function valueEnumOf(
  options: DictEntryOption[],
): Record<number, { text: string }> {
  return Object.fromEntries(
    numericOptions(options).map(({ value, label }) => [value, { text: label }]),
  );
}
