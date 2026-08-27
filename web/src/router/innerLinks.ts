/** Absolute route path -> embedded URL, filled by toUmiRoutes at boot. */
const innerLinks: Record<string, string> = {};

export function setInnerLinks(links: Record<string, string>): void {
  for (const key of Object.keys(innerLinks)) {
    delete innerLinks[key];
  }
  Object.assign(innerLinks, links);
}

export function getInnerLink(pathname: string): string | undefined {
  return innerLinks[pathname];
}
