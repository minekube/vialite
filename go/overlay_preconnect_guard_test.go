package vialite

import (
	"strings"
	"testing"
)

// TestOverlayFailsClosedWhenNoBackendDialHappens guards the overlay delta that
// keeps a bridge connection from staying silent.
//
// The hop is only reachable from Gate, on loopback, and Gate writes its
// handshake immediately after dialling, so the window between accepting a
// connection and dialling the backend is short by construction. It had no upper
// bound and no diagnostic: when an accepted connection never reached
// Client2ProxyHandler (a stall in the Netty/ViaVersion pipeline, an event-loop
// block, a flow-control hold) the runtime logged nothing at all, opened no
// socket to any backend, and left Gate's join parked on a read that — on a Gate
// without gate#1197 — effectively never times out. The operator sees "player
// has connected, completing login" and then nothing, forever, while `ss` shows
// one idle Gate->hop pair and no backend socket; that is the state reported in
// the 2026-09-27 Foxof7207 / Gilly-SMP case (kanban t_ac730a6f) and it was
// reproduced in a pod with a connection that simply never sent a handshake
// (v0.3.7: 40 s of total silence, no console line, no backend socket).
//
// The guard is a daemon thread of its own, so it still fires when an event loop
// is blocked; it names the backend, the configured address, the resolved
// address and the stage reached, and then closes the connection so Gate fails
// the join instead of waiting on a silent one.
//
// The behavioural proof lives in the pod harness (hop-only rows: a silent
// connection must produce the ERROR and a close instead of silence); this Go
// test is the contract guard, because CI cannot start the native runtime here.
func TestOverlayFailsClosedWhenNoBackendDialHappens(t *testing.T) {
	guard := readOverlayFile(t, "VialitePreConnectGuard.java")

	if !strings.Contains(guard, "AttributeKey.valueOf(") {
		t.Error("VialitePreConnectGuard.java no longer tracks a per-connection stage on the channel")
	}
	if !strings.Contains(guard, "STAGE_HANDSHAKE") || !strings.Contains(guard, "STAGE_DIAL") {
		t.Error("VialitePreConnectGuard.java no longer distinguishes the handshake and backend-dial stages")
	}
	if !strings.Contains(guard, "DEFAULT_HANDSHAKE_DEADLINE_MS") ||
		!strings.Contains(guard, "DEFAULT_DIAL_DEADLINE_MS") {
		t.Error("VialitePreConnectGuard.java no longer bounds both pre-connect stages with a deadline")
	}
	if !strings.Contains(guard, "newSingleThreadScheduledExecutor") ||
		!strings.Contains(guard, "setDaemon(true)") {
		t.Error("VialitePreConnectGuard.java must watch from its own daemon thread so a blocked Netty event loop cannot silence the guard")
	}
	if !strings.Contains(guard, "Level.ERROR") || !strings.Contains(guard, "Logger.u_log(") {
		t.Error("VialitePreConnectGuard.java no longer reports the stall as an error the operator can see")
	}
	if !strings.Contains(guard, "VialiteBridge.describeRoute(") {
		t.Error("VialitePreConnectGuard.java no longer names the backend and address in its report")
	}
	if !strings.Contains(guard, "channel.close()") {
		t.Error("VialitePreConnectGuard.java no longer closes the stalled connection (fail closed)")
	}
	if !strings.Contains(guard, "stage=") || !strings.Contains(guard, "no backend was contacted") {
		t.Error("VialitePreConnectGuard.java no longer says which stage was reached and that the backend was never contacted")
	}

	initializer := readOverlayFile(t, "VialiteClient2ProxyChannelInitializer.java")
	armed := strings.Index(initializer, "VialitePreConnectGuard.arm(channel)")
	installed := strings.Index(initializer, "super.initChannel(channel)")
	if armed < 0 {
		t.Fatal("VialiteClient2ProxyChannelInitializer.java no longer arms the pre-connect guard")
	}
	if installed < 0 || armed > installed {
		t.Error("the guard must be armed before the upstream pipeline is installed, otherwise a stall inside that installation is uncovered")
	}

	bridge := readOverlayFile(t, "VialiteBridge.java")
	if !strings.Contains(bridge, "VialitePreConnectGuard.STAGE_HANDSHAKE") {
		t.Error("VialiteBridge.java no longer marks the handshake stage when the route is selected")
	}
	if !strings.Contains(bridge, "ConnectEvent") || !strings.Contains(bridge, "VialitePreConnectGuard.STAGE_DIAL") {
		t.Error("VialiteBridge.java no longer marks the backend-dial stage from ViaProxy's ConnectEvent")
	}
	if !strings.Contains(bridge, "describeRoute") || !strings.Contains(bridge, "resolved ") {
		t.Error("VialiteBridge.java no longer describes the backend with its configured and resolved address")
	}
	if !strings.Contains(bridge, "VialitePreConnectGuard.configure(") {
		t.Error("VialiteBridge.java no longer applies the pre-connect deadlines from the native config")
	}
}
