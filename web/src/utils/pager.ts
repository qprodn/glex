export type TokenPage<T> = {
  items: T[];
  nextPageToken?: string | undefined;
};

/**
 * ProTable's request params minus paging: the live search-form values. The
 * fetcher composes them (e.g. into an AIP-160 filter) itself.
 */
export type ProTableParams = Record<string, unknown> & {
  current?: number;
  pageSize?: number;
};

export type TokenPageFetcher<T> = (
  pageSize: number,
  pageToken: string,
  params: ProTableParams,
) => Promise<TokenPage<T>>;

/**
 * Adapt a page-token API (AIP-158: opaque nextPageToken, no totals) to
 * ProTable's `{current, pageSize, total}` request contract. Page tokens are
 * remembered per page number; a jump past the fetched frontier walks forward
 * token by token (each stop is one request). Changing the page size or any
 * search/sort parameter resets to page one — the tokens name a position in a
 * result set that just changed.
 *
 * `total` is reported as "rows so far + one ghost row" while a next token
 * exists, which is the smallest lie that keeps the next-page arrow honest.
 */
export function createTokenPagedRequest<T>(fetcher: TokenPageFetcher<T>) {
  let tokens: string[] = [""]; // tokens[i] is the token leading INTO page i+1
  let stabilityKey = "";
  return async (params: ProTableParams) => {
    const { current = 1, pageSize = 10 } = params;
    const key = JSON.stringify({ ...params, current: undefined });
    if (key !== stabilityKey) {
      stabilityKey = key;
      tokens = [""];
    }
    while (tokens.length < current) {
      const walker = tokens[tokens.length - 1] ?? "";
      const walked = await fetcher(pageSize, walker, params);
      tokens.push(walked.nextPageToken ?? "");
      if (!walked.nextPageToken) {
        // Last page reached while walking; pad so the loop terminates.
        tokens.push("");
        break;
      }
    }
    const pageToken = tokens[current - 1] ?? "";
    const page = await fetcher(pageSize, pageToken, params);
    tokens[current] = page.nextPageToken ?? "";
    const hasNext = Boolean(page.nextPageToken);
    return {
      data: page.items,
      success: true,
      total: (current - 1) * pageSize + page.items.length + (hasNext ? 1 : 0),
    };
  };
}
