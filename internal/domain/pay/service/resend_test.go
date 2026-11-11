package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	"glex/pkg/listq"
)

// Manual resend is tested at the pay domain service. The notice store is
// in memory and the poster is scripted. The address under test is the Pay
// Application's current result address, not the one copied onto the notice.

func TestResendExhaustedPaymentNoticePostsTheCurrentOrderAddress(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, &payentity.BusinessNotice{
		ID:            11,
		ApplicationID: 7,
		Kind:          payentity.NoticeKindPayment,
		PaymentID:     42,
		BizNo:         "ORDER-9",
		NotifyURL:     "https://caller.example/paid-at-creation",
		Status:        payentity.NoticeExhausted,
		AttemptCount:  9,
	})
	failed := 500
	store.attempts = append(store.attempts, &payentity.BusinessNoticeAttempt{
		ID:         1,
		NoticeID:   11,
		HTTPStatus: &failed,
		CreatedAt:  now.Add(-time.Hour),
	})
	apps := newFakePayApplicationRepo()
	apps.m[7] = &payentity.PayApplication{
		ID:              7,
		Status:          payentity.ApplicationEnabled,
		OrderNotifyURL:  "https://caller.example/paid-now",
		RefundNotifyURL: "https://caller.example/refund-now",
	}
	poster := &scriptPoster{status: 200}
	svc := NewNoticeDesk(store, apps, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now}); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if len(poster.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(poster.posts))
	}
	post := poster.posts[0]
	if post.URL != "https://caller.example/paid-now" {
		t.Fatalf("url = %q, want the Pay Application's current payment result address", post.URL)
	}
	if string(post.Body) != `{"biz_no":"ORDER-9","payment_id":42}` {
		t.Fatalf("body = %s, want payment fields", post.Body)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeAccepted || notice.AttemptCount != 9 || !notice.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want accepted with the automatic count and schedule unchanged", notice)
	}
	if len(store.attempts) != 2 {
		t.Fatalf("attempts = %d, want the earlier row kept and one manual row added", len(store.attempts))
	}
	attempt := store.attempts[1]
	if attempt.NoticeID != 11 || !attempt.Manual || attempt.HTTPStatus == nil || *attempt.HTTPStatus != 200 ||
		attempt.Error != "" || !attempt.Accepted {
		t.Fatalf("attempt = %+v, want a manual HTTP 200 accepted", attempt)
	}
}

func TestResendExhaustedRefundNoticePostsTheCurrentRefundAddress(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, exhaustedNotice(12, payentity.NoticeKindRefund))
	apps := resendApps("https://caller.example/paid-now", "https://caller.example/refund-now")
	poster := &scriptPoster{status: 200}
	svc := NewNoticeDesk(store, apps, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 12, Now: now}); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if len(poster.posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(poster.posts))
	}
	post := poster.posts[0]
	if post.URL != "https://caller.example/refund-now" {
		t.Fatalf("url = %q, want the Pay Application's current refund result address", post.URL)
	}
	if string(post.Body) != `{"refund_id":77,"payment_id":42}` {
		t.Fatalf("body = %s, want refund fields", post.Body)
	}
	notice := store.notice(12)
	if notice.Status != payentity.NoticeAccepted || notice.AttemptCount != 9 || !notice.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want accepted with the automatic count unchanged", notice)
	}
	if len(store.attempts) != 1 || !store.attempts[0].Manual || !store.attempts[0].Accepted {
		t.Fatalf("attempt = %+v, want one manual accepted row", store.attempts)
	}
}

