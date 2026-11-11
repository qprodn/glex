package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
)

// Delivery is tested at the pay domain service. The notice store is in
// memory and the poster is scripted. The address under test is the one
// stored on the notice; this fixture has no Pay Application to read.

func TestDeliverDueAcceptsPaymentNoticeAtStoredAddress(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, &payentity.BusinessNotice{
		ID:            11,
		ApplicationID: 7,
		Kind:          payentity.NoticeKindPayment,
		PaymentID:     42,
		BizNo:         "ORDER-9",
		NotifyURL:     "https://caller.example/paid-at-creation",
		Status:        payentity.NoticeWaiting,
		AttemptCount:  0,
		NextAttemptAt: now,
	})
	poster := &scriptPoster{status: 200}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if len(poster.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(poster.posts))
	}
	post := poster.posts[0]
	if post.URL != "https://caller.example/paid-at-creation" {
		t.Fatalf("url = %q, want the address copied onto the notice", post.URL)
	}
	if string(post.Body) != `{"biz_no":"ORDER-9","payment_id":42}` {
		t.Fatalf("body = %s, want payment fields", post.Body)
	}

	notice := store.notice(11)
	if notice.Status != payentity.NoticeAccepted || notice.AttemptCount != 1 || !notice.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want accepted after one attempt with no next time", notice)
	}
	if len(store.attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(store.attempts))
	}
	attempt := store.attempts[0]
	if attempt.NoticeID != 11 || attempt.Manual || attempt.HTTPStatus == nil || *attempt.HTTPStatus != 200 ||
		attempt.Error != "" || !attempt.Accepted {
		t.Fatalf("attempt = %+v, want automatic HTTP 200 accepted", attempt)
	}

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("second deliver: %v", err)
	}
	if len(poster.posts) != 1 {
		t.Fatalf("posts after acceptance = %d, want the schedule to stop", len(poster.posts))
	}
}

func TestDeliverDueAcceptsRefundNoticeAtStoredAddress(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, &payentity.BusinessNotice{
		ID:            12,
		ApplicationID: 7,
		Kind:          payentity.NoticeKindRefund,
		PaymentID:     42,
		RefundID:      77,
		BizNo:         "ORDER-9",
		NotifyURL:     "https://caller.example/refund-at-creation",
		Status:        payentity.NoticeWaiting,
		NextAttemptAt: now,
	})
	poster := &scriptPoster{status: 200}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(poster.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(poster.posts))
	}
	post := poster.posts[0]
	if post.URL != "https://caller.example/refund-at-creation" {
		t.Fatalf("url = %q, want the address copied onto the notice", post.URL)
	}
	if string(post.Body) != `{"refund_id":77,"payment_id":42}` {
		t.Fatalf("body = %s, want refund fields", post.Body)
	}
	notice := store.notice(12)
	if notice.Status != payentity.NoticeAccepted || notice.AttemptCount != 1 || !notice.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want accepted", notice)
	}
}

func TestDeliverDueFailureWaitsFifteenSeconds(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, waitingPayment(now))
	poster := &scriptPoster{status: 500}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("deliver: %v, want a failed attempt to stay a business outcome", err)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeWaiting || notice.AttemptCount != 1 || !notice.NextAttemptAt.Equal(now.Add(15*time.Second)) {
		t.Fatalf("notice = %+v, want waiting with the next attempt 15s later", notice)
	}
	if len(store.attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(store.attempts))
	}
	attempt := store.attempts[0]
	if attempt.Manual || attempt.HTTPStatus == nil || *attempt.HTTPStatus != 500 || attempt.Error != "" || attempt.Accepted {
		t.Fatalf("attempt = %+v, want automatic HTTP 500 not accepted", attempt)
	}

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now.Add(15*time.Second - time.Second)}); err != nil {
		t.Fatalf("early deliver: %v", err)
	}
	if len(poster.posts) != 1 {
		t.Fatalf("posts before the next time = %d, want 1", len(poster.posts))
	}
}

func TestDeliverDueConnectionFailureRecordsTheError(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, waitingPayment(now))
	poster := &scriptPoster{err: errors.New("dial tcp: connection refused")}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("deliver: %v, want a failed attempt to stay a business outcome", err)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeWaiting || notice.AttemptCount != 1 || !notice.NextAttemptAt.Equal(now.Add(15*time.Second)) {
		t.Fatalf("notice = %+v, want waiting with the next attempt 15s later", notice)
	}
	if len(store.attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(store.attempts))
	}
	attempt := store.attempts[0]
	if attempt.Manual || attempt.HTTPStatus != nil || attempt.Error != "dial tcp: connection refused" || attempt.Accepted {
		t.Fatalf("attempt = %+v, want a connection failure with no HTTP status", attempt)
	}
}

