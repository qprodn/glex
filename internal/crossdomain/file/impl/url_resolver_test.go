package impl

import (
	"context"
	"testing"

	"glex/internal/domain/file/entity"
	fileservice "glex/internal/domain/file/service"
)

// recordingFileService records the resolve asks; the embedded interface
// stays nil because the adapter under test touches nothing else.
type recordingFileService struct {
	fileservice.File
	askedIDs []int64
	files    []*entity.StoredFile
	urls     map[int64]string
	err      error
}

func (s *recordingFileService) BatchGet(_ context.Context, ids []int64) ([]*entity.StoredFile, error) {
	s.askedIDs = ids
	if s.err != nil {
		return nil, s.err
	}
	return s.files, nil
}

func (s *recordingFileService) DisplayURL(_ context.Context, f *entity.StoredFile) (string, error) {
	return s.urls[f.ID], nil
}

func TestURLResolverDelegates(t *testing.T) {
	files := &recordingFileService{
		files: []*entity.StoredFile{{ID: 1, ObjKey: "a"}, {ID: 2, ObjKey: "b"}},
		urls:  map[int64]string{1: "https://u/1", 2: "https://u/2"},
	}
	resolver := NewURLResolver(files, testLogger())

	urls, err := resolver.ResolveURLs(t.Context(), []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("ResolveURLs: %v", err)
	}
	if len(urls) != 2 || urls[1] != "https://u/1" || urls[2] != "https://u/2" {
		t.Fatalf("urls = %v, want both known ids mapped", urls)
	}
}

func TestURLResolverPropagatesLookupFailure(t *testing.T) {
	files := &recordingFileService{err: context.DeadlineExceeded}
	resolver := NewURLResolver(files, testLogger())

	if _, err := resolver.ResolveURLs(t.Context(), []int64{1}); err == nil {
		t.Fatal("row-lookup failures propagate; only per-id signing degrades")
	}
}