func TestResendRefusesAnEmptyCurrentAddress(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices,
		exhaustedNotice(11, payentity.NoticeKindPayment),
		exhaustedNotice(12, payentity.NoticeKindRefund),
	)
	apps := resendApps("", "https://caller.example/refund-now")
	poster := &scriptPoster{status: 200}
	logs := &logCapture{}
	svc := NewNoticeDesk(store, apps, poster, slog.New(logs))

	_, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now})
	if !errors.Is(err, pay.ErrBusinessNoticeAddressEmpty) {
		t.Fatalf("payment resend err = %v, want empty address", err)
	}
	apps.m[7].OrderNotifyURL = "https://caller.example/paid-now"
	apps.m[7].RefundNotifyURL = ""
	_, err = svc.Resend(context.Background(), ResendNoticeRequest{ID: 12, Now: now})
	if !errors.Is(err, pay.ErrBusinessNoticeAddressEmpty) {
		t.Fatalf("refund resend err = %v, want empty address", err)
	}
	if len(poster.posts) != 0 || len(store.attempts) != 0 {
		t.Fatalf("posts = %d attempts = %d, want no delivery", len(poster.posts), len(store.attempts))
	}
	for _, id := range []int64{11, 12} {
		notice := store.notice(id)
		if notice.Status != payentity.NoticeExhausted || notice.AttemptCount != 9 || !notice.NextAttemptAt.IsZero() {
			t.Fatalf("notice %d = %+v, want exhausted and unscheduled", id, notice)
		}
	}
	if logs.warns != 2 || logs.errors != 0 {
		t.Fatalf("warns = %d errors = %d, want a business rejection", logs.warns, logs.errors)
	}
}

func TestResendRefusesWaitingAndAcceptedNotices(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	waiting := exhaustedNotice(11, payentity.NoticeKindPayment)
	waiting.Status = payentity.NoticeWaiting
	waiting.NextAttemptAt = now.Add(15 * time.Second)
	accepted := exhaustedNotice(12, payentity.NoticeKindPayment)
	accepted.Status = payentity.NoticeAccepted
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, waiting, accepted)
	poster := &scriptPoster{status: 200}
	logs := &logCapture{}
	svc := NewNoticeDesk(store, resendApps("https://caller.example/paid-now", "https://caller.example/refund-now"), poster, slog.New(logs))

	for _, id := range []int64{11, 12} {
		_, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: id, Now: now})
		if !errors.Is(err, pay.ErrBusinessNoticeNotExhausted) {
			t.Fatalf("resend %d err = %v, want not exhausted", id, err)
		}
	}
	if len(poster.posts) != 0 || len(store.attempts) != 0 {
		t.Fatalf("posts = %d attempts = %d, want no delivery", len(poster.posts), len(store.attempts))
	}
	if store.notice(11).Status != payentity.NoticeWaiting || !store.notice(11).NextAttemptAt.Equal(now.Add(15*time.Second)) {
		t.Fatalf("waiting notice = %+v, want the automatic schedule left in place", store.notice(11))
	}
	if store.notice(12).Status != payentity.NoticeAccepted {
		t.Fatalf("accepted notice = %+v, want it to stay accepted", store.notice(12))
	}
	if logs.warns != 2 || logs.errors != 0 {
		t.Fatalf("warns = %d errors = %d, want a business rejection", logs.warns, logs.errors)
	}
}

