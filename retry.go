package godam

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

type stopRetrying struct {
	error
}

func parseRetryAfterHeader(retryAfter string) time.Duration {
	when, err := http.ParseTime(retryAfter)
	if err != nil {
		after, err := strconv.ParseInt(retryAfter, 10, 64)
		if err == nil && after > 0 {
			return time.Duration(after) * time.Second
		}
	}
	return time.Until(when)
}

func retry(timeout time.Duration, f func(ctx context.Context) error) (err error) {
	startedTrying := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for retries := 0; time.Since(startedTrying) < timeout; retries++ {
		delay := time.Duration(retries+1) * time.Second / 2

		err = f(ctx)
		switch v := err.(type) {
		case nil:
			break
		case stopRetrying:
			return v.error
		case HTTPError:
			if v.StatusCode == 503 && v.Header.Get("Retry-After") != "" {
				delay = parseRetryAfterHeader(v.Header.Get("Retry-After"))
			}
		}

		time.Sleep(delay)
	}
	return err
}
