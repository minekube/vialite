//go:build windows

package vialite

import "github.com/ebitengine/purego"

func loadNativeSymbols(path string) (*nativeSymbols, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW)
	if err != nil {
		return nil, err
	}
	s := &nativeSymbols{}
	purego.RegisterLibFunc(&s.init, lib, "vialite_init")
	purego.RegisterLibFunc(&s.run, lib, "vialite_run")
	purego.RegisterLibFunc(&s.shutdown, lib, "vialite_shutdown")
	purego.RegisterLibFunc(&s.status, lib, "vialite_status")
	purego.RegisterLibFunc(&s.backendAddress, lib, "vialite_backend_address")
	return s, nil
}
