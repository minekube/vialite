package com.minekube.vialite.bridge;

import io.netty.buffer.ByteBuf;
import net.raphimc.netminecraft.constants.IntendedState;
import net.raphimc.netminecraft.packet.PacketTypes;
import net.raphimc.netminecraft.packet.impl.handshaking.C2SHandshakingClientIntentionPacket;

/**
 * Handshake packet with the address limit a backend-side proxy needs.
 *
 * <p>Upstream {@code C2SHandshakingClientIntentionPacket} reads the handshake
 * address with the vanilla client limit ({@code readString(byteBuf, 255)}), i.e.
 * at most 255 characters / 1020 bytes. That limit describes what a vanilla
 * <em>client</em> sends. A proxy in front of a backend server does not send a
 * vanilla address: BungeeCord-style ("legacy") IP forwarding appends
 * {@code host\0ip\0uuid\0<profile properties JSON>} to it, and BungeeCord, Spigot
 * and Paper therefore read this field with {@code Short.MAX_VALUE}.
 *
 * <p>For an authenticated (online mode) player the appended properties carry the
 * Mojang profile properties - the signed {@code textures} blob is roughly 2.8 kB -
 * so a Gate classic -> vialite hop with {@code forwarding.mode: legacy} always
 * exceeds the vanilla limit. The stock packet then throws
 * {@code DecoderException: The received encoded string buffer length is longer
 * than maximum allowed (2874 > 1020)} (or {@code ... length is longer than maximum
 * allowed (974 > 255)} for smaller payloads) while the handshake is decoded, which
 * closes the connection before Via translation starts: no authenticated player can
 * join, on any client version.
 *
 * <p>This override only widens the limit for the backend-side handshake address;
 * everything else is upstream behaviour. Nothing here changes what vialite sends.
 */
public final class VialiteHandshakePacket extends C2SHandshakingClientIntentionPacket {

    /**
     * Address limit for the backend-side handshake, matching what BungeeCord,
     * Spigot and Paper accept for the BungeeCord forwarding format.
     */
    public static final int MAX_ADDRESS_LENGTH = Short.MAX_VALUE;

    @Override
    public void read(final ByteBuf byteBuf, final int protocolVersion) {
        this.protocolVersion = PacketTypes.readVarInt(byteBuf);
        this.address = PacketTypes.readString(byteBuf, MAX_ADDRESS_LENGTH);
        this.port = byteBuf.readUnsignedShort();
        this.intendedState = IntendedState.byId(PacketTypes.readVarInt(byteBuf));
    }

}
