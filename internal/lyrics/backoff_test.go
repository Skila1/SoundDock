package lyrics

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func backoffService(t *testing.T, fetchErr error, fetches *atomic.Int32, logs *bytes.Buffer, clock *time.Time) *Service {
	t.Helper()
	return &Service{
		log: slog.New(slog.NewTextHandler(logs, nil)),
		listFn: func(context.Context, uuid.UUID) ([]Result, error) {
			return nil, nil
		},
		urlFn: func(context.Context) string { return "https://lrclib.net" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			fetches.Add(1)
			return "", false, fetchErr
		},
		saveFn: func(context.Context, uuid.UUID, string, string, bool) error { return nil },
		lockFn: func(context.Context, uuid.UUID) bool { return false },
		now:    func() time.Time { return *clock },
	}
}

func TestProviderServerErrorBacksOffAndLogsOnce(t *testing.T) {
	var fetches atomic.Int32
	var logs bytes.Buffer
	clock := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	s := backoffService(t, &statusError{Code: 503}, &fetches, &logs, &clock)
	meta := func() Meta { return Meta{Title: "Song", Artist: "A", TrackID: uuid.New()} }

	for i := 0; i < 5; i++ {
		s.GetLyrics(context.Background(), meta())
	}
	if fetches.Load() != 1 {
		t.Fatalf("after a 5xx no requests for the backoff, got %d fetches", fetches.Load())
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("want one warning per outage, got %d:\n%s", n, logs.String())
	}

	clock = clock.Add(providerBackoff - time.Second)
	s.GetLyrics(context.Background(), meta())
	if fetches.Load() != 1 {
		t.Fatal("still inside the backoff window")
	}

	clock = clock.Add(2 * time.Second)
	s.GetLyrics(context.Background(), meta())
	if fetches.Load() != 2 {
		t.Fatalf("backoff over: want a new request, got %d fetches", fetches.Load())
	}
}

func TestProviderClientErrorDoesNotBackOff(t *testing.T) {
	var fetches atomic.Int32
	var logs bytes.Buffer
	clock := time.Now()
	s := backoffService(t, &statusError{Code: 429}, &fetches, &logs, &clock)
	for i := 0; i < 3; i++ {
		s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: uuid.New()})
	}
	if fetches.Load() != 3 {
		t.Fatalf("only 5xx pauses the provider, got %d fetches", fetches.Load())
	}
}

func TestIsServerError(t *testing.T) {
	if !isServerError(&statusError{Code: 502}) {
		t.Fatal("502 is a server error")
	}
	if isServerError(&statusError{Code: 404}) || isServerError(errors.New("lrclib status 500")) || isServerError(nil) {
		t.Fatal("only typed 5xx replies count")
	}
}
