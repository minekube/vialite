/*
 * Race reproduction for net.raphimc.viaproxy.proxy.util.ChannelUtil.
 *
 * Each iteration replays what ViaProxy does on LoginSuccess with nextState=CONFIGURATION:
 *
 *   loop A (backend channel's loop): disableAutoRead(); disableAutoRead();
 *        ^ CompressionPacketHandler:47 and LoginPacketHandler:191 (both on the p2s loop)
 *   loop B (client c2p loop)       : restoreAutoRead();
 *        ^ CompressionPacketHandler:51, listener of the SetCompression write, runs on the c2p loop
 *   loop A (backend channel's loop): restoreAutoRead();
 *        ^ ConfigurationPacketHandler:47, listener of the client's LoginAcknowledged write
 *
 * The two restores are started together, which is what happens when a loopback client answers
 * LoginSuccess within the round trip of the SetCompression write.
 *
 * PASS  = every channel ends with autoRead == true (backend keeps reading).
 * FAIL  = some channel ends with autoRead == false and no exception anywhere: that channel's
 *         socket is never read again, the bytes pile up in the kernel receive queue and the
 *         join hangs silently until the client times out.
 *
 * Compile against the ChannelUtil variant under test (red/, red-deterministic/ or the overlay
 * fix) -- see run.sh.
 */
package racer;

import io.netty.bootstrap.Bootstrap;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelInitializer;
import io.netty.channel.EventLoop;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.channel.socket.nio.NioSocketChannel;
import io.netty.util.AttributeKey;

import java.net.InetSocketAddress;
import java.util.Collection;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicInteger;

import net.raphimc.viaproxy.proxy.util.ChannelUtil;

public final class AutoReadRace {

    private static final AttributeKey<?> LAST_AUTO_READ = AttributeKey.valueOf("last-auto-read");

    public static void main(final String[] args) throws Exception {
        final int iterations = args.length > 0 ? Integer.parseInt(args[0]) : 50000;

        final EventLoopGroup group = new NioEventLoopGroup(2);
        final EventLoop loopA = (EventLoop) group.next(); // backend channel's event loop
        final EventLoop loopB = (EventLoop) group.next(); // client (c2p) channel's event loop
        final AtomicInteger stuck = new AtomicInteger();
        final AtomicInteger errors = new AtomicInteger();
        final AtomicInteger stale = new AtomicInteger();

        final Channel server = new ServerBootstrap()
                .group(loopA)
                .channel(NioServerSocketChannel.class)
                .childHandler(new ChannelInitializer<SocketChannel>() {
                    protected void initChannel(final SocketChannel ch) {
                    }
                })
                .bind(new InetSocketAddress("127.0.0.1", 0)).sync().channel();
        final int port = ((InetSocketAddress) server.localAddress()).getPort();

        try {
            for (int i = 0; i < iterations; i++) {
                final Channel backend = new Bootstrap()
                        .group(loopA)
                        .channel(NioSocketChannel.class)
                        .handler(new ChannelInitializer<SocketChannel>() {
                            protected void initChannel(final SocketChannel ch) {
                            }
                        })
                        .connect("127.0.0.1", port).sync().channel();

                loopA.submit(() -> {
                    ChannelUtil.disableAutoRead(backend); // CompressionPacketHandler
                    ChannelUtil.disableAutoRead(backend); // LoginPacketHandler
                }).sync();

                final CountDownLatch go = new CountDownLatch(1);
                final io.netty.util.concurrent.Future<?> c2pRestore = loopB.submit(() -> {
                    try {
                        go.await();
                        ChannelUtil.restoreAutoRead(backend); // CompressionPacketHandler listener
                    } catch (final Throwable t) {
                        errors.incrementAndGet();
                    }
                    return null;
                });
                final io.netty.util.concurrent.Future<?> ackRestore = loopA.submit(() -> {
                    try {
                        go.await();
                        ChannelUtil.restoreAutoRead(backend); // ConfigurationPacketHandler listener
                    } catch (final Throwable t) {
                        errors.incrementAndGet();
                    }
                    return null;
                });
                go.countDown();
                c2pRestore.sync();
                ackRestore.sync();
                // let transitions deferred onto the backend loop run before sampling
                loopA.submit(() -> {
                }).sync();

                if (!backend.config().isAutoRead()) {
                    stuck.incrementAndGet();
                }
                final Object saved = backend.attr(LAST_AUTO_READ).get();
                if (saved instanceof Collection<?> collection && !collection.isEmpty()) {
                    stale.incrementAndGet();
                }

                backend.close().sync();
            }
        } finally {
            server.close().sync();
            group.shutdownGracefully().sync();
        }

        System.out.printf("iterations=%d autoRead_left_false=%d (%.4f%%) restore_errors=%d stale_saved_states=%d%n",
                iterations, stuck.get(), 100.0 * stuck.get() / iterations, errors.get(), stale.get());
        if (stuck.get() > 0) {
            System.out.println("FAIL: channel left permanently unreadable with no exception raised");
            System.exit(1);
        }
        System.out.println("PASS: every channel restored autoRead");
    }

}
