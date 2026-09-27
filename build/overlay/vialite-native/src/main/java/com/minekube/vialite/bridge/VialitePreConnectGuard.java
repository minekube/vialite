package com.minekube.vialite.bridge;

import io.netty.channel.Channel;
import io.netty.util.AttributeKey;
import net.raphimc.viaproxy.util.logging.Logger;
import org.apache.logging.log4j.Level;

import java.net.SocketAddress;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * Bounds and names the hop's pre-connect stage.
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
 * <p>Nothing here changes translation, deadlines inside ViaProxy, or the
 * backend dial itself; the only behaviour added is "do not stay silent and do
 * not hold the join open forever". Deadline overrides come from the native
 * config ({@code handshake_deadline_ms}, {@code dial_deadline_ms}); 0 disables
 * a stage.
 */
public final class VialitePreConnectGuard {

    /** Accepted; nothing from Gate has been handled yet. */
    public static final int STAGE_ACCEPTED = 0;
    /** The handshake reached Client2ProxyHandler and the route was selected. */
    public static final int STAGE_HANDSHAKE = 1;
    /** The proxy is about to dial (or already dialled) the backend. */
    public static final int STAGE_DIAL = 2;

    private static final AttributeKey<Integer> STAGE =
            AttributeKey.valueOf("vialite_preconnect_stage");

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

    private static volatile long handshakeDeadlineMs = DEFAULT_HANDSHAKE_DEADLINE_MS;
    private static volatile long dialDeadlineMs = DEFAULT_DIAL_DEADLINE_MS;
    private static volatile ScheduledExecutorService scheduler;

    private VialitePreConnectGuard() {
    }

    static void configure(final Long handshakeMs, final Long dialMs) {
        if (handshakeMs != null && handshakeMs >= 0L) {
            handshakeDeadlineMs = handshakeMs;
        }
        if (dialMs != null && dialMs >= 0L) {
            dialDeadlineMs = dialMs;
        }
    }

    static long handshakeDeadlineMs() {
        return handshakeDeadlineMs;
    }

    static long dialDeadlineMs() {
        return dialDeadlineMs;
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

}
