package vialite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The overlay carries vialite's own bridge classes; these paths are relative to
// the go module directory.
const overlayBridgeDir = "../build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge"

func readOverlayFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(overlayBridgeDir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// TestOverlayWidensGateClassicHandshakeAddressLimit guards the overlay delta that
// makes vialite accept a Gate classic legacy-forwarding handshake.
//
// Gate classic with forwarding.mode: legacy appends BungeeCord-style player data
// to the handshake address — host\0ip\0uuid\0<profile properties JSON> — and an
// authenticated (online mode) player's Mojang profile properties are ~2.8 kB.
// netminecraft's stock C2SHandshakingClientIntentionPacket decodes that field with
// the vanilla 255 character limit and throws DecoderException before Via
// translation starts, so no authenticated player can join any backend through the
// hop. The overlay therefore decodes the client-to-proxy handshake with
// Short.MAX_VALUE (what BungeeCord, Spigot and Paper accept for this field).
//
// Those three bridge files live in the overlay, not upstream, so a ViaProxy pin
// bump drops them unless they are re-derived (build/overlay/README.md). A lost
// delta is silent until an operator with legacy forwarding and online mode cannot
// join; this guard turns that into a red Go test. The behavioural proof lives in
// the Gate classic -> vialite -> Paper harness (authenticated join rows), because
// CI cannot start the native runtime here.
func TestOverlayWidensGateClassicHandshakeAddressLimit(t *testing.T) {
	packet := readOverlayFile(t, "VialiteHandshakePacket.java")
	if !strings.Contains(packet, "extends C2SHandshakingClientIntentionPacket") {
		t.Error("VialiteHandshakePacket.java no longer subclasses C2SHandshakingClientIntentionPacket")
	}
	if !strings.Contains(packet, "public void read(final ByteBuf byteBuf, final int protocolVersion)") {
		t.Error("VialiteHandshakePacket.java no longer overrides read")
	}
	if !strings.Contains(packet, "Short.MAX_VALUE") {
		t.Error("VialiteHandshakePacket.java no longer widens the handshake address limit to Short.MAX_VALUE")
	}
	if strings.Contains(packet, "PacketTypes.readString(byteBuf, 255)") {
		t.Error("VialiteHandshakePacket.java fell back to the vanilla 255 character address limit")
	}
	if !strings.Contains(packet, "this.address = PacketTypes.readString(byteBuf, MAX_ADDRESS_LENGTH);") {
		t.Error("VialiteHandshakePacket.java no longer decodes the address with MAX_ADDRESS_LENGTH")
	}

	registry := readOverlayFile(t, "VialitePacketRegistry.java")
	if !strings.Contains(registry, "MCPackets.C2S_HANDSHAKING_CLIENT_INTENTION") ||
		!strings.Contains(registry, "VialiteHandshakePacket::new") {
		t.Error("VialitePacketRegistry.java no longer registers VialiteHandshakePacket for the handshake packet id")
	}

	initializer := readOverlayFile(t, "VialiteClient2ProxyChannelInitializer.java")
	if !strings.Contains(initializer, "MCPipeline.PACKET_REGISTRY_ATTRIBUTE_KEY") ||
		!strings.Contains(initializer, "new VialitePacketRegistry(") {
		t.Error("VialiteClient2ProxyChannelInitializer.java no longer installs the vialite packet registry")
	}

	bridge := readOverlayFile(t, "VialiteBridge.java")
	if !strings.Contains(bridge, "new VialiteClient2ProxyChannelInitializer(") {
		t.Error("VialiteBridge.java no longer creates the per-backend listeners with the vialite channel initializer")
	}
	if strings.Contains(bridge, "new Client2ProxyChannelInitializer(") {
		t.Error("VialiteBridge.java still creates listeners with the stock Client2ProxyChannelInitializer")
	}
}
