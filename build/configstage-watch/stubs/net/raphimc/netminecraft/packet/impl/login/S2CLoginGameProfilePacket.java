package net.raphimc.netminecraft.packet.impl.login;

import java.util.UUID;

/**
 * Harness stub for netminecraft's server-login-success packet: the guard reads
 * the player name off it, and nothing else.
 */
public class S2CLoginGameProfilePacket {

    public final UUID uuid;
    public final String name;

    public S2CLoginGameProfilePacket(final UUID uuid, final String name) {
        this.uuid = uuid;
        this.name = name;
    }
}