func TestDeliverDueRetriesOnTheFixedScheduleThenStops(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, waitingPayment(now))
	poster := &scriptPoster{status: 503}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))

	gaps := []time.Duration{
		15 * time.Second,
		15 * time.Second,
		30 * time.Second,
		3 * time.Minute,
		30 * time.Minute,
		30 * time.Minute,
		30 * time.Minute,
		60 * time.Minute,
	}
	for i, gap := range gaps {
		if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		notice := store.notice(11)
		if notice.Status != payentity.NoticeWaiting || notice.AttemptCount != i+1 || !notice.NextAttemptAt.Equal(now.Add(gap)) {
			t.Fatalf("after attempt %d notice = %+v, want next in %s", i+1, notice, gap)
		}
		if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now.Add(gap - time.Second)}); err != nil {
			t.Fatalf("early after attempt %d: %v", i+1, err)
		}
		if len(poster.posts) != i+1 {
			t.Fatalf("posts = %d, want %d before the gap elapses", len(poster.posts), i+1)
		}
		now = now.Add(gap)
	}
	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("ninth attempt: %v", err)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeExhausted || notice.AttemptCount != 9 || !notice.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want exhausted after nine attempts", notice)
	}
	if len(store.attempts) != 9 {
		t.Fatalf("attempts = %d, want 9", len(store.attempts))
	}
	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now.Add(24 * time.Hour)}); err != nil {
		t.Fatalf("after exhaustion: %v", err)
	}
	if len(poster.posts) != 9 {
		t.Fatalf("posts = %d, want automatic delivery to stop", len(poster.posts))
	}
}

func TestDeliverDueAcceptanceStopsARetryingNotice(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	notice := waitingPayment(now)
	notice.AttemptCount = 4
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, notice)
	poster := &scriptPoster{status: 204}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	got := store.notice(11)
	if got.Status != payentity.NoticeAccepted || got.AttemptCount != 5 || !got.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want accepted and off the schedule", got)
	}
	if len(store.attempts) != 1 || store.attempts[0].HTTPStatus == nil || *store.attempts[0].HTTPStatus != 204 || !store.attempts[0].Accepted {
		t.Fatalf("attempt = %+v, want HTTP 204 accepted", store.attempts[0])
	}
	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("second deliver: %v", err)
	}
	if len(poster.posts) != 1 {
		t.Fatalf("posts = %d, want the schedule to stop", len(poster.posts))
	}
}

func TestDeliverDueStoreFaultDoesNotFailTheCaller(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{saveErr: errors.New("db down")}
	store.notices = append(store.notices, waitingPayment(now))
	logs := &logCapture{}
	svc := NewDelivery(store, &scriptPoster{status: 200}, slog.New(logs))

	n, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now})
	if err != nil || n != 0 {
		t.Fatalf("deliver = (%d, %v), want a recorded fault to leave the caller successful", n, err)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeWaiting || notice.AttemptCount != 0 || !notice.NextAttemptAt.Equal(now.Add(15*time.Second)) {
		t.Fatalf("notice = %+v, want the claim kept and the attempt uncounted", notice)
	}
	if len(store.attempts) != 0 {
		t.Fatalf("attempts = %d, want none", len(store.attempts))
	}
	if logs.warns != 1 || logs.errors != 0 {
		t.Fatalf("warns = %d errors = %d, want the store fault logged once", logs.warns, logs.errors)
	}
}

func TestDeliverDueStopsWhenTheRunIsCanceled(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, waitingPayment(now))
	poster := &scriptPoster{status: 200}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.DeliverDue(ctx, DeliverDueRequest{Now: now})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("deliver err = %v, want context.Canceled", err)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeWaiting || notice.AttemptCount != 0 || !notice.NextAttemptAt.Equal(now) {
		t.Fatalf("notice = %+v, want the attempt left unburned", notice)
	}
	if len(store.attempts) != 0 || len(poster.posts) != 0 {
		t.Fatalf("attempts = %d posts = %d, want none", len(store.attempts), len(poster.posts))
	}
}

func TestDeliverDueOneScanTakesTheNotice(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, waitingPayment(now))
	started := make(chan struct{})
	release := make(chan struct{})
	var claimed sync.Mutex
	poster := &scriptPoster{
		status: 500,
		before: func() {
			if !claimed.TryLock() {
				return
			}
			close(started)
			<-release
			claimed.Unlock()
		},
	}
	svc := NewDelivery(store, poster, slog.New(slog.DiscardHandler))
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now})
		errCh <- err
	}()
	<-started
	if _, err := svc.DeliverDue(context.Background(), DeliverDueRequest{Now: now}); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	poster.mu.Lock()
	calls := poster.calls
	poster.mu.Unlock()
	if calls != 1 {
		t.Fatalf("posts = %d, want the first scan to take the notice", calls)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("first scan: %v", err)
	}
}

func waitingPayment(due time.Time) *payentity.BusinessNotice {
	return &payentity.BusinessNotice{
		ID:            11,
		ApplicationID: 7,
		Kind:          payentity.NoticeKindPayment,
		PaymentID:     42,
		BizNo:         "ORDER-9",
		NotifyURL:     "https://caller.example/paid-at-creation",
		Status:        payentity.NoticeWaiting,
		NextAttemptAt: due,
	}
}

// memDeliveryNotices is the in-memory notice store for delivery tests.
type memDeliveryNotices struct {
	notices  []*payentity.BusinessNotice
	attempts []*payentity.BusinessNoticeAttempt
	nextID   int64
	saveErr  error
}

