package vialite

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// errSubprocessExitedBeforeReady is reported when the runtime died before it
// published the backend listener Gate dials. It is distinct from a readiness
// timeout so callers can tell "the runtime is gone" from "the runtime is slow".
var errSubprocessExitedBeforeReady = errors.New("vialite: subprocess exited before backend listener became ready")

// ErrDynamicAddressExhausted reports that a dynamic backend could not be given a
// loopback address that no other backend owns. Publishing the colliding address
// anyway would point Gate at another session's runtime.
var ErrDynamicAddressExhausted = errors.New("vialite: no unreserved loopback address for dynamic backend")

// ErrAddressNotOwned reports that a dynamic backend's listener is held by
// another process: publishing that address would route its joins through a
// foreign session instead of failing the join.
var ErrAddressNotOwned = errors.New("vialite: dynamic backend address is not owned by its runtime")

// describeBackendBinds renders the backend loopback addresses the runtime was
// supposed to publish, so a readiness failure names which listener is missing
// instead of only saying that something was not ready.
func describeBackendBinds(backends map[string]string) string {
	type entry struct{ name, addr string }
	byAddr := make(map[string]entry, len(backends))
	for name, addr := range backends {
		existing, ok := byAddr[addr]
		// storeBackendAddress records both the original and the lookup name for
		// the same address; keep one deterministic label per address.
		if !ok || name < existing.name {
			byAddr[addr] = entry{name: name, addr: addr}
		}
	}
	entries := make([]string, 0, len(byAddr))
	for _, e := range byAddr {
		entries = append(entries, e.name+"="+e.addr)
	}
	sort.Strings(entries)
	return strings.Join(entries, ", ")
}

type subprocessRunner struct {
	healthy  atomic.Bool
	backends map[string]string

	mu      sync.Mutex
	cmd     *exec.Cmd
	bin     string
	opts    Options
	dynamic map[string]*subprocessBackendProcess
	adding  map[string]struct{}
	pending map[string]*subprocessBackendProcess
	// reserving maps a loopback address a dynamic backend is being given
	// (before its runtime binds it) to the backend key that owns it, so one
	// address is never promised to two runtimes in the same burst.
	reserving map[string]string
}

type subprocessBackendProcess struct {
	name       string
	addr       string
	configPath string
	cmd        *exec.Cmd
	done       chan struct{}
	waitMu     sync.Mutex
	waitErr    error
	// releaseAddr returns the backend's reserved loopback address to the
	// allocator that handed it out (Options.ReserveLoopbackAddress). It is nil
	// when this package allocated the address itself, and is called exactly once,
	// when the runtime no longer uses the address.
	releaseAddr func()
}

// releaseAddress returns the backend's reserved address to its allocator. Safe to
// call more than once and on a nil process.
func (p *subprocessBackendProcess) releaseAddress() {
	if p == nil || p.releaseAddr == nil {
		return
	}
	p.releaseAddr()
}

func (p *subprocessBackendProcess) setWaitErr(err error) {
	p.waitMu.Lock()
	p.waitErr = err
	p.waitMu.Unlock()
}

func (p *subprocessBackendProcess) err() error {
	p.waitMu.Lock()
	defer p.waitMu.Unlock()
	return p.waitErr
}

