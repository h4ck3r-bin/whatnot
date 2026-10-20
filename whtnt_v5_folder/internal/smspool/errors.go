package smspool

import "fmt"

type APIError struct {
	Op  string
	Raw string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, e.Raw)
}
