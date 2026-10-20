//go:build linux

package vialite

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listenerOwnedByPID reports whether the listening socket on addr is held by pid.
//
// Readiness is decided by dialling the address, which any listener satisfies, so
// without this check a runtime that lost its port to a foreign listener is
// published as ready and Gate's joins are routed through that other session's
// tunnel. Measured 2026-10-20 (kanban t_40da126d): Gate allocates its per-join
// bridge listener with net.Listen("tcp", "127.0.0.1:0") from the same ephemeral
// range, so it can take a port this package reserved moments earlier; the child
// then dies with "bind(..) failed with error(-98): Address already in use" and
// the affected joins stall silently in config state.
//
// known=false means ownership could not be established on this platform/host.
func listenerOwnedByPID(addr string, pid int) (owned bool, known bool) {
	if pid <= 0 {
		return false, false
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return false, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false, false
	}
	inodes, ok := listenSocketInodes(port)
	if !ok {
		return false, false
	}
	if len(inodes) == 0 {
		// Nothing listens there at all: whatever answered the readiness dial is
		// not this runtime's listener.
		return false, true
	}
	fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return false, false
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		for _, inode := range inodes {
			if target == fmt.Sprintf("socket:[%s]", inode) {
				return true, true
			}
		}
	}
	return false, true
}

// listenSocketInodes returns the inodes of LISTEN sockets bound to port.
func listenSocketInodes(port int) ([]string, bool) {
	var inodes []string
	checked := false
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		checked = true
		scanner := bufio.NewScanner(f)
		first := true
		for scanner.Scan() {
			if first {
				first = false
				continue
			}
			fields := strings.Fields(scanner.Text())
			// sl local_address rem_address st ... inode
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			local := fields[1]
			idx := strings.LastIndex(local, ":")
			if idx < 0 {
				continue
			}
			got, err := strconv.ParseInt(local[idx+1:], 16, 32)
			if err != nil || int(got) != port {
				continue
			}
			inodes = append(inodes, fields[9])
		}
		_ = f.Close()
	}
	return inodes, checked
}
