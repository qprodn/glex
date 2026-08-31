import type { DictEntryOption } from "@/services/dict/v1";
import { dictLabelOf, numericOptions, tagColorOf } from "./useDict";

const options: DictEntryOption[] = [
  { value: "0", label: "正常", tagStyle: "TAG_STYLE_PRIMARY", isDefault: true },
  { value: "1", label: "停用", tagStyle: "TAG_STYLE_DANGER", isDefault: false },
];

describe("dictLabelOf", () => {
  it("resolves a machine value to its label", () => {
    expect(dictLabelOf(options, 0)).toBe("正常");
    expect(dictLabelOf(options, "1")).toBe("停用");
  });

  it("degrades to the raw value when the entry is missing", () => {
    // The degradation contract: no hardcoded fallback vocabulary — the raw
    // value is the honest rendering of a missing dict entry.
    expect(dictLabelOf(options, 9)).toBe("9");
    expect(dictLabelOf([], 0)).toBe("0");
    expect(dictLabelOf(options, undefined)).toBe("");
  });
});

describe("tagColorOf", () => {
  it("maps wire presets to antd tag colors", () => {
    expect(tagColorOf("TAG_STYLE_PRIMARY")).toBe("processing");
    expect(tagColorOf("TAG_STYLE_DANGER")).toBe("error");
    expect(tagColorOf("TAG_STYLE_SUCCESS")).toBe("success");
    expect(tagColorOf("TAG_STYLE_WARNING")).toBe("warning");
    expect(tagColorOf("TAG_STYLE_INFO")).toBe("default");
  });

  it("treats unset/default as a plain tag", () => {
    expect(tagColorOf("TAG_STYLE_DEFAULT")).toBeUndefined();
    expect(tagColorOf(undefined)).toBeUndefined();
  });
});

describe("numericOptions", () => {
  it("re-keys integer values for int-backed fields", () => {
    expect(numericOptions(options)).toEqual([
      { value: 0, label: "正常" },
      { value: 1, label: "停用" },
    ]);
  });

  it("drops non-integer values", () => {
    const mixed: DictEntryOption[] = [
      { value: "Y", label: "是", tagStyle: undefined, isDefault: false },
      { value: "0", label: "否", tagStyle: undefined, isDefault: false },
    ];
    expect(numericOptions(mixed)).toEqual([{ value: 0, label: "否" }]);
  });
});
