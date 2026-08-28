package scapex

import (
	"context"
	"errors"
	"testing"

	"github.com/sounddock/sounddock/internal/jobs"
)

func TestClassifyFetchError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want FetchClass
	}{
		{context.Canceled, FetchCancelled},
		{jobs.ErrCancelled, FetchCancelled},
		{errors.New("job cancelled by user"), FetchCancelled},
		{jobs.ErrTerminal, FetchTerminal},
		{errors.New("not an allowlisted YouTube watch URL or video id"), FetchTerminal},
		{errors.New("yt-dlp produced no audio"), FetchTerminal},
		{errors.New("ERROR: Private video"), FetchTerminal},
		{errors.New("Video unavailable"), FetchTerminal},
		{errors.New("library storage quota exceeded"), FetchTerminal},
		{errors.New("HTTP 404"), FetchTerminal},
		{errors.New("connection reset by peer"), FetchRetryable},
		{errors.New("HTTP 429 Too Many Requests"), FetchRetryable},
		{errors.New("timed out"), FetchRetryable},
		{errors.New("HTTP 503"), FetchRetryable},
		{context.DeadlineExceeded, FetchRetryable},
	}
	for _, c := range cases {
		if got := ClassifyFetchError(c.err); got != c.want {
			t.Fatalf("%v: got %v want %v", c.err, got, c.want)
		}
	}
}

func TestWrapFetchErrorPreservesClass(t *testing.T) {
	err := WrapFetchError(errors.New("Private video"))
	if ClassifyFetchError(err) != FetchTerminal || !errors.Is(err, jobs.ErrTerminal) {
		t.Fatalf("%v", err)
	}
	err = WrapFetchError(context.Canceled)
	if ClassifyFetchError(err) != FetchCancelled || !errors.Is(err, jobs.ErrCancelled) {
		t.Fatalf("%v", err)
	}
}