func TestResendFailureLeavesTheNoticeExhausted(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	store.notices = append(store.notices, exhaustedNotice(11, payentity.NoticeKindPayment))
	poster := &scriptPoster{status: 302}
	logs := &logCapture{}
	svc := NewNoticeDesk(store, resendApps("https://caller.example/paid-now", ""), poster, slog.New(logs))

	outcomes := []struct {
		status int
		err    error
	}{
		{status: 302},
		{status: 500},
		{err: errors.New("dial tcp: connection refused")},
		{err: errors.New("context deadline exceeded")},
	}
	for i, outcome := range outcomes {
		poster.status = outcome.status
		poster.err = outcome.err
		if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatalf("resend %d: %v, want a failed attempt to stay a business outcome", i+1, err)
		}
		notice := store.notice(11)
		if notice.Status != payentity.NoticeExhausted || notice.AttemptCount != 9 || !notice.NextAttemptAt.IsZero() {
			t.Fatalf("after outcome %d notice = %+v, want exhausted with the automatic schedule finished", i+1, notice)
		}
	}
	if len(poster.posts) != len(outcomes) || len(store.attempts) != len(outcomes) {
		t.Fatalf("posts = %d attempts = %d, want one row per call", len(poster.posts), len(store.attempts))
	}
	for i, outcome := range outcomes {
		attempt := store.attempts[i]
		if !attempt.Manual || attempt.Accepted || attempt.NoticeID != 11 {
			t.Fatalf("attempt %d = %+v, want a manual failure", i+1, attempt)
		}
		if outcome.err != nil {
			if attempt.HTTPStatus != nil || attempt.Error != outcome.err.Error() {
				t.Fatalf("attempt %d = %+v, want the transport error and no HTTP status", i+1, attempt)
			}
			continue
		}
		if attempt.HTTPStatus == nil || *attempt.HTTPStatus != outcome.status || attempt.Error != "" {
			t.Fatalf("attempt %d = %+v, want HTTP %d", i+1, attempt, outcome.status)
		}
	}
	if logs.warns != 0 || logs.errors != 0 {
		t.Fatalf("warns = %d errors = %d, want a failed attempt not to fail the operation", logs.warns, logs.errors)
	}

	poster.err = nil
	poster.status = 204
	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now.Add(time.Hour)}); err != nil {
		t.Fatalf("accepting resend: %v", err)
	}
	notice := store.notice(11)
	if notice.Status != payentity.NoticeAccepted || notice.AttemptCount != 9 || !notice.NextAttemptAt.IsZero() {
		t.Fatalf("notice = %+v, want accepted without restarting the automatic schedule", notice)
	}
	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now.Add(2 * time.Hour)}); !errors.Is(err, pay.ErrBusinessNoticeNotExhausted) {
		t.Fatalf("resend after acceptance err = %v, want refused", err)
	}
	if len(poster.posts) != len(outcomes)+1 {
		t.Fatalf("posts = %d, want the refused call not to deliver", len(poster.posts))
	}
}

func TestResendWorksWhenThePayApplicationIsDisabledOrWithdrawn(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	disabledNotice := exhaustedNotice(11, payentity.NoticeKindPayment)
	disabledNotice.ApplicationID = 7
	withdrawnNotice := exhaustedNotice(12, payentity.NoticeKindRefund)
	withdrawnNotice.ApplicationID = 8
	store.notices = append(store.notices, disabledNotice, withdrawnNotice)
	apps := newFakePayApplicationRepo()
	apps.m[7] = &payentity.PayApplication{
		ID:              7,
		Status:          payentity.ApplicationDisabled,
		OrderNotifyURL:  "https://caller.example/disabled-paid",
		RefundNotifyURL: "https://caller.example/disabled-refund",
	}
	apps.m[8] = &payentity.PayApplication{
		ID:              8,
		Status:          payentity.ApplicationEnabled,
		OrderNotifyURL:  "https://caller.example/withdrawn-paid",
		RefundNotifyURL: "https://caller.example/withdrawn-refund",
	}
	apps.deleted[8] = true
	poster := &scriptPoster{status: 200}
	svc := NewNoticeDesk(store, apps, poster, slog.New(slog.DiscardHandler))

	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now}); err != nil {
		t.Fatalf("disabled resend: %v", err)
	}
	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 12, Now: now}); err != nil {
		t.Fatalf("withdrawn resend: %v", err)
	}
	if len(poster.posts) != 2 {
		t.Fatalf("posts = %d, want 2", len(poster.posts))
	}
	if poster.posts[0].URL != "https://caller.example/disabled-paid" {
		t.Fatalf("disabled url = %q, want the current payment result address", poster.posts[0].URL)
	}
	if poster.posts[1].URL != "https://caller.example/withdrawn-refund" {
		t.Fatalf("withdrawn url = %q, want the current refund result address", poster.posts[1].URL)
	}
	if store.notice(11).Status != payentity.NoticeAccepted || store.notice(12).Status != payentity.NoticeAccepted {
		t.Fatalf("statuses = %v %v, want both accepted", store.notice(11).Status, store.notice(12).Status)
	}
	if store.notice(11).AttemptCount != 9 || store.notice(12).AttemptCount != 9 {
		t.Fatalf("counts = %d %d, want the automatic counts unchanged", store.notice(11).AttemptCount, store.notice(12).AttemptCount)
	}
}

