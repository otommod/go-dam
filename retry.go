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
	if err != nil {
		after, err := strconv.ParseInt(retryAfter, 10, 64)
		if err == nil && after > 0 {
			return time.Duration(after) * time.Second
		}
	}
	return time.Until(when)
}

func retry(timeout time.Duration, f func() error) (err error) {
	retriesStart := time.Now()
	retriesEnd := retriesStart.Add(timeout)

	for retries := 0; time.Now().Before(retriesEnd); retries++ {
		var delay time.Duration

		err = f()
		if err != nil {
			switch v := err.(type) {
			case stopRetrying:
				return v.error
			case HTTPError:
				if v.StatusCode == 503 && v.Header.Get("Retry-After") != "" {
					delay = parseRetryAfterHeader(v.Header.Get("Retry-After"))
				}
			default:
				delay = time.Duration(retries+1) * time.Second / 2
			}

			time.Sleep(delay)
			continue
		}
		break
	}
	return err
}
