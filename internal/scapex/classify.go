package scapex

import (
	"context"
	"errors"
	"strings"

	"github.com/sounddock/sounddock/internal/jobs"
)

// FetchClass is the acquisition error class. Retryable keeps the stub and
// queue occurrence. Terminal fails the intent and drops uncommitted stubs
// unless they are playlist-held. Cancelled drops uncommitted work only.
type FetchClass int

const (
	FetchRetryable FetchClass = iota
	FetchTerminal
	FetchCancelled
)

func ClassifyFetchError(err error) FetchClass {
	if err == nil {
		return FetchRetryable
	}
	if errors.Is(err, jobs.ErrCancelled) || errors.Is(err, context.Canceled) {
		return FetchCancelled
	}
	if errors.Is(err, jobs.ErrTerminal) {
		return FetchTerminal
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "cancel") {
		return FetchCancelled
	}
	for _, needle := range []string{
		"not an allowlisted",
		"produced no audio",
		"video unavailable",
		"private video",
		"copyright",
		"removed by the uploader",
		"this video is not available",
		"sign in to confirm",
		"quota exceeded",
		"http 404",
		"status 404",
		"410 gone",
	} {
		if strings.Contains(msg, needle) {
			return FetchTerminal
		}
	}
	return FetchRetryable
}

func WrapFetchError(err error) error {
	if err == nil {
		return nil
	}
	switch ClassifyFetchError(err) {
	case FetchCancelled:
		if errors.Is(err, jobs.ErrCancelled) {
			return err
		}
		return errors.Join(jobs.ErrCancelled, err)
	case FetchTerminal:
		if errors.Is(err, jobs.ErrTerminal) {
			return err
		}
		return errors.Join(jobs.ErrTerminal, err)
	default:
		return err
	}
}
