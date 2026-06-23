//go:build windows

package vialite

import (
	"syscall"

	"github.com/ebitengine/purego"
)

func loadNativeSymbols(path string) (*nativeSymbols, error) {
	handle, err := syscall.LoadLibrary(path)
	if err != nil {
		return nil, err
	}
	lib := uintptr(handle)
	s := &nativeSymbols{}
	purego.RegisterLibFunc(&s.createIsolate, lib, "graal_create_isolate")
	purego.RegisterLibFunc(&s.tearDownIsolate, lib, "graal_tear_down_isolate")
	purego.RegisterLibFunc(&s.init, lib, "vialite_init")
	purego.RegisterLibFunc(&s.run, lib, "vialite_run")
	purego.RegisterLibFunc(&s.shutdown, lib, "vialite_shutdown")
	purego.RegisterLibFunc(&s.status, lib, "vialite_status")
	purego.RegisterLibFunc(&s.backendAddress, lib, "vialite_backend_address")
	purego.RegisterLibFunc(&s.addBackend, lib, "vialite_add_backend")
	purego.RegisterLibFunc(&s.removeBackend, lib, "vialite_remove_backend")
	return s, nil
}
