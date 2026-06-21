//go:build !unix

package vialite

import "os"

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
