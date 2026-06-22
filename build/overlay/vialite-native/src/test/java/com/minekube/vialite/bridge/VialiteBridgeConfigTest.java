package com.minekube.vialite.bridge;

import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

final class VialiteBridgeConfigTest {
    @Test
    void rejectsMultipleBackends() {
        String json = """
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"a","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"none"},
              {"name":"b","address":"127.0.0.1:25567","version":"auto","detect":true,"forwarding":"none"}
            ]}
            """;

        IllegalArgumentException ex = assertThrows(IllegalArgumentException.class,
            () -> VialiteBridge.parseConfig(json));

        assertTrue(ex.getMessage().contains("exactly one backend"));
    }

    @Test
    void writesLegacyForwardingViaProxyYaml() {
        VialiteBridge.NativeConfig config = VialiteBridge.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"legacy"}
            ]}
            """);

        String yaml = VialiteBridge.toViaProxyYaml(config);

        assertTrue(yaml.contains("bind-address: 127.0.0.1:25590"));
        assertTrue(yaml.contains("target-address: 127.0.0.1:25566"));
        assertTrue(yaml.contains("target-version: Auto Detect (1.7+ servers)"));
        assertTrue(yaml.contains("bungeecord-player-info-passthrough: true"));
    }

    @Test
    void normalizesAutoVersionForViaProxyConfigLoader() {
        VialiteBridge.NativeConfig config = VialiteBridge.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"none"}
            ]}
            """);

        String yaml = VialiteBridge.toViaProxyYaml(config);

        assertTrue(yaml.contains("target-version: Auto Detect (1.7+ servers)"));
    }

    @Test
    void rejectsVelocityForwardingUntilProven() {
        VialiteBridge.NativeConfig config = VialiteBridge.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"velocity"}
            ]}
            """);

        IllegalArgumentException ex = assertThrows(IllegalArgumentException.class,
            () -> VialiteBridge.toViaProxyYaml(config));

        assertTrue(ex.getMessage().contains("velocity"));
    }
}
