package com.minekube.vialite.bridge;

import com.viaversion.viaversion.api.protocol.version.ProtocolVersion;
import io.netty.buffer.Unpooled;
import io.netty.channel.embedded.EmbeddedChannel;
import net.raphimc.netminecraft.packet.impl.login.S2CLoginGameProfilePacket;
import net.raphimc.viaproxy.proxy.session.ProxyConnection;
import net.raphimc.viaproxy.util.logging.Logger;

import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

/**
 * Behavioural proof for the CONFIGURATION stage of {@link VialitePreConnectGuard}.
 *
 * <p>The guard file under test is the real overlay source; only ViaProxy's
 * neighbours (Logger, ProxyConnection, the login-success packet, the backend
 * route descriptor) are stubbed, and Netty is the real 4.2.x library. Each case
 * drives the actual pipeline handlers on an EmbeddedChannel, so what is proven
 * is the guard's behaviour, not a copy of it:
 *
 * <ul>
 *   <li>{@code fires-when-nothing-clientbound}: LoginSuccess written to the
 *       client, nothing after it -> the connection is closed and one ERROR names
 *       the stage, the player, the configured and resolved backend address, the
 *       bytes seen in both directions and the elapsed time;</li>
 *   <li>{@code does-not-fire-when-clientbound-bytes-arrive}: the same, followed
 *       by the ~40 kB configuration burst -> the join is left alone (this is the
 *       "must not fire on a healthy join" branch);</li>
 *   <li>{@code does-not-arm-for-pre-1.20.2-clients}: a client without a
 *       CONFIGURATION state is never armed;</li>
 *   <li>{@code diagnosis-names-stage-player-and-both-addresses}: the unwritable
 *       branch of the diagnosis text.</li>
 * </ul>
 *
 * Exit code 0 means every case passed; 1 means at least one branch behaved
 * differently. {@code run.sh} runs this against the overlay guard (all cases
 * PASS) and against {@code red/} - the same guard with the stage timer removed -
 * where {@code fires-when-nothing-clientbound} must FAIL. That is the RED half:
 * the test can see the defect.
 */
public final class ConfigurationStageProbe {

    private static final String PLAYER = "Foxof7207";
    private static final long DEADLINE_MS = 250L;
    private static final long WAIT_MS = 900L;
    private static final int SECONDARY_BYTES = 65;
    private static final int CONFIGURATION_BURST_BYTES = 40 * 1024;

    private static final List<String> FAILURES = new ArrayList<>();

    private ConfigurationStageProbe() {
    }

    public static void main(final String[] args) throws Exception {
        VialitePreConnectGuard.configure(null, null, DEADLINE_MS);
        run("fires-when-nothing-clientbound", ConfigurationStageProbe::firesWhenNothingClientbound);
        run("does-not-fire-when-clientbound-bytes-arrive", ConfigurationStageProbe::doesNotFireWhenBytesArrive);
        run("does-not-arm-for-pre-1.20.2-clients", ConfigurationStageProbe::doesNotArmForPreConfigurationClients);
        run("diagnosis-names-stage-player-and-both-addresses", ConfigurationStageProbe::diagnosisNamesEverything);

        if (FAILURES.isEmpty()) {
            System.out.println("ALL PASS");
            System.exit(0);
        }
        System.out.println(FAILURES.size() + " CHECK(S) FAILED");
        System.exit(1);
    }

    /**
     * LoginSuccess reaches the client and nothing follows: the join is stuck in
     * CONFIGURATION, so the guard must close it and name the stage.
     */
    private static void firesWhenNothingClientbound() throws Exception {
        final EmbeddedChannel channel = newChannel();
        try {
            VialitePreConnectGuard.markStage(channel, VialitePreConnectGuard.STAGE_DIAL);
            channel.writeOutbound(loginSuccess(PLAYER));
            Thread.sleep(WAIT_MS);
            channel.runPendingTasks();

            check("the stalled connection is closed", !channel.isOpen());
            final String error = Logger.lastError();
            check("one ERROR was logged", error != null);
            if (error == null) {
                return;
            }
            System.out.println("    logged: " + error);
            check("the message names the stage", error.contains("stage=configuration"));
            check("the message names the player", error.contains("player \"" + PLAYER + "\""));
            check("the message names the configured backend address",
                    error.contains("configured localhost:25000"));
            check("the message names the resolved backend address",
                    error.contains("resolved 127.0.0.1:25000"));
            check("the message reports zero clientbound bytes",
                    error.contains("clientbound bytes since LoginSuccess=0"));
            check("the message reports the elapsed time", error.contains("elapsed "));
        } finally {
            channel.finishAndReleaseAll();
        }
    }

