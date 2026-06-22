package com.minekube.vialite.bridge;

import com.google.gson.Gson;
import java.io.File;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import net.raphimc.viaproxy.ViaProxy;

public final class VialiteLauncher {
    private static final String VIA_PROXY_AUTO_DETECT_VERSION = "Auto Detect (1.7+ servers)";
    private static final Gson GSON = new Gson();

    private VialiteLauncher() {
    }

    public static void main(String[] args) throws Throwable {
        File configPath = configPath(args);
        NativeConfig config = parseConfig(Files.readString(configPath.toPath()));
        Path runDir = Files.createTempDirectory("vialite-viaproxy-");
        Path viaProxyConfig = runDir.resolve("viaproxy.yml");
        Files.writeString(viaProxyConfig, toViaProxyYaml(config));

        System.setProperty("java.awt.headless", "true");
        System.setProperty("skipUpdateCheck", "true");
        System.setProperty("ignoreSystemRequirements", "true");
        System.setProperty("user.dir", runDir.toString());
        ViaProxy.main(new String[]{"config", viaProxyConfig.toString()});
    }

    static NativeConfig parseConfig(String json) {
        NativeConfig config = GSON.fromJson(json, NativeConfig.class);
        if (config == null || config.backends == null || config.backends.size() != 1) {
            throw new IllegalArgumentException("vialite native config must contain exactly one backend");
        }
        if (config.bind == null || config.bind.isBlank()) {
            config.bind = "127.0.0.1:0";
        }
        NativeBackend backend = config.backends.get(0);
        if (backend.name == null || backend.name.isBlank()) {
            throw new IllegalArgumentException("backend name is required");
        }
        if (backend.address == null || backend.address.isBlank()) {
            throw new IllegalArgumentException("backend address is required");
        }
        if (backend.version == null || backend.version.isBlank() || backend.detect) {
            backend.version = "auto";
        }
        if (backend.forwarding == null || backend.forwarding.isBlank()) {
            backend.forwarding = "none";
        }
        return config;
    }

    static String toViaProxyYaml(NativeConfig config) {
        NativeBackend backend = config.backends.get(0);
        boolean legacyForwarding = switch (backend.forwarding.toLowerCase()) {
            case "none" -> false;
            case "legacy", "bungeecord" -> true;
            case "velocity" -> throw new IllegalArgumentException("velocity forwarding is not supported by this vialite runtime slice");
            default -> throw new IllegalArgumentException("unsupported forwarding mode: " + backend.forwarding);
        };

        return "bind-address: " + yamlString(config.bind) + "\n"
            + "target-address: " + yamlString(backend.address) + "\n"
            + "target-version: " + yamlString(viaProxyTargetVersion(backend.version)) + "\n"
            + "proxy-online-mode: false\n"
            + "auth-method: none\n"
            + "backend-haproxy: false\n"
            + "frontend-haproxy: false\n"
            + "ignore-protocol-translation-errors: false\n"
            + "suppress-client-protocol-errors: false\n"
            + "bungeecord-player-info-passthrough: " + legacyForwarding + "\n"
            + "rewrite-handshake-packet: true\n"
            + "rewrite-transfer-packets: true\n"
            + "log-client-status-requests: false\n";
    }

    static String viaProxyTargetVersion(String version) {
        if (version == null || version.isBlank() || "auto".equalsIgnoreCase(version)) {
            return VIA_PROXY_AUTO_DETECT_VERSION;
        }
        return version;
    }

    static String yamlString(String value) {
        return "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"") + "\"";
    }

    static File configPath(String[] args) {
        if (args.length != 2 || !"--config".equals(args[0]) || args[1].isBlank()) {
            throw new IllegalArgumentException("usage: vialite --config <native-config.json>");
        }
        return new File(args[1]);
    }

    static final class NativeConfig {
        String bind;
        String gateProtocol;
        List<NativeBackend> backends;
    }

    static final class NativeBackend {
        String name;
        String address;
        String version;
        boolean detect;
        String forwarding;
    }
}
