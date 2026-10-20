package com.viaversion.viaversion.api.protocol.version;

/** Harness stub for ViaVersion's ProtocolVersion: only the 1.20.2 comparison matters. */
public final class ProtocolVersion {

    public static final ProtocolVersion v1_20_1 = new ProtocolVersion(763);
    public static final ProtocolVersion v1_20_2 = new ProtocolVersion(764);

    private final int version;

    private ProtocolVersion(final int version) {
        this.version = version;
    }

    public boolean newerThanOrEqualTo(final ProtocolVersion other) {
        return this.version >= other.version;
    }
}
