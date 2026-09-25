package com.minekube.vialite.bridge;

import io.netty.channel.Channel;
import io.netty.channel.ChannelHandler;
import net.raphimc.netminecraft.constants.MCPipeline;
import net.raphimc.viaproxy.proxy.client2proxy.Client2ProxyChannelInitializer;

import java.util.function.Supplier;

/**
 * Client-to-proxy channel initializer that installs {@link VialitePacketRegistry}.
 *
 * <p>ViaProxy sets the per-channel packet registry inside
 * {@code Client2ProxyChannelInitializer#initChannel}; the packet codec reads that
 * attribute per decoded frame, so replacing it right after the upstream
 * initialization takes effect for the handshake that follows.
 */
public final class VialiteClient2ProxyChannelInitializer extends Client2ProxyChannelInitializer {

    public VialiteClient2ProxyChannelInitializer(final Supplier<ChannelHandler> handlerSupplier) {
        super(handlerSupplier);
    }

    @Override
    protected void initChannel(final Channel channel) {
        super.initChannel(channel);
        channel.attr(MCPipeline.PACKET_REGISTRY_ATTRIBUTE_KEY).set(new VialitePacketRegistry(false, -1));
    }

}
