package payinfra

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	payservice "glex/internal/domain/pay/service"
)

// noticePostTimeout bounds one automatic delivery. A timeout is a failed
// attempt, the same as a connection failure.
const noticePostTimeout = 5 * time.Second

// errNotifyTargetDisallowed is a refused dial to a non-public address.
var errNotifyTargetDisallowed = errors.New("business notice target is not a public address")

type httpNoticePoster struct {
	client *http.Client
}

// NewNoticePoster posts unsigned JSON to the address stored on a notice.
// Redirects are not followed. A dial to a loopback, private, or link-local
// address fails. The response body is discarded.
func NewNoticePoster() payservice.NoticePoster {
	return newHTTPNoticePoster(false)
}

// newHTTPNoticePoster builds the delivery client. allowPrivate is for tests
// that post to a local server; production delivery leaves it false.
func newHTTPNoticePoster(allowPrivate bool) *httpNoticePoster {
	dialer := &net.Dialer{Timeout: noticePostTimeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if allowPrivate {
				return dialer.DialContext(ctx, network, addr)
			}
			return dialPublic(ctx, dialer, network, addr)
		},
	}
	return &httpNoticePoster{client: &http.Client{
		Timeout:   noticePostTimeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// dialPublic resolves addr and dials one public address. Any loopback,
// private, or link-local answer refuses the whole lookup, so a name that
// mixes public and private addresses is not dialed.
func dialPublic(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errNotifyTargetDisallowed
	}
	for _, ip := range ips {
		if payservice.DisallowedNotifyIP(ip.IP) {
			return nil, errNotifyTargetDisallowed
		}
	}
	// Dial the resolved address. A second lookup of the name could rebind.
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// Post implements the delivery HTTP boundary.
func (p *httpNoticePoster) Post(ctx context.Context, req payservice.NoticePost) (payservice.NoticePostResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return payservice.NoticePostResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return payservice.NoticePostResult{}, err
	}
	defer resp.Body.Close()
	// The body is not part of the attempt. Drain a bounded prefix so the
	// connection can be reused; a short read does not change the status.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return payservice.NoticePostResult{StatusCode: resp.StatusCode}, nil
}