func (r *subprocessRunner) run(ctx context.Context, s *Server) error {
	bin, err := locateBinary(ctx, s.opts)
	if err != nil {
		return err
	}
	opts := s.opts
	r.mu.Lock()
	r.bin = bin
	r.opts = opts
	if r.backends == nil {
		r.backends = make(map[string]string)
	}
	if r.dynamic == nil {
		r.dynamic = make(map[string]*subprocessBackendProcess)
	}
	if r.adding == nil {
		r.adding = make(map[string]struct{})
	}
	if r.pending == nil {
		r.pending = make(map[string]*subprocessBackendProcess)
	}
	r.mu.Unlock()

	if len(opts.Backends) == 0 && opts.AllowDynamicBackends {
		r.healthy.Store(true)
		s.markReady(nil)
		<-ctx.Done()
		r.healthy.Store(false)
		r.stopDynamicBackends(context.Background(), opts.ShutdownTimeout)
		return nil
	}

	backends, err := loopbackBackendAddresses(opts.Bind, opts.Backends)
	if err != nil {
		return err
	}
	config, err := opts.nativeConfigJSONWithBackendBinds(backends)
	if err != nil {
		return err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(configPath) }()

	r.mu.Lock()
	r.backends = backends
	r.mu.Unlock()
	staticBackends := cloneBackendAddresses(backends)

	backoff := s.opts.RestartPolicy.MinBackoff
	for attempt := 0; ; attempt++ {
		cmd := exec.Command(bin, "--config", configPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		r.mu.Lock()
		r.cmd = cmd
		r.mu.Unlock()
		if err := cmd.Start(); err != nil {
			return err
		}
		r.healthy.Store(true)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		processDone, err := waitBackendListeners(ctx, done, staticBackends)
		if err != nil {
			r.healthy.Store(false)
			if ctx.Err() != nil {
				// The caller stopped us while this runtime was still coming up
				// (a crash-restart in flight, or a startup that never published
				// its listener). The runtime must not outlive the runner: a
				// leaked one keeps holding its loopback bind, so the next Start
				// fails closed on a listener nobody owns. Join the child this
				// attempt started - bounded, so a runtime that ignores the
				// termination signal cannot wedge the runner - and tear the
				// dynamic backends down with it, like the ready-path shutdown
				// below does.
				if !processDone {
					_ = r.waitProcess(ctx, cmd, done, s.opts.ShutdownTimeout)
				}
				r.stopDynamicBackends(context.Background(), s.opts.ShutdownTimeout)
				return nil
			}
			if !processDone {
				_ = terminateProcess(cmd.Process)
				<-done
			}
		} else {
			s.markReady(nil)
			err = r.waitProcess(ctx, cmd, done, s.opts.ShutdownTimeout)
			r.healthy.Store(false)
			if ctx.Err() != nil || err == nil {
				r.stopDynamicBackends(context.Background(), s.opts.ShutdownTimeout)
			}
		}

		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			return nil
		}
		if s.opts.RestartPolicy.MaxRetries >= 0 && attempt >= s.opts.RestartPolicy.MaxRetries {
			r.stopDynamicBackends(context.Background(), s.opts.ShutdownTimeout)
			return err
		}
		if sleepContext(ctx, backoff) != nil {
			return nil
		}
		backoff *= 2
		if backoff > s.opts.RestartPolicy.MaxBackoff {
			backoff = s.opts.RestartPolicy.MaxBackoff
		}
	}
}

func (r *subprocessRunner) waitProcess(ctx context.Context, cmd *exec.Cmd, done <-chan error, shutdownTimeout time.Duration) error {
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if err := terminateProcess(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		select {
		case err := <-done:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case <-time.After(shutdownTimeout):
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
			<-done
			return nil
		}
	}
}

func waitBackendListeners(ctx context.Context, done <-chan error, backends map[string]string) (bool, error) {
	waitCtx, cancel := backendListenerReadyContext(ctx)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if allBackendsDialable(backends) {
			// A dialable address is not proof that *this* runtime owns the
			// listener: a foreign process on a pinned bind port (via.bind) accepts
			// every connection the readiness check makes. Never report readiness
			// for a runtime that has already exited - Gate would come up
			// "healthy" with a dead hop and every join would fail later with a
			// misleading "not started".
			select {
			case err := <-done:
				if err == nil {
					err = errSubprocessExitedBeforeReady
				}
				return true, err
			default:
			}
			return false, nil
		}
		select {
		case err := <-done:
			if err == nil {
				err = errSubprocessExitedBeforeReady
			}
			return true, err
		case <-waitCtx.Done():
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, fmt.Errorf(
					"vialite: subprocess backend listener did not become ready within the caller's deadline (stage: runtime startup, expected %s): %w",
					describeBackendBinds(backends), ctxErr)
			}
			return false, fmt.Errorf(
				"vialite: subprocess backend listener did not become ready (stage: runtime startup, expected %s)",
				describeBackendBinds(backends))
		case <-ticker.C:
		}
	}
}

