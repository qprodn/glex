package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"glex/internal/domain/file"
	"glex/internal/domain/file/entity"
	"glex/internal/domain/file/repository"
	"glex/internal/domain/file/storage"
)

// fakeFileRepo is an in-memory repository.FileRepo for testing the file
// domain service in isolation from the infrastructure layer.
type fakeFileRepo struct {
	byID      map[int64]*entity.StoredFile
	nextID    int64
	created   []*entity.StoredFile
	deleted   []int64
	createErr error
}

func newFakeFileRepo() *fakeFileRepo {
	return &fakeFileRepo{byID: make(map[int64]*entity.StoredFile), nextID: 1001}
}

func (r *fakeFileRepo) NextID() int64 {
	id := r.nextID
	r.nextID++
	return id
}

func (r *fakeFileRepo) FindByID(_ context.Context, id int64) (*entity.StoredFile, error) {
	f, ok := r.byID[id]
	if !ok {
		return nil, file.ErrFileNotFound
	}
	return f, nil
}

func (r *fakeFileRepo) List(context.Context, ...repository.ListOption) ([]*entity.StoredFile, error) {
	return nil, nil
}

func (r *fakeFileRepo) Create(_ context.Context, f *entity.StoredFile) (*entity.StoredFile, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	f.CreatedAt = time.Unix(0, 1).UTC()
	f.UpdatedAt = f.CreatedAt
	r.byID[f.ID] = f
	r.created = append(r.created, f)
	return f, nil
}

func (r *fakeFileRepo) Delete(_ context.Context, id, _ int64) error {
	if _, ok := r.byID[id]; !ok {
		return file.ErrFileNotFound
	}
	delete(r.byID, id)
	r.deleted = append(r.deleted, id)
	return nil
}

// fakeObjectStorage is an in-memory storage.ObjectStorage that hashes like
// the S3 implementation does.
type fakeObjectStorage struct {
	bucket    string
	put       map[string]string // key → sha256 hex
	putErr    error
	getObj    *storage.Object
	getErr    error
	lastRange *storage.ByteRange
	deleted   []string
	deleteErr error
}

func newFakeObjectStorage() *fakeObjectStorage {
	return &fakeObjectStorage{bucket: "glex-test", put: make(map[string]string)}
}

func (s *fakeObjectStorage) Bucket() string { return s.bucket }

func (s *fakeObjectStorage) EnsureBucket(context.Context) error { return nil }

func (s *fakeObjectStorage) Put(_ context.Context, key string, content io.Reader, _ int64, _ string) (string, error) {
	if s.putErr != nil {
		return "", s.putErr
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, content); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	digest := hex.EncodeToString(sum[:])
	s.put[key] = digest
	return digest, nil
}

func (s *fakeObjectStorage) Get(_ context.Context, _ string, rng *storage.ByteRange) (*storage.Object, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	s.lastRange = rng
	return s.getObj, nil
}

func (s *fakeObjectStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.deleteErr
}

