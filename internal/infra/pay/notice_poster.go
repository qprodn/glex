package payinfra

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	payservice "glex/internal/domain/pay/service"
)

// noticePostTimeout bounds one automatic delivery. A timeout is a failed
// attempt, the same as a connection failure.
const noticePostTimeout = 5 * time.Second

type httpNoticePoster struct {
	client *http.Client
}

// NewNoticePoster posts unsigned JSON to the address stored on a notice.
// The response body is discarded.
func NewNoticePoster() payservice.NoticePoster {
	return &httpNoticePoster{client: &http.Client{Timeout: noticePostTimeout}}
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
