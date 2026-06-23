package vialite

import (
	"fmt"
	"unsafe"
)

type nativeExitError struct {
	op   string
	code int
}

func (e nativeExitError) Error() string {
	return fmt.Sprintf("vialite: native %s failed with exit code %d", e.op, e.code)
}

type nativeSymbols struct {
	createIsolate   func(unsafe.Pointer, *unsafe.Pointer, *unsafe.Pointer) int
	tearDownIsolate func(unsafe.Pointer) int
	init            func(unsafe.Pointer, string) int
	run             func(unsafe.Pointer) int
	shutdown        func(unsafe.Pointer) int
	status          func(unsafe.Pointer) int
	backendAddress  func(unsafe.Pointer, string) string
	addBackend      func(unsafe.Pointer, string) string
	removeBackend   func(unsafe.Pointer, string) int
}
