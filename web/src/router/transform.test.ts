import type { Router, RouterMeta } from "@/services/access/v1";
import { toUmiRoutes } from "./transform";

const Leaf = () => null;
const InnerLink = () => null;

const deps = {
  resolve: (component: string) =>
    component === "iam/user/index" ? Leaf : undefined,
  innerLinkComponent: InnerLink,
  iconFor: () => null,
};

const meta = (title: string, extra: Partial<RouterMeta> = {}): RouterMeta => ({
  title,
  icon: undefined,
  noCache: undefined,
  link: undefined,
  activeMenu: undefined,
  ...extra,
});

const leaf = (
  path: string,
  component: string,
  overrides: Partial<Router> = {},
): Router => ({
  name: path,
  path,
  component,
  meta: meta(`t-${path}`),
  hidden: undefined,
  redirect: undefined,
  query: undefined,
  alwaysShow: undefined,
  children: undefined,
  ...overrides,
});

const dir = (
  path: string,
  children: Router[],
  overrides: Partial<Router> = {},
): Router => ({
  name: path,
  path,
  component: "Layout",
  meta: meta(`t-${path}`),
  hidden: undefined,
  redirect: "noRedirect",
  query: undefined,
  alwaysShow: true,
  children,
  ...overrides,
});

describe("toUmiRoutes", () => {
  it("maps a directory with children and redirects to the first leaf", () => {
    const { routes } = toUmiRoutes(
      [dir("/iam", [leaf("user", "iam/user/index"), leaf("role", "nope")], { meta: meta("IAM") })],
      deps,
    );
    expect(routes).toHaveLength(1);
    const directory = routes[0];
    expect(directory.path).toBe("/iam");
    expect(directory.name).toBe("IAM");
    expect(directory.alwaysShow).toBe(true);
    // "noRedirect" is RuoYi's sentinel: replaced by the first reachable leaf.
    expect(directory.redirect).toBe("/iam/user");
    expect(directory.routes?.map((r) => r.path)).toEqual(["/iam/user", "/iam/role"]);
    expect(directory.routes?.[0].component).toBe(Leaf);
  });

  it("maps an unregistered component to undefined (caller renders placeholder)", () => {
    const { routes } = toUmiRoutes(
      [leaf("user", "iam/user/index"), leaf("x", "typo/key")],
      deps,
    );
    expect(routes[0].component).toBe(Leaf);
    expect(routes[1].component).toBeUndefined();
  });

  it("records InnerLink URLs keyed by absolute path", () => {
    const { routes, innerLinks } = toUmiRoutes(
      [
        dir("/docs", [
          leaf("ruoyi/com/doc", "InnerLink", {
            meta: meta("RuoYi 文档", { link: "https://ruoyi.com/doc" }),
          }),
        ]),
      ],
      deps,
    );
    expect(innerLinks["/docs/ruoyi/com/doc"]).toBe("https://ruoyi.com/doc");
    expect(routes[0].routes?.[0].component).toBe(InnerLink);
  });

  it("honors an explicit redirect over the sentinel", () => {
    const { routes } = toUmiRoutes(
      [dir("/d", [leaf("a", "iam/user/index")], { redirect: "/somewhere" })],
      deps,
    );
    expect(routes[0].redirect).toBe("/somewhere");
  });

  it("keeps absolute child paths absolute and marks hidden leaves", () => {
    const { routes } = toUmiRoutes([
      dir("/d", [leaf("/absolute", "iam/user/index", { hidden: true })]),
    ], deps);
    expect(routes[0].routes?.[0].path).toBe("/absolute");
    expect(routes[0].routes?.[0].hideInMenu).toBe(true);
  });

  it("drops childless directories instead of a dead route", () => {
    const { routes } = toUmiRoutes([dir("/empty", [])], deps);
    expect(routes).toHaveLength(0);
  });
});
