package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

type Policy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

func New(initialDelay, maxDelay time.Duration, maxAttempts int) Policy {
	if initialDelay <= 0 {
		initialDelay = time.Second
	}
	if maxDelay < initialDelay {
		maxDelay = initialDelay
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return Policy{
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		MaxAttempts:  maxAttempts,
	}
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	var permanent *permanentError
	if errors.As(err, &permanent) {
		return err
	}
	return &permanentError{err: err}
}

func IsPermanent(err error) bool {
	var permanent *permanentError
	return errors.As(err, &permanent)
}

func (p Policy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := p.InitialDelay
	for i := 1; i < attempt && delay < p.MaxDelay; i++ {
		if delay > p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	if delay <= 1 {
		return delay
	}
	half := delay / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func (p Policy) Do(ctx context.Context, operation func(context.Context) error) error {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation(ctx)
		if err == nil {
			return nil
		}
		if IsPermanent(err) || attempt >= p.MaxAttempts {
			return err
		}
		delay := p.Delay(attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
