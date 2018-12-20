package dam

import (
	"net/http"
)

type HTTPError struct {
	*http.Response
}

func (e HTTPError) Error() string {
	return e.Status
}
