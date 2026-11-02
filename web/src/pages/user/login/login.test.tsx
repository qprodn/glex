import { TestBrowser } from '@@/testBrowser';
import { services } from '@/services';
import { render } from '@testing-library/react';
import React from 'react';

// Smoke: the rewritten login page mounts against the real runtime config
// (render() hook, initialState, layout) without touching the network — the
// token store is empty in jsdom, so loadSession short-circuits, and the
// Login Challenge picture is a stub.
describe('Login Page', () => {
  beforeEach(() => {
    jest.spyOn(services.auth, 'IssueLoginChallenge').mockResolvedValue({
      challengeId: 'cid',
      imagePng:
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
    });
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });
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
    expect(await rootContainer.findByPlaceholderText(/验证码|Verification code/)).toBeTruthy();
    expect(await rootContainer.findByRole('img', { name: /验证码|Verification code/ })).toBeTruthy();

    rootContainer.unmount();
  });
});
