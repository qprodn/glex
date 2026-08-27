import { matchPermission } from "./permission";

describe("matchPermission", () => {
  it("matches an exact handle", () => {
    expect(matchPermission("system:user:add", ["system:user:add"])).toBe(true);
  });

  it("rejects a handle the snapshot lacks", () => {
    expect(matchPermission("system:user:add", ["system:user:list"])).toBe(false);
  });

  it("matches the superadmin wildcard *:*:*", () => {
    expect(matchPermission("monitor:online:forceLogout", ["*:*:*"])).toBe(true);
  });

  it("matches a segment wildcard only at that segment", () => {
    expect(matchPermission("system:user:add", ["system:*:add"])).toBe(true);
    expect(matchPermission("system:user:add", ["*:user:add"])).toBe(true);
    expect(matchPermission("system:user:add", ["system:user:*"])).toBe(true);
  });

  it("does not let a short wildcard span segments", () => {
    // `*` matches exactly one segment; it is not a prefix glob.
    expect(matchPermission("system:user:add", ["system:*"])).toBe(false);
    expect(matchPermission("system:user:add", ["*"])).toBe(false);
  });

  it("requires equal segment counts", () => {
    expect(matchPermission("system:user", ["system:user:add"])).toBe(false);
  });

  it("handles empty inputs without throwing", () => {
    expect(matchPermission("", ["*:*:*"])).toBe(false);
    expect(matchPermission("system:user:add", undefined)).toBe(false);
    expect(matchPermission("system:user:add", [""])).toBe(false);
  });
});
