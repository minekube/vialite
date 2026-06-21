package com.minekube.vialite.bridge;

import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicBoolean;
import org.graalvm.nativeimage.IsolateThread;
import org.graalvm.nativeimage.c.function.CEntryPoint;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

public final class VialiteBridge {
    private static final AtomicBoolean INITIALIZED = new AtomicBoolean(false);
    private static final AtomicBoolean RUNNING = new AtomicBoolean(false);
    private static final Map<String, String> BACKENDS = new ConcurrentHashMap<>();

    private VialiteBridge() {
    }

    public static void main(String[] args) throws Exception {
        System.err.println("vialite native bridge is intended to be loaded through libvialite");
        Thread.sleep(Long.MAX_VALUE);
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
