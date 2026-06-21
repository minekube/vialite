package vialite

import "fmt"

type nativeExitError struct {
	op   string
	code int
}

func (e nativeExitError) Error() string {
	return fmt.Sprintf("vialite: native %s failed with exit code %d", e.op, e.code)
}

type nativeSymbols struct {
	init           func(string) int
	run            func() int
	shutdown       func() int
	status         func() int
	backendAddress func(string) string
}
