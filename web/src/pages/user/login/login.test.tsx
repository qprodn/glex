import { TestBrowser } from '@@/testBrowser';
import { services } from '@/services';
import { fireEvent, render, waitFor } from '@testing-library/react';
import React from 'react';

const picture =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

// Smoke: the rewritten login page mounts against the real runtime config
// (render() hook, initialState, layout) without touching the network — the
// token store is empty in jsdom, so loadSession short-circuits, and the
// Login Challenge picture is a stub.
describe('Login Page', () => {
  beforeEach(() => {
    jest.spyOn(services.auth, 'IssueLoginChallenge').mockResolvedValue({
      challengeId: 'cid',
      imagePng: picture,
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

  it('asks for a picture again instead of logging in when none loaded', async () => {
    const issue = jest.spyOn(services.auth, 'IssueLoginChallenge').mockRejectedValue(new Error('down'));
    const login = jest.spyOn(services.auth, 'Login').mockResolvedValue({ accessToken: 'token' });
    const rootContainer = render(
      <TestBrowser historyRef={React.createRef()} location={{ pathname: '/user/login' }} />,
    );

    const username = await rootContainer.findByPlaceholderText(/用户名|Username/);
    fireEvent.change(username, { target: { value: 'admin' } });
    fireEvent.change(rootContainer.getByPlaceholderText(/密码|Password/), {
      target: { value: 'admin123' },
    });
    fireEvent.change(rootContainer.getByPlaceholderText(/验证码|Verification code/), {
      target: { value: 'ABCD' },
    });
    fireEvent.click(rootContainer.getByRole('button', { name: /登\s*录|Login/ }));

    await waitFor(() => expect(issue).toHaveBeenCalledTimes(2));
    expect(login).not.toHaveBeenCalled();

    rootContainer.unmount();
  });

  it('clears the typed answer when the picture is replaced', async () => {
    const issue = jest
      .spyOn(services.auth, 'IssueLoginChallenge')
      .mockResolvedValueOnce({ challengeId: 'cid-1', imagePng: picture })
      .mockResolvedValueOnce({ challengeId: 'cid-2', imagePng: picture });
    const rootContainer = render(
      <TestBrowser historyRef={React.createRef()} location={{ pathname: '/user/login' }} />,
    );

    const answer = await rootContainer.findByPlaceholderText(/验证码|Verification code/);
    await rootContainer.findByRole('img', { name: /验证码|Verification code/ });
    fireEvent.change(answer, { target: { value: 'ABCD' } });
    fireEvent.click(rootContainer.getByTitle(/看不清|Can't read it/));

    await waitFor(() => expect(issue).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(
        (rootContainer.getByPlaceholderText(/验证码|Verification code/) as HTMLInputElement).value,
      ).toBe(''),
    );

    rootContainer.unmount();
  });
});