func (m *memDeliveryNotices) Insert(context.Context, *payentity.BusinessNotice) (bool, error) {
	return false, nil
}

func (m *memDeliveryNotices) NextID() int64 {
	m.nextID++
	return 800 + m.nextID
}

func (m *memDeliveryNotices) ListDue(_ context.Context, req payrepo.ListDueNoticesRequest) ([]*payentity.BusinessNotice, error) {
	var due []*payentity.BusinessNotice
	for _, n := range m.notices {
		if n.Status != payentity.NoticeWaiting || n.NextAttemptAt.IsZero() || n.NextAttemptAt.After(req.Now) {
			continue
		}
		cp := *n
		due = append(due, &cp)
	}
	if req.Limit > 0 && len(due) > req.Limit {
		due = due[:req.Limit]
	}
	return due, nil
}

func (m *memDeliveryNotices) ClaimDueNotice(_ context.Context, req payrepo.ClaimDueNoticeRequest) (bool, error) {
	n := m.notice(req.ID)
	if n == nil || n.Status != payentity.NoticeWaiting || n.NextAttemptAt.IsZero() || n.NextAttemptAt.After(req.Now) {
		return false, nil
	}
	n.NextAttemptAt = req.Until
	return true, nil
}

func (m *memDeliveryNotices) FindByID(_ context.Context, id int64) (*payentity.BusinessNotice, error) {
	n := m.notice(id)
	if n == nil {
		return nil, pay.ErrBusinessNoticeNotFound
	}
	cp := *n
	return &cp, nil
}

func (m *memDeliveryNotices) RecordManualAttempt(_ context.Context, req payrepo.RecordManualAttemptRequest) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	n := m.notice(req.Notice.ID)
	if n == nil || n.Status != payentity.NoticeExhausted {
		return pay.ErrBusinessNoticeNotExhausted
	}
	n.Status = req.Notice.Status
	cp := *req.Attempt
	m.attempts = append(m.attempts, &cp)
	return nil
}

func (m *memDeliveryNotices) ListExhausted(_ context.Context, q listq.Query) ([]*payrepo.ExhaustedBusinessNotice, error) {
	var rows []*payrepo.ExhaustedBusinessNotice
	for _, n := range m.notices {
		if n.Status != payentity.NoticeExhausted {
			continue
		}
		cp := *n
		item := &payrepo.ExhaustedBusinessNotice{Notice: &cp}
		var last *payentity.BusinessNoticeAttempt
		for _, attempt := range m.attempts {
			if attempt.NoticeID != n.ID {
				continue
			}
			if last != nil && (attempt.CreatedAt.Before(last.CreatedAt) || (attempt.CreatedAt.Equal(last.CreatedAt) && attempt.ID <= last.ID)) {
				continue
			}
			ac := *attempt
			last = &ac
		}
		item.Last = last
		rows = append(rows, item)
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Notice.ID > rows[j-1].Notice.ID; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	if q.Offset > 0 {
		if q.Offset >= len(rows) {
			return nil, nil
		}
		rows = rows[q.Offset:]
	}
	if q.Limit > 0 && len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

func (m *memDeliveryNotices) RecordAutomaticAttempt(_ context.Context, req payrepo.RecordAutomaticAttemptRequest) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	n := m.notice(req.Notice.ID)
	if n == nil {
		return errDeliveryTestMissing
	}
	n.Status = req.Notice.Status
	n.AttemptCount = req.Notice.AttemptCount
	n.NextAttemptAt = req.Notice.NextAttemptAt
	cp := *req.Attempt
	m.attempts = append(m.attempts, &cp)
	return nil
}

func (m *memDeliveryNotices) notice(id int64) *payentity.BusinessNotice {
	for _, n := range m.notices {
		if n.ID == id {
			return n
		}
	}
	return nil
}

var _ payrepo.BusinessNoticeRepo = (*memDeliveryNotices)(nil)

type scriptPoster struct {
	status int
	err    error
	before func()
	mu     sync.Mutex
	calls  int
	posts  []NoticePost
}

func (p *scriptPoster) Post(_ context.Context, req NoticePost) (NoticePostResult, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.before != nil {
		p.before()
	}
	p.mu.Lock()
	p.posts = append(p.posts, NoticePost{URL: req.URL, Body: append([]byte(nil), req.Body...)})
	p.mu.Unlock()
	if p.err != nil {
		return NoticePostResult{}, p.err
	}
	return NoticePostResult{StatusCode: p.status}, nil
}

type logCapture struct {
	warns  int
	errors int
}

func (h *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *logCapture) Handle(_ context.Context, r slog.Record) error {
	switch {
	case r.Level >= slog.LevelError:
		h.errors++
	case r.Level >= slog.LevelWarn:
		h.warns++
	}
	return nil
}

func (h *logCapture) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *logCapture) WithGroup(string) slog.Handler { return h }

type deliveryTestError struct{ msg string }

func (e deliveryTestError) Error() string { return e.msg }

var errDeliveryTestMissing = deliveryTestError{msg: "notice missing"}
