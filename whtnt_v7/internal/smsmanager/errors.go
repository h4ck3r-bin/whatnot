package smsmanager

import "fmt"

type Error struct {
	Provider string
	Op       string
	Msg      string
	Cause    error
}

func (e *Error) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("%s %s: %s", e.Provider, e.Op, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Msg)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func wrap(provider, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Provider: provider, Op: op, Msg: err.Error(), Cause: err}
}

func msg(provider, op, m string) error {
	return &Error{Provider: provider, Op: op, Msg: m}
}
