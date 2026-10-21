package com.minekube.vialite.bridge;

/**
 * Harness stub for the overlay's VialiteBridge: the guard's diagnoses all go
 * through describeRoute, which in the real class resolves the listener port to
 * "backend &lt;name&gt; (configured ..., resolved ...), listener port N".
 */
final class VialiteBridge {

    private VialiteBridge() {
    }

    static String describeRoute(final int port) {
        return "backend \"lobby\" (configured localhost:25000, resolved 127.0.0.1:25000), listener port "
                + port;
    }
}