    /**
     * The healthy path: LoginSuccess followed by the configuration burst within
     * milliseconds. The guard must not touch the connection.
     */
    private static void doesNotFireWhenBytesArrive() throws Exception {
        final EmbeddedChannel channel = newChannel();
        try {
            VialitePreConnectGuard.markStage(channel, VialitePreConnectGuard.STAGE_DIAL);
            channel.writeOutbound(loginSuccess(PLAYER));
            channel.writeOutbound(Unpooled.wrappedBuffer(new byte[CONFIGURATION_BURST_BYTES]));
            Thread.sleep(WAIT_MS);
            channel.runPendingTasks();

            check("the healthy connection is left open", channel.isOpen());
            check("no ERROR was logged", Logger.lastError() == null);
        } finally {
            channel.finishAndReleaseAll();
        }
    }

    /** A pre-1.20.2 client has no CONFIGURATION state, so there is nothing to bound. */
    private static void doesNotArmForPreConfigurationClients() throws Exception {
        ProxyConnection.clientVersion = ProtocolVersion.v1_20_1;
        final EmbeddedChannel channel = newChannel();
        try {
            VialitePreConnectGuard.markStage(channel, VialitePreConnectGuard.STAGE_DIAL);
            channel.writeOutbound(loginSuccess(PLAYER));
            Thread.sleep(WAIT_MS);
            channel.runPendingTasks();

            check("the connection is left open", channel.isOpen());
            check("no ERROR was logged", Logger.lastError() == null);
            check("the stage did not advance to CONFIGURATION",
                    stageOf(channel) < VialitePreConnectGuard.STAGE_CONFIGURATION);
        } finally {
            channel.finishAndReleaseAll();
            ProxyConnection.clientVersion = ProtocolVersion.v1_20_2;
        }
    }

    /** The unwritable branch has to name everything an operator needs. */
    private static void diagnosisNamesEverything() {
        final String message = VialitePreConnectGuard.configurationDiagnosis(
                45001, PLAYER, "the client channel was unwritable",
                15000L, 15012L, 0L, SECONDARY_BYTES, true, false);
        System.out.println("    message: " + message);
        check("the message names the stage", message.contains("stage=configuration"));
        check("the message names the player", message.contains("player \"" + PLAYER + "\""));
        check("the message names the configured backend address",
                message.contains("configured localhost:25000"));
        check("the message names the resolved backend address",
                message.contains("resolved 127.0.0.1:25000"));
        check("the message names the listener port", message.contains("listener port 45001"));
        check("the message names the unwritable branch", message.contains("unwritable"));
        check("the message reports clientbound bytes",
                message.contains("clientbound bytes since LoginSuccess=0"));
        check("the message reports serverbound bytes",
                message.contains("serverbound bytes since LoginSuccess=" + SECONDARY_BYTES));
        check("the message reports the elapsed time", message.contains("elapsed 15012 ms"));
    }

    private static EmbeddedChannel newChannel() {
        Logger.reset();
        // Head = socket side (byte counter), tail = packet side (login watch),
        // exactly the two positions VialiteClient2ProxyChannelInitializer uses.
        return new EmbeddedChannel(new VialitePreConnectGuard.TrafficCounter(),
                new VialitePreConnectGuard.LoginSuccessWatch());
    }

    private static S2CLoginGameProfilePacket loginSuccess(final String player) {
        return new S2CLoginGameProfilePacket(UUID.nameUUIDFromBytes(player.getBytes()), player);
    }

    private static int stageOf(final EmbeddedChannel channel) {
        final Integer stage = channel
                .attr(io.netty.util.AttributeKey.<Integer>valueOf("vialite_preconnect_stage"))
                .get();
        return stage == null ? -1 : stage;
    }

    private static void run(final String name, final Case testCase) {
        System.out.println("CASE " + name);
        final int before = FAILURES.size();
        try {
            testCase.run();
        } catch (Throwable t) {
            FAILURES.add(name + ": " + t);
            System.out.println("    FAILED: threw " + t);
        }
        if (FAILURES.size() == before) {
            System.out.println("RESULT " + name + " PASS");
        } else {
            System.out.println("RESULT " + name + " FAIL");
            System.out.println("    " + FAILURES.get(FAILURES.size() - 1));
        }
    }

    private static void check(final String what, final boolean ok) {
        if (ok) {
            System.out.println("    ok: " + what);
        } else {
            FAILURES.add(what);
            System.out.println("    FAILED: " + what);
        }
    }

    private interface Case {
        void run() throws Exception;
    }
}
