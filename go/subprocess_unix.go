//go:build unix

package vialite

import (
	"os"
	"syscall"
)

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}
