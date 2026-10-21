/*
 * Deterministic RED test for the silent CONFIGURATION stall in auto-read save/restore.
 *
 * The statement under test is net.raphimc.viaproxy.proxy.util.ChannelUtil:
 *
 *     restoreAutoRead():  setAutoRead(stack.pop())      // not atomic with the pop
 *     disableAutoRead():  stack.push(config().isAutoRead()); setAutoRead(false)
 *
 * The saved states are per-channel state with no synchronisation, and the four
 * call sites do not agree on a thread:
 *
 *   CompressionPacketHandler:47   disable  -> backend (p2s) event loop
 *   LoginPacketHandler:191        disable  -> backend (p2s) event loop (nested)
 *   CompressionPacketHandler:51   restore  -> CLIENT (c2p) event loop (write listener)
 *   ConfigurationPacketHandler:47 restore  -> backend (p2s) event loop
 *
 * Two restores overlapping pop the same stack; the value each one writes back is
 * whatever it popped, so the losing restore can write `false` last and leave the
 * backend channel permanently unreadable, with the saved stack empty and no
 * exception anywhere (both restore guards see autoRead == false).
 *
 * The schedule this test forces is the one an in-runtime trace of a real join
 * produced (a cold 1.20.2+ join through vialite, captured 2026-10-21):
 *
 *   p2s loop : disable -> pushed=true  autoRead=false      (CompressionPacketHandler:47)
 *   p2s loop : disable -> pushed=false autoRead=false      (LoginPacketHandler:191)
 *   c2p loop : restore -> pop()=false                      (CompressionPacketHandler:51)
 *   p2s loop : restore -> pop()=true,  autoRead=true       (ConfigurationPacketHandler:47)
 *   c2p loop : setAutoRead(false)      <- the loser's write-back lands last
 *
 * Determinism without touching the class under test: the test pre-installs its own
 * Stack under the very attribute ChannelUtil uses ("last-auto-read"), so the real
 * implementation calls the real Stack API and the test only decides *when* the
 * losing restore's write-back is allowed to land. Nothing in ChannelUtil is
 * modified, widened or re-ordered - the leak is produced by the shipped code.
 *
 * Exit 1 = the channel was left unreadable (RED, the defect is present).
 * Exit 0 = every restore wrote a value that leaves the channel reading.
 *
 * Compile against the verbatim upstream ChannelUtil for RED; compile against the
 * event-loop-confined overlay ChannelUtil for GREEN (see run.sh).
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
import java.util.Stack;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import net.raphimc.viaproxy.proxy.util.ChannelUtil;

public final class AutoReadOrder {

    @SuppressWarnings("rawtypes")
    private static final AttributeKey LAST_AUTO_READ = AttributeKey.valueOf("last-auto-read");

    /** Stack whose pop() can be parked, so the losing restore writes back last. */
    static final class ParkedStack extends Stack<Boolean> {

        private final CountDownLatch popped = new CountDownLatch(1);
        private final CountDownLatch release = new CountDownLatch(1);
        private final boolean park;
        private boolean parked;

        ParkedStack(final boolean park) {
            this.park = park;
        }

        @Override
        public Boolean pop() {
            final Boolean value;
            synchronized (this) {
                value = super.pop();
            }
            popped.countDown();
            boolean doPark = false;
            if (park) {
                synchronized (this) {
                    if (!parked) {
                        parked = true;
                        doPark = true;
                    }
                }
            }
            if (doPark) {
                try {
                    if (!release.await(10, TimeUnit.SECONDS)) {
                        System.out.println("WARN: park was not released within 10s");
                    }
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            }
            return value;
        }

        void awaitPopped() throws InterruptedException {
            if (!popped.await(10, TimeUnit.SECONDS)) {
                throw new IllegalStateException("the losing restore never reached pop()");
            }
        }

        void release() {
            release.countDown();
        }
    }

    /**
     * No hook, no parking: the production call shape - two disables on the backend
     * loop and two restores started together from the backend and client loops -
     * repeated N times. Used as the GREEN stress for the event-loop-confined fix
     * (and as a stochastic reference run for the verbatim upstream file).
     */
    private static void stress(final int iterations) throws Exception {
        final EventLoopGroup group = new NioEventLoopGroup(2);
        final EventLoop backendLoop = group.next();
        final EventLoop clientLoop = group.next();
        final Channel server = new ServerBootstrap()
                .group(backendLoop)
                .channel(NioServerSocketChannel.class)
                .childHandler(new ChannelInitializer<SocketChannel>() {
                    protected void initChannel(final SocketChannel ch) {
                    }
                })
                .bind(new InetSocketAddress("127.0.0.1", 0)).sync().channel();
        final int port = ((InetSocketAddress) server.localAddress()).getPort();
        int leaks = 0;
        int errors = 0;
        int stale = 0;
        try {
            for (int i = 0; i < iterations; i++) {
                final Channel backend = new Bootstrap()
                        .group(backendLoop)
                        .channel(NioSocketChannel.class)
                        .handler(new ChannelInitializer<SocketChannel>() {
                            protected void initChannel(final SocketChannel ch) {
                            }
                        })
                        .connect("127.0.0.1", port).sync().channel();
                backendLoop.submit(() -> {
                    ChannelUtil.disableAutoRead(backend);
                    ChannelUtil.disableAutoRead(backend);
                }).sync();

                final CountDownLatch go = new CountDownLatch(1);
                final io.netty.util.concurrent.Future<?> c2pRestore = clientLoop.submit(() -> {
                    go.await();
                    try {
                        ChannelUtil.restoreAutoRead(backend);
                    } catch (Throwable t) {
                        throw t;
                    }
                    return null;
                });
                final io.netty.util.concurrent.Future<?> ackRestore = backendLoop.submit(() -> {
                    go.await();
                    try {
                        ChannelUtil.restoreAutoRead(backend);
                    } catch (Throwable t) {
                        throw t;
                    }
                    return null;
                });
                go.countDown();
                try {
                    c2pRestore.sync();
                    ackRestore.sync();
                } catch (Throwable t) {
                    errors++;
                }
                backendLoop.submit(() -> {
                }).sync();

                if (!backend.config().isAutoRead()) {
                    leaks++;
                }
                final Object saved = backend.attr(LAST_AUTO_READ).get();
                if (saved instanceof java.util.Collection<?> collection && !collection.isEmpty()) {
                    stale++;
                }
                backend.close().sync();
            }
        } finally {
            server.close().sync();
            group.shutdownGracefully().sync();
        }
        System.out.printf("iterations=%d autoRead_left_false=%d (%.4f%%) restore_errors=%d "
                        + "stale_saved_states=%d%n",
                iterations, leaks, 100.0 * leaks / iterations, errors, stale);
        if (leaks > 0) {
            System.out.println("FAIL: channel left permanently unreadable with no exception raised");
            System.exit(1);
        }
        System.out.println("PASS: every channel restored autoRead");
    }

    public static void main(final String[] args) throws Exception {
        final String mode = args.length > 0 ? args[0] : "red";
        if (mode.equals("stress")) {
            stress(args.length > 1 ? Integer.parseInt(args[1]) : 20000);
            return;
        }
        final boolean healthyControl = mode.equals("healthy");
        final EventLoopGroup group = new NioEventLoopGroup(2);
        final EventLoop backendLoop = group.next(); // p2s
        final EventLoop clientLoop = group.next();  // c2p
        final Channel server = new ServerBootstrap()
                .group(backendLoop)
                .channel(NioServerSocketChannel.class)
                .childHandler(new ChannelInitializer<SocketChannel>() {
                    protected void initChannel(final SocketChannel ch) {
                    }
                })
                .bind(new InetSocketAddress("127.0.0.1", 0)).sync().channel();
        final int port = ((InetSocketAddress) server.localAddress()).getPort();

        int leaks = 0;
        int errors = 0;
        int stale = 0;
        try {
            final Channel backend = new Bootstrap()
                    .group(backendLoop)
                    .channel(NioSocketChannel.class)
                    .handler(new ChannelInitializer<SocketChannel>() {
                        protected void initChannel(final SocketChannel ch) {
                        }
                    })
                    .connect("127.0.0.1", port).sync().channel();

            final ParkedStack stack = new ParkedStack(!healthyControl);
            backend.attr(LAST_AUTO_READ).set(stack);

            // CompressionPacketHandler:47 and LoginPacketHandler:191, both on the p2s loop.
            // The second disable runs while the first one has already turned reads off,
            // so it pushes `false` - exactly what the runtime trace shows.
            backendLoop.submit(() -> {
                ChannelUtil.disableAutoRead(backend);
                ChannelUtil.disableAutoRead(backend);
            }).sync();
            System.out.printf("after the two disables: autoRead=%s saved=%s%n",
                    backend.config().isAutoRead(), stack);

            if (healthyControl) {
                // Control: the healthy ordering - both restores run one after the other on
                // the backend loop, which is what a non-overlapping join does.
                backendLoop.submit(() -> {
                    ChannelUtil.restoreAutoRead(backend);
                    ChannelUtil.restoreAutoRead(backend);
                }).sync();
            } else {
                // CompressionPacketHandler:51 - the SetCompression write listener, which
                // runs on the CLIENT channel's loop, pops first and parks before writing.
                final io.netty.util.concurrent.Future<?> loser = clientLoop.submit(() -> {
                    try {
                        ChannelUtil.restoreAutoRead(backend);
                    } catch (Throwable t) {
                        System.out.println("loser restore threw: " + t);
                        throw t;
                    }
                    return null;
                });
                stack.awaitPopped();
                // ConfigurationPacketHandler:47 - on the p2s loop, pops the remaining value
                // and writes it back while the loser is still parked.
                backendLoop.submit(() -> {
                    try {
                        ChannelUtil.restoreAutoRead(backend);
                    } catch (Throwable t) {
                        System.out.println("winner restore threw: " + t);
                        throw t;
                    }
                }).sync();
                System.out.printf("after the winning restore: autoRead=%s saved=%s%n",
                        backend.config().isAutoRead(), stack);
                stack.release();
                try {
                    loser.sync();
                } catch (Throwable t) {
                    errors++;
                    System.out.println("loser restore failed: " + t);
                }
            }

            // Let any deferred transition on the backend loop finish before sampling.
            backendLoop.submit(() -> {
            }).sync();

            final boolean reading = backend.config().isAutoRead();
            final boolean empty = stack.isEmpty();
            System.out.printf("final: autoRead=%s saved_depth=%d restore_errors=%d%n",
                    reading, stack.size(), errors);
            if (!reading) {
                leaks++;
            }
            if (empty == false) {
                stale++;
            }
            backend.close().sync();
        } finally {
            server.close().sync();
            group.shutdownGracefully().sync();
        }

        if (healthyControl) {
            if (leaks == 0 && errors == 0) {
                System.out.println("PASS (control): a single restore leaves the channel reading");
                return;
            }
            System.out.println("FAIL (control): the harness itself broke the channel");
            System.exit(2);
        }

        if (leaks > 0) {
            System.out.println("FAIL: the backend channel was left with autoRead=false and an "
                    + "empty saved stack - it will never be read again, and nothing was logged");
            System.exit(1);
        }
        System.out.println("PASS: every restore wrote back a value that leaves the channel reading");
    }
}
