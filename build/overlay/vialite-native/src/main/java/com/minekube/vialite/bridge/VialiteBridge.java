package com.minekube.vialite.bridge;

import com.google.gson.Gson;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicBoolean;
import net.raphimc.viaproxy.ViaProxy;
import org.graalvm.nativeimage.IsolateThread;
import org.graalvm.nativeimage.c.function.CEntryPoint;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

public final class VialiteBridge {
    private static final String VIA_PROXY_AUTO_DETECT_VERSION = "Auto Detect (1.7+ servers)";
    private static final Gson GSON = new Gson();
    private static final AtomicBoolean INITIALIZED = new AtomicBoolean(false);
    private static final AtomicBoolean RUNNING = new AtomicBoolean(false);
    private static final Map<String, String> BACKENDS = new ConcurrentHashMap<>();

    private VialiteBridge() {
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

        return "bind-address: " + config.bind + "\n"
            + "target-address: " + backend.address + "\n"
            + "target-version: " + viaProxyTargetVersion(backend.version) + "\n"
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

    @CEntryPoint(name = "vialite_init")
    public static int init(IsolateThread thread, CCharPointer configJson) {
        String config = CTypeConversion.toJavaString(configJson);
        BACKENDS.clear();
        // Initial scaffold: expose one deterministic address until the Via runtime
        // parser is wired. The Go API and native ABI stay stable.
        if (config != null && !config.isBlank()) {
            BACKENDS.put("default", "127.0.0.1:0");
        }
        INITIALIZED.set(true);
        return 0;
    }

    @CEntryPoint(name = "vialite_run")
    public static int run(IsolateThread thread) {
        if (!INITIALIZED.get()) {
            return 2;
        }
        RUNNING.set(true);
        while (RUNNING.get()) {
            try {
                Thread.sleep(100L);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                RUNNING.set(false);
            }
        }
        return 0;
    }

    @CEntryPoint(name = "vialite_shutdown")
    public static int shutdown(IsolateThread thread) {
        RUNNING.set(false);
        return 0;
    }

    @CEntryPoint(name = "vialite_status")
    public static int status(IsolateThread thread) {
        return INITIALIZED.get() && RUNNING.get() ? 1 : 0;
    }

    @CEntryPoint(name = "vialite_backend_address")
    public static CCharPointer backendAddress(IsolateThread thread, CCharPointer backendName) {
        String name = CTypeConversion.toJavaString(backendName);
        String address = BACKENDS.getOrDefault(name, BACKENDS.getOrDefault("default", ""));
        byte[] bytes = (address + "\0").getBytes(StandardCharsets.UTF_8);
        return CTypeConversion.toCString(new String(bytes, StandardCharsets.UTF_8)).get();
    }
}
