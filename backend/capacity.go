// SPDX-License-Identifier: GPL-2.0-only
package main

import "fmt"

type capacityError struct {
	What   string
	Needed int
	Limit  int
	msg    string
}

func (e *capacityError) Error() string { return e.msg }

func newCapacityError(what string, needed, limit int, format string, args ...any) *capacityError {
	return &capacityError{
		What:   what,
		Needed: needed,
		Limit:  limit,
		msg:    fmt.Sprintf(format, args...),
	}
}
