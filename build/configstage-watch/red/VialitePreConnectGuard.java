package com.minekube.vialite.bridge;

import com.viaversion.viaversion.api.protocol.version.ProtocolVersion;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelOutboundHandlerAdapter;
import io.netty.channel.ChannelPromise;
import io.netty.util.AttributeKey;
import net.raphimc.netminecraft.packet.impl.login.S2CLoginGameProfilePacket;
import net.raphimc.viaproxy.proxy.session.ProxyConnection;
import net.raphimc.viaproxy.util.logging.Logger;
import org.apache.logging.log4j.Level;

import java.net.SocketAddress;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Bounds and names the hop's stages: accept -> handshake -> backend dial, and
 * then CONFIGURATION.
 *
 * <p>The bridge is only reachable from Gate, on loopback, and Gate writes its
 * handshake immediately after dialling: accept -> handshake handled (the route
 * for the listener is selected) -> backend dial attempted. Everything a join
 * needs happens in that window, and until this class existed the window had no
 * upper bound and no diagnostic: a connection that was accepted but whose
 * handshake never reached {@code Client2ProxyHandler} (a stall anywhere in the
 * Netty/ViaVersion pipeline, an event-loop block, a flow-control hold) produced
 * <em>no console output at all</em>, opened no socket to any backend, and left
 * Gate's join parked on a read that - on a Gate without
 * {@code gate#1197} - effectively never times out. The operator sees "player
 * has connected, completing login" and then nothing, forever, while
 * {@code ss} shows one idle Gate->hop pair and no backend socket.
 *
 * <p>This guard is the fail-closed half of the fix: a dedicated daemon thread
 * (independent of Netty's event loops, so it still fires when an event loop is
 * blocked) watches every accepted bridge connection and, if the join has not
 * reached the next stage within its deadline, logs one ERROR naming the
 * backend, the configured address, the resolved address and the stage that was
 * reached, then closes the connection. Gate therefore sees a closed bridge
 * connection in seconds instead of a parked join, and the console says why.
 *
 * <p>The pre-connect stages end at the backend dial, which is one stage too
 * early for the other silent class: a join that logs {@code Connected
 * successfully! Switching to CONFIGURATION state} and then emits nothing. The
 * backend socket keeps receiving (the runtime reads it) but nothing is written
 * toward the client, so the client waits, Gate waits and nobody kicks. The
 * CONFIGURATION stage closes that hole: once LoginSuccess has been written
 * toward Gate's bridge connection ({@link LoginSuccessWatch}) a healthy
 * 1.20.2+ join delivers the configuration burst (plugin.Message plus ~40 kB of
 * registry data) within milliseconds, so a client that still has zero
 * clientbound bytes after {@code configuration_deadline_ms} - or whose channel
 * is unwritable at that point - is stuck. The guard logs one named diagnosis
 * (player, backend, configured and resolved address, stage, bytes in both
 * directions, elapsed time) and closes the connection instead of parking the
 * join.
 *
 * <p>Nothing here changes translation, deadlines inside ViaProxy, or the
 * backend dial itself; the only behaviour added is "do not stay silent and do
 * not hold the join open forever". Deadline overrides come from the native
 * config ({@code handshake_deadline_ms}, {@code dial_deadline_ms},
 * {@code configuration_deadline_ms}); 0 disables a stage.
 */
public final class VialitePreConnectGuard {

    /** Accepted; nothing from Gate has been handled yet. */
    public static final int STAGE_ACCEPTED = 0;
    /** The handshake reached Client2ProxyHandler and the route was selected. */
    public static final int STAGE_HANDSHAKE = 1;
    /** The proxy is about to dial (or already dialled) the backend. */
    public static final int STAGE_DIAL = 2;
    /** LoginSuccess went to the client; the client is in CONFIGURATION. */
    public static final int STAGE_CONFIGURATION = 3;

    private static final String COUNTER_HANDLER_NAME = "vialite-clientbound-counter";
    private static final String LOGIN_SUCCESS_WATCH_NAME = "vialite-login-success-watch";

    private static final AttributeKey<Integer> STAGE =
            AttributeKey.valueOf("vialite_preconnect_stage");
    private static final AttributeKey<Watch> WATCH =
            AttributeKey.valueOf("vialite_configuration_watch");

    /**
     * Gate writes its handshake right after dialling, so ten seconds is already
     * an order of magnitude more than a healthy hop needs.
     */
    private static final long DEFAULT_HANDSHAKE_DEADLINE_MS = 10_000L;
    /**
     * Covers the auto-detection ping (3000 ms inside ViaProxy) plus the backend
     * connect, with room to spare.
     */
    private static final long DEFAULT_DIAL_DEADLINE_MS = 25_000L;
    /**
     * A client in CONFIGURATION that is sent nothing is stuck: the runtime has
     * already delivered LoginSuccess, and a healthy 1.20.2+ join follows it with
     * the configuration burst within milliseconds. Fifteen seconds is far above
     * that and still below Gate's default read timeout (30000 ms, see
     * {@code pkg/edition/java/config}), so the hop fails the join and names the
     * stage before Gate's own read timeout would fire.
     */
    private static final long DEFAULT_CONFIGURATION_DEADLINE_MS = 15_000L;

    private static volatile long handshakeDeadlineMs = DEFAULT_HANDSHAKE_DEADLINE_MS;
    private static volatile long dialDeadlineMs = DEFAULT_DIAL_DEADLINE_MS;
    private static volatile long configurationDeadlineMs = DEFAULT_CONFIGURATION_DEADLINE_MS;
    private static volatile ScheduledExecutorService scheduler;

    private VialitePreConnectGuard() {
    }

    static void configure(final Long handshakeMs, final Long dialMs, final Long configurationMs) {
        if (handshakeMs != null && handshakeMs >= 0L) {
            handshakeDeadlineMs = handshakeMs;
        }
        if (dialMs != null && dialMs >= 0L) {
            dialDeadlineMs = dialMs;
        }
        if (configurationMs != null && configurationMs >= 0L) {
            configurationDeadlineMs = configurationMs;
        }
    }

    static long handshakeDeadlineMs() {
        return handshakeDeadlineMs;
    }

    static long dialDeadlineMs() {
        return dialDeadlineMs;
    }

    static long configurationDeadlineMs() {
        return configurationDeadlineMs;
    }

    /** Records how far a connection has progressed. Never moves a stage back. */
    static void markStage(final Channel channel, final int stage) {
        if (channel == null) {
            return;
        }
        final Integer current = channel.attr(STAGE).get();
        if (current == null || current < stage) {
            channel.attr(STAGE).set(stage);
        }
    }

    /**
     * Arms the guard for a freshly accepted bridge connection. Called from the
     * channel initializer <em>before</em> the upstream pipeline is installed, so
     * a stall inside that installation is covered too.
     */
    static void arm(final Channel channel) {
        if (channel == null) {
            return;
        }
        channel.attr(STAGE).set(STAGE_ACCEPTED);
        final int port = routePort(channel);
        log(Level.INFO, channel, "accepted bridge connection for " + VialiteBridge.describeRoute(port)
                + "; waiting for Gate's handshake (deadline " + handshakeDeadlineMs + " ms)");
        final long handshakeMs = handshakeDeadlineMs;
        final long dialMs = dialDeadlineMs;
        if (handshakeMs > 0L) {
            schedule(channel, port, handshakeMs, STAGE_HANDSHAKE);
        }
        if (dialMs > 0L) {
            schedule(channel, port, dialMs, STAGE_DIAL);
        }
    }

    /**
     * Installs the two handlers the CONFIGURATION stage needs. Called from
     * {@link VialiteClient2ProxyChannelInitializer} once the upstream pipeline
     * exists, so the counters see socket-level traffic and the login-success
     * watch sees the packets the proxy writes toward the client.
     *
     * <p>{@link TrafficCounter} sits at the head of the pipeline (closest to the
     * socket): clientbound bytes are counted when the write to the socket
     * succeeds, so a write parked in a wedged outbound buffer still counts as
     * "nothing delivered". {@link LoginSuccessWatch} sits at the tail, where
     * outbound messages are still {@code Packet} objects, so it can recognise
     * LoginSuccess instead of guessing from bytes.
     */
    static void installConfigurationWatch(final Channel channel) {
        if (channel == null) {
            return;
        }
        try {
            if (channel.pipeline().get(COUNTER_HANDLER_NAME) == null) {
                channel.pipeline().addFirst(COUNTER_HANDLER_NAME, new TrafficCounter());
            }
            if (channel.pipeline().get(LOGIN_SUCCESS_WATCH_NAME) == null) {
                channel.pipeline().addLast(LOGIN_SUCCESS_WATCH_NAME, new LoginSuccessWatch());
            }
        } catch (Throwable t) {
            log(Level.WARN, channel, "could not install the CONFIGURATION stage watch: " + t);
        }
    }

    /**
     * Arms the CONFIGURATION stage timer: the runtime has written LoginSuccess
     * toward Gate's bridge connection, so the client is (for 1.20.2+) in
     * CONFIGURATION and must start receiving clientbound bytes.
     */
    static void armConfigurationStage(final Channel channel, final String player) {
        if (channel == null || !channel.isOpen()) {
            return;
        }
        if (!clientEntersConfiguration(channel)) {
            return; // pre-1.20.2 clients move straight to PLAY; no CONFIGURATION stage exists
        }
        final Watch watch = watch(channel);
        watch.clientbound.set(0L);
        watch.serverbound.set(0L);
        watch.player = player == null ? "" : player;
        watch.armedNanos = System.nanoTime();
        // Last: TrafficCounter only counts while armed, and both run on the
        // client channel's event loop, so the LoginSuccess write that got us
        // here can never be counted as CONFIGURATION traffic.
        watch.armed = true;
        markStage(channel, STAGE_CONFIGURATION);

        final int port = routePort(channel);
        final long deadlineMs = configurationDeadlineMs;
        log(Level.INFO, channel, "login success delivered to the client for "
                + VialiteBridge.describeRoute(port) + "; watching the CONFIGURATION stage for the "
                + "first clientbound byte (deadline " + deadlineMs + " ms)");
        if (deadlineMs <= 0L) {
            return;
        }
        try {
            // MUTATION (red/VialitePreConnectGuard.java): the CONFIGURATION
            // stage timer is removed. Everything else is the shipped guard, so
            // this copy must make the "fires-when-nothing-clientbound" case fail
            // and leave every other case passing.
            scheduler().schedule(() -> {
            }, deadlineMs, TimeUnit.MILLISECONDS);
        } catch (Throwable t) {
            log(Level.WARN, channel, "could not arm the CONFIGURATION stage guard: " + t);
        }
    }

    private static void schedule(final Channel channel, final int port, final long afterMs, final int requiredStage) {
        try {
            scheduler().schedule(() -> fire(channel, port, afterMs, requiredStage), afterMs, TimeUnit.MILLISECONDS);
        } catch (Throwable t) {
            log(Level.WARN, channel, "could not arm the pre-connect guard: " + t);
        }
    }

    private static void fire(final Channel channel, final int port, final long afterMs, final int requiredStage) {
        try {
            if (!channel.isOpen()) {
                return; // the connection resolved itself (or a closer guard fired)
            }
            final Integer stage = channel.attr(STAGE).get();
            if (stage != null && stage >= requiredStage) {
                return; // progressed in time
            }
            final int reached = stage == null ? STAGE_ACCEPTED : stage;
            final String message = "the join made no progress through the translation bridge: "
                    + VialiteBridge.describeRoute(port)
                    + ", stage=" + stageName(requiredStage) + " reached=" + stageName(reached)
                    + " after " + afterMs + " ms, and no backend was contacted. Closing this bridge "
                    + "connection so Gate fails the join instead of waiting on a silent connection "
                    + "(a hop that cannot reach its backend must not hold the join open).";
            log(Level.ERROR, channel, message);
            channel.close();
        } catch (Throwable t) {
            log(Level.WARN, channel, "pre-connect guard failed: " + t);
        }
    }

    private static void fireConfiguration(final Channel channel, final int port, final long afterMs) {
        try {
            if (!channel.isOpen()) {
                return; // already closed: Gate saw the join fail
            }
            final Watch watch = watch(channel);
            if (!watch.armed) {
                return;
            }
            final Integer stage = channel.attr(STAGE).get();
            if (stage == null || stage < STAGE_CONFIGURATION) {
                return; // not past LoginSuccess, so the pre-connect stages own this connection
            }
            final long clientbound = watch.clientbound.get();
            if (clientbound > 0L) {
                // The configuration phase is being fed. Writability is only
                // judged when nothing was delivered: a healthy ~40 kB burst can
                // dip below the high watermark transiently, and closing a join
                // for that would be worse than the stall this guard detects.
                return;
            }
            final long serverbound = watch.serverbound.get();
            final long elapsedMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - watch.armedNanos);
            final boolean active = channel.isActive();
            final boolean writable = channel.isWritable();
            final String verdict = !active
                    ? "the client channel was inactive"
                    : !writable
                    ? "the client channel was unwritable"
                    : "no clientbound bytes were written toward the client";
            log(Level.ERROR, channel, configurationDiagnosis(port, watch.player, verdict, afterMs,
                    elapsedMs, clientbound, serverbound, active, writable));
            channel.close();
        } catch (Throwable t) {
            log(Level.WARN, channel, "CONFIGURATION stage guard failed: " + t);
        }
    }

    /**
     * The one named statement an operator gets for a silent CONFIGURATION stage:
     * player, backend (configured and resolved address), the stage that was
     * reached, the bytes seen in both directions and how long the hop waited.
     */
    static String configurationDiagnosis(final int port, final String player, final String verdict,
                                        final long deadlineMs, final long elapsedMs,
                                        final long clientboundBytes, final long serverboundBytes,
                                        final boolean active, final boolean writable) {
        return "the join reached the CONFIGURATION stage and then stopped moving: "
                + VialiteBridge.describeRoute(port)
                + ", player \"" + (player == null || player.isEmpty() ? "unknown" : player) + "\""
                + ", stage=configuration reached=configuration"
                + ", " + verdict
                + " (deadline " + deadlineMs + " ms, elapsed " + elapsedMs + " ms)"
                + ", clientbound bytes since LoginSuccess=" + clientboundBytes
                + ", serverbound bytes since LoginSuccess=" + serverboundBytes
                + ", client channel active=" + active + " writable=" + writable
                + ". Closing this bridge connection so Gate fails the join instead of waiting on a "
                + "silent CONFIGURATION stage (a hop that cannot deliver the configuration phase must "
                + "not hold the join open).";
    }

    /**
     * True when the client behind this bridge connection is on a version that
     * has a CONFIGURATION state. Unknown (no ProxyConnection attribute yet) is
     * treated as yes: the byte counter protects healthy joins either way, and a
     * missing attribute is itself worth catching silently.
     */
    private static boolean clientEntersConfiguration(final Channel channel) {
        try {
            final ProxyConnection connection = ProxyConnection.fromChannel(channel);
            if (connection == null) {
                return true;
            }
            final ProtocolVersion clientVersion = connection.getClientVersion();
            if (clientVersion == null) {
                return true;
            }
            return clientVersion.newerThanOrEqualTo(ProtocolVersion.v1_20_2);
        } catch (Throwable t) {
            return true;
        }
    }

    /**
     * Counts socket-level traffic in both directions. Inbound counts raw bytes
     * read from the client; clientbound counts bytes whose write to the socket
     * succeeded, so bytes parked in an unwritable outbound buffer do not count
     * as delivered.
     */
    static final class TrafficCounter extends ChannelDuplexHandler {

        @Override
        public void channelRead(final ChannelHandlerContext ctx, final Object msg) {
            if (msg instanceof ByteBuf byteBuf) {
                record(ctx.channel(), byteBuf.readableBytes(), false);
            }
            ctx.fireChannelRead(msg);
        }

        @Override
        public void write(final ChannelHandlerContext ctx, final Object msg, final ChannelPromise promise) {
            if (msg instanceof ByteBuf byteBuf) {
                final Channel channel = ctx.channel();
                final int bytes = byteBuf.readableBytes();
                promise.addListener(future -> {
                    if (future.isSuccess()) {
                        record(channel, bytes, true);
                    }
                });
            }
            ctx.write(msg, promise);
        }
    }

    /**
     * Recognises the runtime writing LoginSuccess toward Gate's bridge
     * connection - the packet whose handling logs "Connected successfully!
     * Switching to CONFIGURATION state" - and arms the stage after that write
     * completes, so the stage timer never counts LoginSuccess itself as
     * progress.
     */
    static final class LoginSuccessWatch extends ChannelOutboundHandlerAdapter {

        @Override
        public void write(final ChannelHandlerContext ctx, final Object msg, final ChannelPromise promise) {
            if (msg instanceof S2CLoginGameProfilePacket loginSuccess) {
                final Channel channel = ctx.channel();
                final String player = loginSuccess.name;
                promise.addListener(future -> {
                    if (future.isSuccess()) {
                        try {
                            channel.eventLoop().execute(() -> armConfigurationStage(channel, player));
                        } catch (Throwable t) {
                            armConfigurationStage(channel, player);
                        }
                    }
                });
            }
            ctx.write(msg, promise);
        }
    }

    private static void record(final Channel channel, final int bytes, final boolean clientbound) {
        if (bytes <= 0) {
            return;
        }
        final Watch watch = watch(channel);
        if (!watch.armed) {
            return; // before LoginSuccess: not CONFIGURATION traffic
        }
        if (clientbound) {
            watch.clientbound.addAndGet(bytes);
        } else {
            watch.serverbound.addAndGet(bytes);
        }
    }

    private static Watch watch(final Channel channel) {
        Watch current = channel.attr(WATCH).get();
        if (current == null) {
            final Watch created = new Watch();
            current = channel.attr(WATCH).setIfAbsent(created);
            if (current == null) {
                current = created;
            }
        }
        return current;
    }

    private static void log(final Level level, final Channel channel, final String message) {
        try {
            final SocketAddress remote = channel == null ? null : channel.remoteAddress();
            Logger.u_log(level, "vialite", remote, null, message);
        } catch (Throwable ignored) {
            // logging must never break the guard
        }
    }

    private static int routePort(final Channel channel) {
        try {
            final SocketAddress local = channel.localAddress();
            if (local instanceof java.net.InetSocketAddress inet) {
                return inet.getPort();
            }
        } catch (Throwable ignored) {
        }
        return -1;
    }

    private static String stageName(final int stage) {
        switch (stage) {
            case STAGE_CONFIGURATION:
                return "configuration";
            case STAGE_DIAL:
                return "backend-dial";
            case STAGE_HANDSHAKE:
                return "handshake";
            default:
                return "accept";
        }
    }

    private static ScheduledExecutorService scheduler() {
        ScheduledExecutorService current = scheduler;
        if (current == null) {
            synchronized (VialitePreConnectGuard.class) {
                current = scheduler;
                if (current == null) {
                    current = Executors.newSingleThreadScheduledExecutor(runnable -> {
                        final Thread thread = new Thread(runnable, "vialite-preconnect-guard");
                        thread.setDaemon(true);
                        return thread;
                    });
                    scheduler = current;
                }
            }
        }
        return current;
    }

    /** Per-connection CONFIGURATION stage state. */
    static final class Watch {
        final AtomicLong clientbound = new AtomicLong();
        final AtomicLong serverbound = new AtomicLong();
        volatile long armedNanos;
        volatile boolean armed;
        volatile String player = "";
    }

}
