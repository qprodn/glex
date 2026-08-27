/* eslint-disable */
// CommonJS on purpose: jest loads this config with require(), and the
// project tsconfig's module=esnext (needed for the registry's dynamic
// imports) would otherwise break ts-node's config loader.
const { configUmiAlias, createConfig } = require('@umijs/max/test');

module.exports = async () => {
  const config = await configUmiAlias({
    ...createConfig({
      target: 'browser',
    }),
  });
  return {
    ...config,
    testEnvironmentOptions: {
      ...(config?.testEnvironmentOptions || {}),
      url: 'http://localhost:8000',
    },
    setupFiles: [...(config.setupFiles || []), './tests/setupTests.jsx'],
    globals: {
      ...config.globals,
      localStorage: null,
    },
  };
};
