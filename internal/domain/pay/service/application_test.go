package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
)

// fakePayApplicationRepo is the in-memory Pay Application store the domain
// service tests drive. Display-name uniqueness matches the partial unique
// index: only rows that are not withdrawn hold the name.
type fakePayApplicationRepo struct {
	m       map[int64]*payentity.PayApplication
	deleted map[int64]bool
	nextID  int64
}

func newFakePayApplicationRepo() *fakePayApplicationRepo {
	return &fakePayApplicationRepo{
		m:       map[int64]*payentity.PayApplication{},
		deleted: map[int64]bool{},
		nextID:  800,
	}
}

func (f *fakePayApplicationRepo) NextID() int64 { f.nextID++; return f.nextID }

func (f *fakePayApplicationRepo) nameTaken(name string, exceptID int64) bool {
	for id, app := range f.m {
		if f.deleted[id] || id == exceptID {
			continue
		}
		if app.Name == name {
			return true
		}
	}
	return false
}

func (f *fakePayApplicationRepo) FindByID(ctx context.Context, id int64) (*payentity.PayApplication, error) {
	if f.deleted[id] {
		return nil, pay.ErrApplicationNotFound
	}
	app, ok := f.m[id]
	if !ok {
		return nil, pay.ErrApplicationNotFound
	}
	cp := *app
	return &cp, nil
}

func (f *fakePayApplicationRepo) FindByIDAny(ctx context.Context, id int64) (*payentity.PayApplication, error) {
	app, ok := f.m[id]
	if !ok {
		return nil, pay.ErrApplicationNotFound
	}
	cp := *app
	return &cp, nil
}

