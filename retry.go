package godam

import (
	"net/http"
	"strconv"
	"time"
)

type stopRetrying struct {
	error
}

func parseRetryAfterHeader(retryAfter string) time.Duration {
	when, err := http.ParseTime(retryAfter)
	if err == nil && time.Now().Before(when) {
		return time.Until(when)
	}
	after, err := strconv.Atoi(retryAfter)
	if err == nil && after > 0 {
		return time.Duration(after) * time.Second
	}
	return 0
}

func retry(timeout time.Duration, f func() error) (err error) {
	startedTrying := time.Now()

	for time.Since(startedTrying) < timeout {
		delay := time.Second / 2

		err = f()
		switch v := err.(type) {
		case nil:
			return
		case stopRetrying:
			return v.error
		case HTTPError:
			if v.StatusCode == 503 && v.Header.Get("Retry-After") != "" {
				delay = parseRetryAfterHeader(v.Header.Get("Retry-After"))
			}
		}

		time.Sleep(delay)
	}
	return
}