func waitBackendListenersProcess(ctx context.Context, proc *subprocessBackendProcess, backends map[string]string) (bool, error) {
	waitCtx, cancel := backendListenerReadyContext(ctx)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if allBackendsDialable(backends) {
			// See waitBackendListeners: a dialable address does not prove this
			// runtime owns the listener, so an exited process always wins.
			select {
			case <-proc.done:
				err := proc.err()
				if err == nil {
					err = errSubprocessExitedBeforeReady
				}
				return true, err
			default:
			}
			return false, nil
		}
		select {
		case <-proc.done:
			err := proc.err()
			if err == nil {
				err = errSubprocessExitedBeforeReady
			}
			return true, err
		case <-waitCtx.Done():
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, fmt.Errorf(
					"vialite: subprocess backend listener did not become ready within the caller's deadline (stage: runtime startup, backend %s, expected %s): %w",
					proc.name, describeBackendBinds(backends), ctxErr)
			}
			return false, fmt.Errorf(
				"vialite: subprocess backend listener did not become ready (stage: runtime startup, backend %s, expected %s)",
				proc.name, describeBackendBinds(backends))
		case <-ticker.C:
		}
	}
}

func backendListenerReadyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	const fallbackTimeout = 10 * time.Second
	if ctx == nil {
		return context.WithTimeout(context.Background(), fallbackTimeout)
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, fallbackTimeout)
}

