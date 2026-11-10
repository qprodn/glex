package payinfra

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	pay "glex/internal/domain/pay"
	payservice "glex/internal/domain/pay/service"
)

type fakeDelivery struct {
	n   int
	err error
	now time.Time
	ran int
}

func (f *fakeDelivery) DeliverDue(_ context.Context, req payservice.DeliverDueRequest) (int, error) {
	f.ran++
	f.now = req.Now
	if f.err != nil {
		return 0, f.err
	}
	return f.n, nil
}

func TestNoticeDeliverPeriodIsWithinFifteenSeconds(t *testing.T) {
	sched, err := cron.ParseStandard(NoticeDeliverCron)
	if err != nil {
		t.Fatalf("parse cron: %v", err)
	}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	gap := sched.Next(start).Sub(start)
	if gap <= 0 || gap > 15*time.Second {
		t.Fatalf("period = %s, want no coarser than 15s", gap)
	}
	body, err := os.ReadFile("../../../docs/db/seed.sql")
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	want := "'" + NoticeDeliverHandler + "', '" + NoticeDeliverCron + "'"
	if !bytes.Contains(body, []byte(want)) {
		t.Fatalf("seed is missing %s", want)
	}
}

func TestNoticeDeliverExecutorName(t *testing.T) {
	e := NewNoticeDeliverExecutor(&fakeDelivery{})
	if e.Name() != "pay.notice-deliver" {
		t.Fatalf("name = %q", e.Name())
	}
}

func TestNoticeDeliverExecutorRunSummarizes(t *testing.T) {
	fake := &fakeDelivery{n: 4}
	e := NewNoticeDeliverExecutor(fake)
	summary, err := e.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary != "attempted 4 business notice(s)" {
		t.Fatalf("summary = %q", summary)
	}
	if fake.ran != 1 || fake.now.IsZero() {
		t.Fatalf("delivery ran = %d now = %v", fake.ran, fake.now)
	}
}

func TestNoticeDeliverExecutorRunPropagatesError(t *testing.T) {
	e := NewNoticeDeliverExecutor(&fakeDelivery{err: pay.ErrPayStoreUnavailable})
	if _, err := e.Run(context.Background(), ""); !errors.Is(err, pay.ErrPayStoreUnavailable) {
		t.Fatalf("err = %v, want ErrPayStoreUnavailable", err)
	}
}
