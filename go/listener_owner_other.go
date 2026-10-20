//go:build !linux

package vialite

// listenerOwnedByPID cannot answer ownership of a listening socket on this
// platform, so the caller keeps its dial-based readiness verdict.
func listenerOwnedByPID(string, int) (owned bool, known bool) {
	return false, false
}