func newTestService(files repository.FileRepo, store storage.ObjectStorage) File {
	return NewFileService(files, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAcceptPersistsRecordWithFingerprint(t *testing.T) {
	repo := newFakeFileRepo()
	store := newFakeObjectStorage()
	svc := newTestService(repo, store)

	created, err := svc.Accept(context.Background(),
		&entity.Upload{OriginalName: "报告.PDF", BizPath: "avatar/", MIMEType: "application/pdf", SizeBytes: 5},
		strings.NewReader("hello"), 42)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	sum := sha256.Sum256([]byte("hello"))
	wantDigest := hex.EncodeToString(sum[:])
	if created.ContentHash != wantDigest {
		t.Errorf("ContentHash = %q, want %q", created.ContentHash, wantDigest)
	}
	if created.CreateBy != 42 || created.UpdateBy != 42 {
		t.Errorf("audit actor not stamped: create_by=%d update_by=%d", created.CreateBy, created.UpdateBy)
	}
	if created.Bucket != "glex-test" {
		t.Errorf("Bucket = %q, want glex-test", created.Bucket)
	}
	// The Stored Key keeps the normalized biz prefix, the minted ID, and the
	// lowercased suffix.
	wantKey := "avatar/" + time.Now().Format("2006/01/02") + "/" + itoa(created.ID) + ".pdf"
	if created.ObjKey != wantKey {
		t.Errorf("ObjKey = %q, want %q", created.ObjKey, wantKey)
	}
	if created.Suffix != "pdf" {
		t.Errorf("Suffix = %q, want pdf", created.Suffix)
	}
	if len(repo.created) != 1 || repo.created[0].ID != created.ID {
		t.Errorf("repo created %d rows, want exactly the returned one", len(repo.created))
	}
}

func TestAcceptRejectsInvalidUploads(t *testing.T) {
	repo := newFakeFileRepo()
	store := newFakeObjectStorage()
	svc := newTestService(repo, store)

	cases := []entity.Upload{
		{OriginalName: "", MIMEType: "text/plain", SizeBytes: 5},
		{OriginalName: "a.pdf", MIMEType: "text/plain", SizeBytes: 0},
		{OriginalName: "a.pdf", BizPath: "../etc", MIMEType: "text/plain", SizeBytes: 5},
		{OriginalName: "a.pdf", BizPath: "/abs", MIMEType: "text/plain", SizeBytes: 5},
	}
	for i, up := range cases {
		if _, err := svc.Accept(context.Background(), &up, strings.NewReader("hello"), 1); !errors.Is(err, file.ErrFileInvalid) {
			t.Errorf("case %d: err = %v, want ErrFileInvalid", i, err)
		}
	}
	if len(repo.created) != 0 || len(store.put) != 0 {
		t.Errorf("invalid upload reached storage or rows: puts=%d created=%d", len(store.put), len(repo.created))
	}
}

func TestAcceptPutFailurePropagates(t *testing.T) {
	repo := newFakeFileRepo()
	store := newFakeObjectStorage()
	store.putErr = file.ErrFileStorage
	svc := newTestService(repo, store)

	if _, err := svc.Accept(context.Background(), &entity.Upload{OriginalName: "a.txt", MIMEType: "text/plain", SizeBytes: 5}, strings.NewReader("hello"), 1); !errors.Is(err, file.ErrFileStorage) {
		t.Fatalf("err = %v, want ErrFileStorage", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("a failed put must not create a row")
	}
}

func TestAcceptCreateFailureAfterPutIsAnError(t *testing.T) {
	repo := newFakeFileRepo()
	repo.createErr = errors.New("insert failed")
	store := newFakeObjectStorage()
	svc := newTestService(repo, store)

	if _, err := svc.Accept(context.Background(), &entity.Upload{OriginalName: "a.txt", MIMEType: "text/plain", SizeBytes: 5}, strings.NewReader("hello"), 1); err == nil {
		t.Fatal("row failure after a successful put must surface as an error (the object is the logged orphan)")
	}
	if len(store.put) != 1 {
		t.Errorf("bytes should have landed before the row attempt")
	}
}

func TestWithdrawRemovesRowThenBytes(t *testing.T) {
	repo := newFakeFileRepo()
	store := newFakeObjectStorage()
	svc := newTestService(repo, store)

	created, err := svc.Accept(context.Background(), &entity.Upload{OriginalName: "a.txt", MIMEType: "text/plain", SizeBytes: 5}, strings.NewReader("hello"), 7)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := svc.Withdraw(context.Background(), created.ID, 7); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, err := repo.FindByID(context.Background(), created.ID); !errors.Is(err, file.ErrFileNotFound) {
		t.Errorf("row should be gone from active reads")
	}
	if len(store.deleted) != 1 || store.deleted[0] != created.ObjKey {
		t.Errorf("bytes deleted = %v, want [%s]", store.deleted, created.ObjKey)
	}
}

func TestWithdrawSucceedsWhenByteRemovalFails(t *testing.T) {
	repo := newFakeFileRepo()
	store := newFakeObjectStorage()
	store.deleteErr = errors.New("s3 down")
	svc := newTestService(repo, store)

	created, err := svc.Accept(context.Background(), &entity.Upload{OriginalName: "a.txt", MIMEType: "text/plain", SizeBytes: 5}, strings.NewReader("hello"), 7)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	// ADR 0009: the row withdrawal stands; the orphan is logged, not failed.
	if err := svc.Withdraw(context.Background(), created.ID, 7); err != nil {
		t.Fatalf("Withdraw must succeed despite byte-removal failure: %v", err)
	}
}

func TestWithdrawUnknownID(t *testing.T) {
	svc := newTestService(newFakeFileRepo(), newFakeObjectStorage())
	if err := svc.Withdraw(context.Background(), 999, 1); !errors.Is(err, file.ErrFileNotFound) {
		t.Fatalf("err = %v, want ErrFileNotFound", err)
	}
}

func TestOpenPassesRangeThrough(t *testing.T) {
	repo := newFakeFileRepo()
	store := newFakeObjectStorage()
	store.getObj = &storage.Object{Body: io.NopCloser(strings.NewReader("hello")), ContentType: "text/plain", ContentLength: 5}
	svc := newTestService(repo, store)

	created, err := svc.Accept(context.Background(), &entity.Upload{OriginalName: "a.txt", MIMEType: "text/plain", SizeBytes: 5}, strings.NewReader("hello"), 7)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	rng := &storage.ByteRange{Header: "bytes=0-1"}
	obj, f, err := svc.Open(context.Background(), created.ID, rng)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer obj.Body.Close()
	if f.ID != created.ID {
		t.Errorf("returned file ID = %d, want %d", f.ID, created.ID)
	}
	if store.lastRange != rng {
		t.Errorf("range not passed through")
	}
}

func TestStoredKey(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if got := storedKey(123, "", "pdf", now); got != "2026/08/31/123.pdf" {
		t.Errorf("no biz path: %q", got)
	}
	if got := storedKey(123, "dict-import", "", now); got != "dict-import/2026/08/31/123" {
		t.Errorf("with biz path and no suffix: %q", got)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
