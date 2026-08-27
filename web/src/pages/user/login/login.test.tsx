import { TestBrowser } from '@@/testBrowser';
import { render } from '@testing-library/react';
import React from 'react';

// Smoke: the rewritten login page mounts against the real runtime config
// (render() hook, initialState, layout) without touching the network — the
// token store is empty in jsdom, so loadSession short-circuits.
describe('Login Page', () => {
  it('renders the glex login form', async () => {
    const historyRef = React.createRef<any>();
    const rootContainer = render(
      <TestBrowser
        historyRef={historyRef}
        location={{
          pathname: '/user/login',
        }}
      />,
    );

    await rootContainer.findAllByText('glex');

    // jsdom's navigator.language is en-US, so either locale's wording passes.
    expect(await rootContainer.findByPlaceholderText(/用户名|Username/)).toBeTruthy();
    expect(await rootContainer.findByPlaceholderText(/密码|Password/)).toBeTruthy();

    rootContainer.unmount();
  });
});
