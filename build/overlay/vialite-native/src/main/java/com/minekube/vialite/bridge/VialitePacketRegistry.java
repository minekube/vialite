package com.minekube.vialite.bridge;

import net.raphimc.netminecraft.constants.MCPackets;
import net.raphimc.netminecraft.packet.registry.DefaultPacketRegistry;

/**
 * Packet registry that decodes the client-to-proxy handshake with
 * {@link VialiteHandshakePacket} instead of the vanilla 255-character limit.
 *
 * <p>The registry installed by ViaProxy's {@code Client2ProxyChannelInitializer}
 * is a {@code DefaultPacketRegistry}, whose handshake entry is the stock
 * {@code C2SHandshakingClientIntentionPacket}. Re-registering the same
 * {@link MCPackets#C2S_HANDSHAKING_CLIENT_INTENTION} type replaces both the
 * creator and the reverse lookup, so handshakes coming from a Gate classic
 * deployment with legacy forwarding (long BungeeCord forwarding address) decode
 * instead of throwing.
 */
public final class VialitePacketRegistry extends DefaultPacketRegistry {

    public VialitePacketRegistry(final boolean isClientside, final int protocolVersion) {
        super(isClientside, protocolVersion);
        this.registerPacket(MCPackets.C2S_HANDSHAKING_CLIENT_INTENTION, VialiteHandshakePacket::new);
    }

}
