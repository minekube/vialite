package net.raphimc.viaproxy.util.logging;

import org.apache.logging.log4j.Level;

import java.net.SocketAddress;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Harness stub for ViaProxy's Logger: keeps what the guard logs so the probe can
 * assert on the operator-visible diagnosis instead of on stdout formatting.
 */
public final class Logger {

    private static final List<String> LINES = Collections.synchronizedList(new ArrayList<>());

    private Logger() {
    }

    public static void u_log(final Level level, final String title, final SocketAddress address,
                             final Object gameProfile, final String msg) {
        LINES.add(level + " " + msg);
    }

    public static String lastError() {
        synchronized (LINES) {
            for (int i = LINES.size() - 1; i >= 0; i--) {
                if (LINES.get(i).startsWith("ERROR ")) {
                    return LINES.get(i);
                }
            }
        }
        return null;
    }

    public static void reset() {
        LINES.clear();
    }
}
