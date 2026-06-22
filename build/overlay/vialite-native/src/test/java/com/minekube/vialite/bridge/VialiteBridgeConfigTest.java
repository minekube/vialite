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
            () -> VialiteLauncher.parseConfig(json));

        assertTrue(ex.getMessage().contains("exactly one backend"));
    }

    @Test
    void writesLegacyForwardingViaProxyYaml() {
        VialiteLauncher.NativeConfig config = VialiteLauncher.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"legacy"}
            ]}
            """);

        String yaml = VialiteLauncher.toViaProxyYaml(config);

        assertTrue(yaml.contains("bind-address: \"127.0.0.1:25590\""));
        assertTrue(yaml.contains("target-address: \"127.0.0.1:25566\""));
        assertTrue(yaml.contains("target-version: \"Auto Detect (1.7+ servers)\""));
        assertTrue(yaml.contains("bungeecord-player-info-passthrough: true"));
    }

    @Test
    void normalizesAutoVersionForViaProxyConfigLoader() {
        VialiteLauncher.NativeConfig config = VialiteLauncher.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"none"}
            ]}
            """);

        String yaml = VialiteLauncher.toViaProxyYaml(config);

        assertTrue(yaml.contains("target-version: \"Auto Detect (1.7+ servers)\""));
    }

    @Test
    void quotesYamlStringScalars() {
        VialiteLauncher.NativeConfig config = VialiteLauncher.parseConfig("""
            {"bind":"[::1]:25590","backends":[
              {"name":"lobby","address":"[::1]:25566","version":"1.20.4","detect":false,"forwarding":"none"}
            ]}
            """);

        String yaml = VialiteLauncher.toViaProxyYaml(config);

        assertTrue(yaml.contains("bind-address: \"[::1]:25590\""));
        assertTrue(yaml.contains("target-address: \"[::1]:25566\""));
        assertTrue(yaml.contains("target-version: \"1.20.4\""));
    }

    @Test
    void rejectsVelocityForwardingUntilProven() {
        VialiteLauncher.NativeConfig config = VialiteLauncher.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"velocity"}
            ]}
            """);

        IllegalArgumentException ex = assertThrows(IllegalArgumentException.class,
            () -> VialiteLauncher.toViaProxyYaml(config));

        assertTrue(ex.getMessage().contains("velocity"));
    }
}
