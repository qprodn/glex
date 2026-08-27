import { history } from "@umijs/max";

// ADR 0018: the one-shot bearer token from POST /auth/login is kept in
// localStorage; every generated-client call attaches it, and any 401 purges
// it and returns to the login page.
const TOKEN_KEY = "glex_token";

export const LOGIN_PATH = "/user/login";

export function getToken(): string | null {
  return window.localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  window.localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  window.localStorage.removeItem(TOKEN_KEY);
}

/**
 * Drop the token and go to the login page, remembering where we came from so
 * a successful login can return here. `forceLoad` reloads the app so the
 * dynamic routes injected from getRouters are torn down too.
 */
export function redirectToLogin(forceLoad = false): void {
  clearToken();
  const { pathname, search } = window.location;
  const target =
    pathname === LOGIN_PATH
      ? LOGIN_PATH
      : `${LOGIN_PATH}?redirect=${encodeURIComponent(pathname + search)}`;
  if (forceLoad) {
    window.location.href = target;
  } else {
    history.push(target);
  }
}