func (f *fakePayApplicationRepo) Create(ctx context.Context, app *payentity.PayApplication) (*payentity.PayApplication, error) {
	if f.nameTaken(app.Name, 0) {
		return nil, pay.ErrApplicationDuplicate
	}
	cp := *app
	f.m[app.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakePayApplicationRepo) Update(ctx context.Context, req payrepo.UpdatePayApplicationRequest) (*payentity.PayApplication, error) {
	app := req.App
	if _, ok := f.m[app.ID]; !ok || f.deleted[app.ID] {
		return nil, pay.ErrApplicationNotFound
	}
	if f.nameTaken(app.Name, app.ID) {
		return nil, pay.ErrApplicationDuplicate
	}
	cp := *app
	cp.UpdateBy = req.ActorID
	f.m[app.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakePayApplicationRepo) UpdateStatus(ctx context.Context, req payrepo.UpdatePayApplicationStatusRequest) error {
	app, ok := f.m[req.ID]
	if !ok || f.deleted[req.ID] {
		return pay.ErrApplicationNotFound
	}
	app.Status = req.Status
	app.UpdateBy = req.ActorID
	return nil
}

func (f *fakePayApplicationRepo) List(ctx context.Context, q listq.Query) ([]*payentity.PayApplication, error) {
	out := make([]*payentity.PayApplication, 0, len(f.m))
	for id, app := range f.m {
		if f.deleted[id] {
			continue
		}
		cp := *app
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakePayApplicationRepo) Delete(ctx context.Context, req payrepo.DeletePayApplicationRequest) error {
	if _, ok := f.m[req.ID]; !ok || f.deleted[req.ID] {
		return pay.ErrApplicationNotFound
	}
	f.deleted[req.ID] = true
	f.m[req.ID].UpdateBy = req.ActorID
	return nil
}

func TestPayApplicationCreateBindsEnabledMerchant(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))

	got, err := svc.Create(context.Background(), CreateApplicationRequest{
		Name:       "收银台",
		MerchantID: 42,
		ActorID:    7,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.ID == 0 || got.Name != "收银台" || got.MerchantID != 42 || got.Status != payentity.ApplicationEnabled || got.CreateBy != 7 || got.UpdateBy != 7 {
		t.Fatalf("created = %+v, want a new enabled Pay Application bound to merchant 42", got)
	}
}

func TestPayApplicationCreateRefusesDisabledMerchant(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantDisabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))

	_, err := svc.Create(context.Background(), CreateApplicationRequest{
		Name: "收银台", MerchantID: 42, ActorID: 7,
	})
	if !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
}

func TestPayApplicationCreateRefusesDuplicateDisplayName(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	if _, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if !errors.Is(err, pay.ErrApplicationDuplicate) {
		t.Fatalf("err = %v, want ErrApplicationDuplicate", err)
	}
}

func TestPayApplicationCreateRejectsBlankOrLongName(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	if _, err := svc.Create(ctx, CreateApplicationRequest{Name: "", MerchantID: 42, ActorID: 7}); !errors.Is(err, pay.ErrApplicationInvalid) {
		t.Fatalf("blank name err = %v, want ErrApplicationInvalid", err)
	}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := svc.Create(ctx, CreateApplicationRequest{Name: string(long), MerchantID: 42, ActorID: 7}); !errors.Is(err, pay.ErrApplicationInvalid) {
		t.Fatalf("long name err = %v, want ErrApplicationInvalid", err)
	}
}

func TestPayApplicationRenameKeepsNumber(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	created, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := svc.Update(ctx, UpdateApplicationRequest{
		ID: created.ID, Name: "新收银台", ActorID: 8,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != created.ID || updated.Name != "新收银台" || updated.MerchantID != 42 || updated.UpdateBy != 8 {
		t.Fatalf("updated = %+v, want same number bound to merchant 42", updated)
	}
}

func TestPayApplicationRenameRefusesLiveDisplayName(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	first, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.Create(ctx, CreateApplicationRequest{Name: "另一家", MerchantID: 42, ActorID: 7}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := svc.Update(ctx, UpdateApplicationRequest{ID: first.ID, Name: "收银台", ActorID: 8}); err != nil {
		t.Fatalf("keeping its own name: %v", err)
	}
	_, err = svc.Update(ctx, UpdateApplicationRequest{ID: first.ID, Name: "另一家", ActorID: 8})
	if !errors.Is(err, pay.ErrApplicationDuplicate) {
		t.Fatalf("err = %v, want ErrApplicationDuplicate", err)
	}
}

func TestPayApplicationAddressesStartEmptyAndEitherChanges(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	created, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.OrderNotifyURL != "" || created.RefundNotifyURL != "" {
		t.Fatalf("addresses = %q %q, want both empty", created.OrderNotifyURL, created.RefundNotifyURL)
	}
	withOrder, err := svc.Update(ctx, UpdateApplicationRequest{
		ID: created.ID, Name: created.Name, OrderNotifyURL: "https://pay.example/paid", ActorID: 8,
	})
	if err != nil {
		t.Fatalf("set payment address: %v", err)
	}
	if withOrder.OrderNotifyURL != "https://pay.example/paid" || withOrder.RefundNotifyURL != "" || withOrder.MerchantID != 42 {
		t.Fatalf("after payment address = %+v", withOrder)
	}
	withBoth, err := svc.Update(ctx, UpdateApplicationRequest{
		ID: created.ID, Name: created.Name,
		OrderNotifyURL:  "https://pay.example/paid",
		RefundNotifyURL: "https://pay.example/refunded",
		ActorID:         8,
	})
	if err != nil {
		t.Fatalf("set refund address: %v", err)
	}
	if withBoth.OrderNotifyURL != "https://pay.example/paid" || withBoth.RefundNotifyURL != "https://pay.example/refunded" || withBoth.ID != created.ID {
		t.Fatalf("after refund address = %+v", withBoth)
	}
}

func TestPayApplicationRejectsOverlongAddress(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	long := strings.Repeat("a", 513)
	_, err := svc.Create(ctx, CreateApplicationRequest{
		Name: "收银台", MerchantID: 42, OrderNotifyURL: long, ActorID: 7,
	})
	if !errors.Is(err, pay.ErrApplicationInvalid) {
		t.Fatalf("create err = %v, want ErrApplicationInvalid", err)
	}
	created, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = svc.Update(ctx, UpdateApplicationRequest{
		ID: created.ID, Name: created.Name, RefundNotifyURL: long, ActorID: 8,
	})
	if !errors.Is(err, pay.ErrApplicationInvalid) {
		t.Fatalf("update err = %v, want ErrApplicationInvalid", err)
	}
}

func TestPayApplicationDisable(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	created, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.ChangeStatus(ctx, ChangeApplicationStatusRequest{
		ID: created.ID, Status: payentity.ApplicationDisabled, ActorID: 9,
	})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if got.ID != created.ID || got.Status != payentity.ApplicationDisabled || got.MerchantID != 42 || got.UpdateBy != 9 {
		t.Fatalf("disabled = %+v", got)
	}
	if _, err := svc.ChangeStatus(ctx, ChangeApplicationStatusRequest{
		ID: created.ID, Status: payentity.ApplicationStatus(9), ActorID: 9,
	}); !errors.Is(err, pay.ErrApplicationInvalid) {
		t.Fatalf("err = %v, want ErrApplicationInvalid", err)
	}
}

func TestPayApplicationWithdrawFreesDisplayName(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	created, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Withdraw(ctx, WithdrawApplicationRequest{ID: created.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, pay.ErrApplicationNotFound) {
		t.Fatalf("err = %v, want ErrApplicationNotFound", err)
	}
	again, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("reuse name: %v", err)
	}
	if again.ID == 0 || again.ID == created.ID || again.Name != "收银台" || again.MerchantID != 42 {
		t.Fatalf("reused = %+v, want a new Pay Application", again)
	}
}

func TestPayApplicationCreateRefusesWithdrawnMerchant(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	merchants.deleted[42] = true
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	_, err := svc.Create(context.Background(), CreateApplicationRequest{
		Name: "收银台", MerchantID: 42, ActorID: 7,
	})
	if !errors.Is(err, pay.ErrMerchantNotFound) {
		t.Fatalf("err = %v, want ErrMerchantNotFound", err)
	}
}

func TestPayApplicationSeveralBindOneMerchant(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	first, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.Create(ctx, CreateApplicationRequest{Name: "另一家", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID == second.ID || first.MerchantID != 42 || second.MerchantID != 42 {
		t.Fatalf("first=%+v second=%+v, want two Pay Applications on merchant 42", first, second)
	}
}

func TestPayApplicationListOmitsWithdrawn(t *testing.T) {
	merchants := newFakeMerchantRepo(nil)
	merchants.m[42] = &payentity.MerchantAccount{
		ID: 42, Name: "主店", Status: payentity.MerchantEnabled,
	}
	svc := NewApplicationService(newFakePayApplicationRepo(), merchants, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	gone, err := svc.Create(ctx, CreateApplicationRequest{Name: "收银台", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	kept, err := svc.Create(ctx, CreateApplicationRequest{Name: "另一家", MerchantID: 42, ActorID: 7})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := svc.Withdraw(ctx, WithdrawApplicationRequest{ID: gone.ID, ActorID: 9}); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	listed, err := svc.List(ctx, listq.Query{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != kept.ID {
		t.Fatalf("listed = %+v, want only %d", listed, kept.ID)
	}
}
