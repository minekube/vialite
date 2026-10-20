package vialite

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// gateDefaultReadTimeoutMs is Gate's shipped default (pkg/edition/java/config:
// ReadTimeout: 30000ms). The CONFIGURATION stage deadline has to stay below it:
// the hop must fail the join and name the stage itself instead of leaving Gate
// to time the join out with no idea where the join stopped.
const gateDefaultReadTimeoutMs int64 = 30_000

// overlayLongConstant reads an integer constant like
// `private static final long DEFAULT_X_MS = 15_000L;` out of an overlay source.
func overlayLongConstant(t *testing.T, source, name string) int64 {
	t.Helper()
	match := regexp.MustCompile(name + `\s*=\s*([0-9][0-9_]*)L\s*;`).FindStringSubmatch(source)
	if match == nil {
		t.Fatalf("%s is no longer declared as a millisecond constant", name)
	}
	value, err := strconv.ParseInt(strings.ReplaceAll(match[1], "_", ""), 10, 64)
	if err != nil {
		t.Fatalf("parse %s value %q: %v", name, match[1], err)
	}
	return value
}

// TestOverlayConfigurationStageFailsClosedAfterLoginSuccess guards the overlay
// delta that bounds the CONFIGURATION stage - the second silent stall class.
//
// The pre-connect guard covers accept -> handshake -> backend dial. A join that
// logs "Connected successfully! Switching to CONFIGURATION state" and then emits
// nothing was still a silent hang: the backend socket keeps being read, nothing
// is written toward the client, no kick and no log line, and Gate waits until its
// own read timeout (30 s by default) with no diagnosis. That is how the 0.2% class
// presented (backend socket rx growing while the client socket stayed empty).
//
// The overlay therefore arms a stage timer once LoginSuccess has been written to
// the client, counts socket-level bytes in both directions, and closes the
// connection with one named diagnosis if the client is fed nothing before the
// deadline - or if its channel is unwritable at that point. It changes no
// translation and no ViaProxy deadline.
//
// The behavioural proof for the three branches (fires on silence, does not fire
// on a healthy join, diagnosis names the stage/player/both addresses) lives in
// build/configstage-watch/run.sh, which drives the real guard source on real
// Netty and also runs a timer-less mutant that must fail only the fire-on-silence
// branch. This Go test is the contract guard, because CI cannot start the native
// runtime here.
func TestOverlayConfigurationStageFailsClosedAfterLoginSuccess(t *testing.T) {
	guard := readOverlayFile(t, "VialitePreConnectGuard.java")

	if !strings.Contains(guard, "STAGE_CONFIGURATION = 3") {
		t.Error("VialitePreConnectGuard.java no longer declares the CONFIGURATION stage after the backend dial")
	}
	if !strings.Contains(guard, `return "configuration";`) {
		t.Error("VialitePreConnectGuard.java no longer names the CONFIGURATION stage in its diagnoses")
	}

	if !strings.Contains(guard, "msg instanceof S2CLoginGameProfilePacket") ||
		!strings.Contains(guard, "armConfigurationStage(channel") {
		t.Error("VialitePreConnectGuard.java no longer arms the CONFIGURATION stage when LoginSuccess is written to the client")
	}
	if !strings.Contains(guard, "promise.addListener(future -> {") {
		t.Error("the CONFIGURATION stage must be armed after the LoginSuccess write completes, not when it is queued, or the stage timer starts before the client has anything")
	}
	if !strings.Contains(guard, "clientEntersConfiguration(") || !strings.Contains(guard, "ProtocolVersion.v1_20_2") {
		t.Error("VialitePreConnectGuard.java no longer limits the CONFIGURATION stage to clients that have one (1.20.2+)")
	}

	for _, want := range []string{
		"static final class TrafficCounter extends ChannelDuplexHandler",
		"public void channelRead(",
		"public void write(",
		"record(channel, bytes, true)",
	} {
		if !strings.Contains(guard, want) {
			t.Fatalf("VialitePreConnectGuard.java no longer counts bytes in both directions (%q missing)", want)
		}
	}
	if !strings.Contains(guard, "if (!watch.armed)") {
		t.Error("VialitePreConnectGuard.java must only count CONFIGURATION traffic after the stage is armed, otherwise LoginSuccess itself looks like progress")
	}
	if !strings.Contains(guard, "if (clientbound > 0L)") {
		t.Error("VialitePreConnectGuard.java must not fire when clientbound bytes did arrive (a healthy 1.20.2+ join sends the configuration burst within milliseconds)")
	}
	if !strings.Contains(guard, "channel.isWritable()") || !strings.Contains(guard, "channel.isActive()") {
		t.Error("VialitePreConnectGuard.java no longer checks whether the client channel is unwritable/inactive while the stage is CONFIGURATION")
	}

	if !strings.Contains(guard, "scheduler().schedule(() -> fireConfiguration") {
		t.Error("VialitePreConnectGuard.java no longer schedules the CONFIGURATION stage timer on the guard's own daemon thread")
	}
	if !strings.Contains(guard, "Level.ERROR") || !strings.Contains(guard, "channel.close()") {
		t.Error("VialitePreConnectGuard.java no longer fails the join closed with a visible ERROR")
	}

	for _, want := range []string{
		"stage=configuration",
		`player \"`,
		"clientbound bytes since LoginSuccess=",
		"serverbound bytes since LoginSuccess=",
		"elapsed ",
		"VialiteBridge.describeRoute(",
	} {
		if !strings.Contains(guard, want) {
			t.Fatalf("the CONFIGURATION diagnosis no longer carries %q", want)
		}
	}

	configurationDeadline := overlayLongConstant(t, guard, "DEFAULT_CONFIGURATION_DEADLINE_MS")
	if configurationDeadline < 10_000 {
		t.Errorf("DEFAULT_CONFIGURATION_DEADLINE_MS is %d ms: too tight for a 1.20.2+ join over the internet", configurationDeadline)
	}
	if configurationDeadline >= gateDefaultReadTimeoutMs {
		t.Errorf("DEFAULT_CONFIGURATION_DEADLINE_MS is %d ms: Gate's default read timeout is %d ms, so the hop would leave the diagnosis to Gate",
			configurationDeadline, gateDefaultReadTimeoutMs)
	}

	initializer := readOverlayFile(t, "VialiteClient2ProxyChannelInitializer.java")
	installed := strings.Index(initializer, "VialitePreConnectGuard.installConfigurationWatch(channel)")
	upstream := strings.Index(initializer, "super.initChannel(channel)")
	if installed < 0 {
		t.Fatal("VialiteClient2ProxyChannelInitializer.java no longer installs the CONFIGURATION stage watch")
	}
	if upstream < 0 || installed < upstream {
		t.Error("the CONFIGURATION stage watch must be installed after the upstream pipeline exists, so it sits at the socket and packet ends of the real pipeline")
	}

	bridge := readOverlayFile(t, "VialiteBridge.java")
	if !strings.Contains(bridge, `@SerializedName("configuration_deadline_ms")`) {
		t.Error("VialiteBridge.java no longer accepts configuration_deadline_ms from the native config")
	}
	if !strings.Contains(bridge, "nativeConfig.configurationDeadlineMs") {
		t.Error("VialiteBridge.java no longer passes configuration_deadline_ms to the guard")
	}
}
