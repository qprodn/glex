/**
 * @name 代理的配置
 * @see 在生产环境 代理是无法生效的，所以这里没有生产环境的配置
 * @doc https://umijs.org/docs/guides/proxy
 *
 * The backend mounts its HTTP handlers at the root (see the google.api.http
 * annotations in api/-/-v1 protos: /auth, /iam, /monitor), listening on :8000.
 */
export default {
  dev: {
    "/auth": {
      target: "http://localhost:8000",
      changeOrigin: true,
    },
    "/iam": {
      target: "http://localhost:8000",
      changeOrigin: true,
    },
    "/monitor": {
      target: "http://localhost:8000",
      changeOrigin: true,
    },
  },
};