func TestListExhaustedNoticesKeepsSuccessfulAttemptsOffTheQueue(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	store := &memDeliveryNotices{}
	waiting := exhaustedNotice(12, payentity.NoticeKindPayment)
	waiting.Status = payentity.NoticeWaiting
	waiting.PaymentID = 43
	waiting.AttemptCount = 2
	waiting.NextAttemptAt = now.Add(30 * time.Second)
	accepted := exhaustedNotice(13, payentity.NoticeKindRefund)
	accepted.Status = payentity.NoticeAccepted
	accepted.AttemptCount = 4
	store.notices = append(store.notices,
		exhaustedNotice(11, payentity.NoticeKindPayment),
		waiting,
		accepted,
	)
	failed := 500
	ok := 200
	store.attempts = append(store.attempts,
		&payentity.BusinessNoticeAttempt{
			ID: 1, NoticeID: 11, HTTPStatus: &failed, CreatedAt: now.Add(-2 * time.Hour),
		},
		&payentity.BusinessNoticeAttempt{
			ID: 2, NoticeID: 11, Manual: true, Error: "dial tcp: connection refused", CreatedAt: now.Add(-time.Minute),
		},
		&payentity.BusinessNoticeAttempt{
			ID: 3, NoticeID: 13, HTTPStatus: &ok, Accepted: true, CreatedAt: now.Add(-time.Hour),
		},
	)
	poster := &scriptPoster{status: 200}
	svc := NewNoticeDesk(store, resendApps("https://caller.example/paid-now", "https://caller.example/refund-now"), poster, slog.New(slog.DiscardHandler))

	got, err := svc.ListExhausted(context.Background(), listq.Query{Limit: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Notice.ID != 11 {
		t.Fatalf("list = %+v, want only the exhausted notice", got)
	}
	last := got[0].Last
	if last == nil || last.ID != 2 || !last.Manual || last.HTTPStatus != nil || last.Error != "dial tcp: connection refused" || last.Accepted {
		t.Fatalf("last = %+v, want the newest failed attempt", last)
	}
	if !attemptStored(store, 3) {
		t.Fatalf("attempts = %+v, want the accepted notice's successful row kept", store.attempts)
	}

	if _, err := svc.Resend(context.Background(), ResendNoticeRequest{ID: 11, Now: now}); err != nil {
		t.Fatalf("resend: %v", err)
	}
	got, err = svc.ListExhausted(context.Background(), listq.Query{Limit: 50})
	if err != nil {
		t.Fatalf("list after acceptance: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("list after acceptance = %d, want the queue to drop it", len(got))
	}
	if len(store.attempts) != 4 || !attemptStored(store, 3) {
		t.Fatalf("attempts = %d, want the successful rows kept", len(store.attempts))
	}
}

func attemptStored(store *memDeliveryNotices, id int64) bool {
	for _, attempt := range store.attempts {
		if attempt.ID == id {
			return true
		}
	}
	return false
}

func exhaustedNotice(id int64, kind payentity.NoticeKind) *payentity.BusinessNotice {
	n := &payentity.BusinessNotice{
		ID:            id,
		ApplicationID: 7,
		Kind:          kind,
		PaymentID:     42,
		BizNo:         "ORDER-9",
		NotifyURL:     "https://caller.example/copied-at-creation",
		Status:        payentity.NoticeExhausted,
		AttemptCount:  9,
	}
	if kind == payentity.NoticeKindRefund {
		n.RefundID = 77
	}
	return n
}

func resendApps(orderURL, refundURL string) *fakePayApplicationRepo {
	apps := newFakePayApplicationRepo()
	apps.m[7] = &payentity.PayApplication{
		ID:              7,
		Status:          payentity.ApplicationEnabled,
		OrderNotifyURL:  orderURL,
		RefundNotifyURL: refundURL,
	}
	return apps
}