func allBackendsDialable(backends map[string]string) bool {
	for _, addr := range backends {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	return true
}

func (r *subprocessRunner) isHealthy() bool {
	return r.healthy.Load()
}

func (r *subprocessRunner) backendAddress(name string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	addr, ok := r.backends[name]
	if !ok {
		addr, ok = r.backends[backendLookupName(name)]
	}
	if !ok {
		return "", ErrBackendNotFound
	}
	return addr, nil
}

func (r *subprocessRunner) addBackend(ctx context.Context, backend Backend) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if !r.healthy.Load() || r.bin == "" {
		r.mu.Unlock()
		return "", ErrNotStarted
	}
	key := backendLookupName(backend.Name)
	if _, ok := r.backends[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	if _, ok := r.dynamic[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	if _, ok := r.adding[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	if _, ok := r.pending[key]; ok {
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrDuplicateBackend, backend.Name)
	}
	bin := r.bin
	opts := r.opts
	if r.adding == nil {
		r.adding = make(map[string]struct{})
	}
	r.adding[key] = struct{}{}
	if r.pending == nil {
		r.pending = make(map[string]*subprocessBackendProcess)
	}
	r.mu.Unlock()

	cleanupAdding := func() {
		r.mu.Lock()
		delete(r.adding, key)
		r.mu.Unlock()
	}

	r.mu.Lock()
	addr, releaseAddr, err := r.reserveDynamicAddressLocked(key, opts.Bind)
	r.mu.Unlock()
	if err != nil {
		cleanupAdding()
		return "", fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
	}
	releaseReservation := func() {
		r.mu.Lock()
		if r.reserving[addr] == key {
			delete(r.reserving, addr)
		}
		r.mu.Unlock()
		if releaseAddr != nil {
			releaseAddr()
		}
	}
	binds := map[string]string{backend.Name: addr, key: addr}
	opts.Backends = []Backend{backend}
	config, err := opts.nativeConfigJSONWithBackendBinds(binds)
	if err != nil {
		releaseReservation()
		cleanupAdding()
		return "", err
	}
	configPath, err := writeTempConfig(config)
	if err != nil {
		releaseReservation()
		cleanupAdding()
		return "", err
	}

	cmd := exec.Command(bin, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	done := make(chan error, 1)

	r.mu.Lock()
	if !r.healthy.Load() || r.bin == "" {
		delete(r.adding, key)
		r.mu.Unlock()
		releaseReservation()
		_ = os.Remove(configPath)
		return "", ErrNotStarted
	}
	if err := cmd.Start(); err != nil {
		delete(r.adding, key)
		r.mu.Unlock()
		releaseReservation()
		cleanupAdding()
		_ = os.Remove(configPath)
		return "", err
	}
	go func() { done <- cmd.Wait() }()
	proc := newSubprocessBackendProcess(backend.Name, addr, configPath, cmd, done)
	proc.releaseAddr = releaseAddr
	delete(r.adding, key)
	r.pending[key] = proc
	r.mu.Unlock()

	if _, err := waitBackendListenersProcess(ctx, proc, map[string]string{key: addr}); err != nil {
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		r.mu.Lock()
		if r.pending[key] == proc {
			delete(r.pending, key)
		}
		stopped := !r.healthy.Load() || r.bin == ""
		r.mu.Unlock()
		if stopped {
			releaseReservation()
			return "", ErrNotStarted
		}
		releaseReservation()
		return "", err
	}

	if !dynamicAddressOwned(proc, addr) {
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		r.mu.Lock()
		if r.pending[key] == proc {
			delete(r.pending, key)
		}
		r.mu.Unlock()
		releaseReservation()
		return "", fmt.Errorf("%w: backend %s could not own %s; its listener belongs to another process, "+
			"so joins routed there would reach a foreign session", ErrAddressNotOwned, backend.Name, addr)
	}

	r.mu.Lock()
	if !r.healthy.Load() || r.bin == "" {
		if r.pending[key] == proc {
			delete(r.pending, key)
		}
		r.mu.Unlock()
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		releaseReservation()
		return "", ErrNotStarted
	}
	if r.pending[key] != proc {
		r.mu.Unlock()
		_ = stopSubprocessBackend(context.Background(), proc, opts.ShutdownTimeout)
		releaseReservation()
		return "", ErrNotStarted
	}
	delete(r.pending, key)
	if r.reserving[addr] == key {
		delete(r.reserving, addr)
	}
	if r.dynamic == nil {
		r.dynamic = make(map[string]*subprocessBackendProcess)
	}
	r.dynamic[key] = proc
	storeBackendAddress(r.backends, backend.Name, addr)
	r.mu.Unlock()
	go func() {
		<-proc.done
		r.dynamicBackendExited(key, proc)
	}()
	return addr, nil
}

func (r *subprocessRunner) removeBackend(ctx context.Context, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	key := backendLookupName(name)
	r.mu.Lock()
	proc, ok := r.dynamic[key]
	if !ok {
		r.mu.Unlock()
		if _, err := r.backendAddress(name); err == nil {
			return ErrDynamicBackendsUnsupported
		}
		return ErrBackendNotFound
	}
	delete(r.dynamic, key)
	delete(r.backends, proc.name)
	delete(r.backends, backendLookupName(proc.name))
	r.mu.Unlock()

	return stopSubprocessBackend(ctx, proc, r.opts.ShutdownTimeout)
}

func (r *subprocessRunner) stopDynamicBackends(ctx context.Context, timeout time.Duration) {
	r.mu.Lock()
	processes := make([]*subprocessBackendProcess, 0, len(r.dynamic)+len(r.pending))
	for key, proc := range r.dynamic {
		processes = append(processes, proc)
		delete(r.dynamic, key)
		delete(r.backends, proc.name)
		delete(r.backends, backendLookupName(proc.name))
	}
	for key, proc := range r.pending {
		processes = append(processes, proc)
		delete(r.pending, key)
	}
	for key := range r.adding {
		delete(r.adding, key)
	}
	for addr := range r.reserving {
		delete(r.reserving, addr)
	}
	r.mu.Unlock()
	for _, proc := range processes {
		_ = stopSubprocessBackend(ctx, proc, timeout)
	}
}

func (r *subprocessRunner) dynamicBackendExited(key string, proc *subprocessBackendProcess) {
	r.mu.Lock()
	if r.dynamic[key] == proc {
		delete(r.dynamic, key)
		delete(r.backends, proc.name)
		delete(r.backends, backendLookupName(proc.name))
	}
	if r.pending[key] == proc {
		delete(r.pending, key)
	}
	r.mu.Unlock()
	_ = os.Remove(proc.configPath)
}

func newSubprocessBackendProcess(name, addr, configPath string, cmd *exec.Cmd, wait <-chan error) *subprocessBackendProcess {
	proc := &subprocessBackendProcess{
		name:       name,
		addr:       addr,
		configPath: configPath,
		cmd:        cmd,
		done:       make(chan struct{}),
	}
	go func() {
		err := <-wait
		proc.setWaitErr(err)
		close(proc.done)
	}()
	return proc
}

func stopSubprocessBackend(ctx context.Context, proc *subprocessBackendProcess, shutdownTimeout time.Duration) error {
	defer func() {
		_ = os.Remove(proc.configPath)
		proc.releaseAddress()
	}()
	select {
	case <-proc.done:
		err := proc.err()
		if isExpectedProcessExit(err) {
			return nil
		}
		return err
	default:
	}
	if err := terminateProcess(proc.cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-proc.done:
		err := proc.err()
		if isExpectedProcessExit(err) {
			return nil
		}
		return err
	case <-ctx.Done():
		if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-proc.done
		return ctx.Err()
	case <-time.After(shutdownTimeout):
		if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		<-proc.done
		return nil
	}
}

func isExpectedProcessExit(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func writeTempConfig(data []byte) (string, error) {
	f, err := os.CreateTemp("", "vialite-*.json")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func loopbackBackendAddresses(bind string, backends []Backend) (map[string]string, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	if len(backends) > 1 {
		_, port, err := net.SplitHostPort(bind)
		if err != nil {
			return nil, err
		}
		if port != "0" {
			return nil, fmt.Errorf("vialite: subprocess bind %s cannot be shared by %d backends", bind, len(backends))
		}
	}
	addrs := make(map[string]string, len(backends)*2)
	for i, backend := range backends {
		addr, err := loopbackBackendAddress(bind, i)
		if err != nil {
			return nil, fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
		}
		storeBackendAddress(addrs, backend.Name, addr)
	}
	return addrs, nil
}

func storeBackendAddress(addrs map[string]string, name string, addr string) {
	addrs[name] = addr
	addrs[backendLookupName(name)] = addr
}

func cloneBackendAddresses(addrs map[string]string) map[string]string {
	clone := make(map[string]string, len(addrs))
	for name, addr := range addrs {
		clone[name] = addr
	}
	return clone
}

func concreteLoopbackBind(bind string) (string, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "", err
	}
	if port != "0" {
		// A pinned port is handed to the runtime verbatim, so this host must be
		// able to bind it. Readiness is decided by dialing the address, which a
		// foreign listener on the same port satisfies, so without this check the
		// runtime can fail to own its listener while the Server still reports
		// ready (measured 2026-09-27: Gate came up "healthy" with a dead runtime
		// and every join failed with "vialite: not started"). Fail here, naming
		// the address, instead of publishing a listener the runtime does not own.
		ln, err := net.Listen("tcp", bind)
		if err != nil {
			return "", fmt.Errorf("bind %s is not available for the runtime: %w", bind, err)
		}
		return bind, ln.Close()
	}
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	return addr, ln.Close()
}

func loopbackBackendAddress(bind string, index int) (string, error) {
	return concreteLoopbackBind(bind)
}

// addressOwnerLocked reports the backend a loopback address is already promised
// to: a published backend (static or dynamic), a pending dynamic runtime, or an
// in-flight add. A subprocess runtime cannot own a port another runtime holds,
// and publishing such an address sends Gate's joins through the other runtime's
// route (measured 2026-10-20: silent config-state stall, kanban t_40da126d).
func (r *subprocessRunner) addressOwnerLocked(addr string) (string, bool) {
	for name, published := range r.backends {
		if published == addr {
			return name, true
		}
	}
	for key, proc := range r.pending {
		if proc != nil && proc.addr == addr {
			return key, true
		}
	}
	if owner, ok := r.reserving[addr]; ok {
		return owner, true
	}
	return "", false
}

// reserveDynamicAddressLocked allocates and reserves a loopback address for a
// dynamic backend that no other backend has been promised. The allocation is a
// listen(:0) whose listener is closed again before the child binds it, so a
// burst can hand the same port to two children; reserving inside this runner
// keeps one address with one runtime instead of publishing an address whose
// listener belongs to another session.
func (r *subprocessRunner) reserveDynamicAddressLocked(key, bind string) (string, func(), error) {
	const attempts = 8
	lastAddr := ""
	lastOwner := ""
	for attempt := 0; attempt < attempts; attempt++ {
		addr, release, err := r.allocateBackendAddressLocked(bind)
		if err != nil {
			return "", nil, err
		}
		lastAddr = addr
		if owner, taken := r.addressOwnerLocked(addr); taken && owner != key {
			lastOwner = owner
			if release != nil {
				release()
			}
			continue
		}
		if r.reserving == nil {
			r.reserving = make(map[string]string)
		}
		r.reserving[addr] = key
		return addr, release, nil
	}
	return "", nil, fmt.Errorf("%w: %s is already reserved by %s", ErrDynamicAddressExhausted, lastAddr, lastOwner)
}

// allocateBackendAddressLocked reserves the loopback address a dynamic backend is
// told to bind. An embedder that owns one loopback allocator for the whole
// process injects it with Options.ReserveLoopbackAddress, so this package's
// reservations and that process's other dynamic listeners cannot name the same
// port. Without a hook the package allocates from its own reserved range.
//
// The returned release function (nil when this package owns the address) is
// called exactly once, when the runtime no longer uses the address.
func (r *subprocessRunner) allocateBackendAddressLocked(bind string) (string, func(), error) {
	if reserve := r.opts.ReserveLoopbackAddress; reserve != nil {
		if bind == "" {
			bind = "127.0.0.1:0"
		}
		host, _, err := net.SplitHostPort(bind)
		if err != nil {
			return "", nil, err
		}
		if host == "" {
			host = "127.0.0.1"
		}
		// The embedder owns every dynamic loopback address, including when
		// Options.Bind pins the runtime's static port: that port is the static
		// listener's, not a dynamic backend's.
		addr, release, err := reserve(host)
		if err != nil {
			return "", nil, err
		}
		if release == nil {
			release = func() {}
		}
		var once sync.Once
		return addr, func() { once.Do(release) }, nil
	}
	addr, err := allocateDynamicBackendAddress(bind)
	return addr, nil, err
}

// allocateDynamicBackendAddress reserves the loopback address that a dynamic
// subprocess backend is told to bind. It is a variable so tests can pin the
// address a racing allocation returns.
var allocateDynamicBackendAddress = dynamicLoopbackBackendAddress

func dynamicLoopbackBackendAddress(bind string) (string, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return "", err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	// Only the host carries over from Options.Bind: its port belongs to the
	// runtime's own static listener, and a dynamic backend that reused it would
	// collide with that listener (the pre-existing "dynamic backend must not
	// reuse the fixed bind" contract).
	return reserveLoopbackAddress(host)
}

// dynamicBackendPortRange keeps dynamic backend listeners out of the kernel's
// ephemeral range (linux default 32768-60999). Every allocation here is a
// listen(:0) whose socket is closed again before the runtime binds it, so any
// socket the same process allocates from the ephemeral range in that window can
// take the port: Gate's per-join bridge listener does exactly that, the child
// then fails to bind, and its joins are routed through the wrong tunnel
// (measured 2026-10-20, kanban t_40da126d).
var (
	dynamicBackendPortRangeStart = 20000
	dynamicBackendPortRangeEnd   = 29999
)

// reserveLoopbackAddress picks a free loopback port outside the ephemeral range
// and returns it after closing the probe listener.
func reserveLoopbackAddress(host string) (string, error) {
	span := dynamicBackendPortRangeEnd - dynamicBackendPortRangeStart + 1
	for attempt := 0; attempt < 256; attempt++ {
		port := dynamicBackendPortRangeStart + rand.IntN(span)
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		addr := ln.Addr().String()
		return addr, ln.Close()
	}
	// Every probed port is taken. Failing closed is deliberate: a kernel-chosen
	// fallback port comes from the ephemeral range, which is exactly where a
	// listen-then-close reservation can be taken by another listener in the same
	// process or host, so the fallback re-opens the collision this range exists to
	// remove (kanban t_40da126d).
	return "", fmt.Errorf("%w: %d-%d", ErrDynamicAddressExhausted, dynamicBackendPortRangeStart, dynamicBackendPortRangeEnd)
}

// dynamicAddressOwned reports whether the runtime owns the listener on addr.
// Readiness only proves that something answers the dial; a published address
// whose listener belongs to another process routes that backend's joins through
// a foreign tunnel, which stalls them silently.
func dynamicAddressOwned(proc *subprocessBackendProcess, addr string) bool {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return false
	}
	owned, known := listenerOwnedByPID(addr, proc.cmd.Process.Pid)
	if !known {
		return true
	}
	return owned
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
