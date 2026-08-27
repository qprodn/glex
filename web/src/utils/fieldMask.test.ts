import { collectUpdateMask } from "./fieldMask";

describe("collectUpdateMask", () => {
  const before = { nickName: "Ada", email: "ada@example.com", status: 0 };

  it("lists only changed fields as lowerCamel, comma-joined", () => {
    expect(
      collectUpdateMask(before, { ...before, nickName: "Grace", status: 1 }, [
        "nickName",
        "email",
        "status",
      ]),
    ).toBe("nickName,status");
  });

  it("returns an empty mask when nothing changed", () => {
    expect(
      collectUpdateMask(before, { ...before }, ["nickName", "email", "status"]),
    ).toBe("");
  });

  it("treats absent-before as a change (create-then-edit flows)", () => {
    expect(collectUpdateMask(undefined, { remark: "hi" }, ["remark"])).toBe(
      "remark",
    );
  });

  it("distinguishes 0 from undefined (Object.is semantics)", () => {
    expect(collectUpdateMask({ gender: 0 }, { gender: 0 }, ["gender"])).toBe("");
    expect(collectUpdateMask({}, { gender: 0 }, ["gender"])).toBe("gender");
    expect(
      collectUpdateMask({ gender: undefined }, { gender: 0 }, ["gender"]),
    ).toBe("gender");
  });

  it("ignores fields outside candidates even when changed", () => {
    expect(
      collectUpdateMask(before, { ...before, avatar: "x.png" }, [
        "nickName",
        "email",
      ]),
    ).toBe("");
  });
});
