import { buildFilter } from "./aipFilter";

describe("buildFilter", () => {
  it("quotes strings and leaves numbers bare", () => {
    expect(
      buildFilter([
        { field: "user_name", value: "admin" },
        { field: "status", value: 0 },
      ]),
    ).toBe('user_name="admin" AND status=0');
  });

  it("skips empty and undefined values", () => {
    expect(
      buildFilter([
        { field: "user_name", value: "" },
        { field: "email", value: undefined },
        { field: "phone_number", value: "13800000000" },
      ]),
    ).toBe('phone_number="13800000000"');
  });

  it("returns undefined when nothing survives", () => {
    expect(buildFilter([{ field: "user_name", value: "" }])).toBeUndefined();
    expect(buildFilter([])).toBeUndefined();
  });

  it("supports range operators", () => {
    expect(
      buildFilter([
        { field: "created_at", value: "2026-01-01T00:00:00Z", op: ">=" },
      ]),
    ).toBe('created_at>="2026-01-01T00:00:00Z"');
  });

  it("quotes the string form of numeric status from a select", () => {
    expect(buildFilter([{ field: "status", value: "1" }])).toBe('status="1"');
  });
});
