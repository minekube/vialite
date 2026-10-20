package net.raphimc.viaproxy.proxy.session;

import com.viaversion.viaversion.api.protocol.version.ProtocolVersion;
import io.netty.channel.Channel;

/**
 * Harness stub for ViaProxy's ProxyConnection: the guard only asks which client
 * version is behind the bridge connection, to know whether a CONFIGURATION
 * stage exists at all.
 */
public final class ProxyConnection {

    /** Client version the stub reports; v1_20_2 has CONFIGURATION, v1_20_1 does not. */
    public static volatile ProtocolVersion clientVersion = ProtocolVersion.v1_20_2;

    private ProxyConnection() {
    }

    public static ProxyConnection fromChannel(final Channel channel) {
        return new ProxyConnection();
    }

    public ProtocolVersion getClientVersion() {
        return clientVersion;
    }
}
