package com.minekube.vialite.bridge;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;
import com.viaversion.viaversion.api.protocol.version.ProtocolVersion;
import io.netty.channel.Channel;
import io.netty.channel.ChannelHandler;
import java.io.File;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.nio.file.Files;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Supplier;
import net.lenni0451.lambdaevents.EventHandler;
import net.raphimc.netminecraft.netty.connection.NetServer;
import net.raphimc.viaproxy.ViaProxy;
import net.raphimc.viaproxy.plugins.PluginManager;
import net.raphimc.viaproxy.plugins.events.Client2ProxyHandlerCreationEvent;
import net.raphimc.viaproxy.plugins.events.PreConnectEvent;
import net.raphimc.viaproxy.protocoltranslator.ProtocolTranslator;
import net.raphimc.viaproxy.protocoltranslator.viaproxy.ViaProxyConfig;
import net.raphimc.viaproxy.proxy.client2proxy.Client2ProxyChannelInitializer;
import net.raphimc.viaproxy.proxy.client2proxy.Client2ProxyHandler;
import net.raphimc.viaproxy.saves.SaveManager;
import net.raphimc.viaproxy.util.AddressUtil;
import net.raphimc.viaproxy.util.ProtocolVersionUtil;
import net.raphimc.viaproxy.util.logging.Logger;
import org.graalvm.nativeimage.IsolateThread;
import org.graalvm.nativeimage.c.function.CEntryPoint;
import org.graalvm.nativeimage.c.type.CCharPointer;
import org.graalvm.nativeimage.c.type.CTypeConversion;

public final class VialiteBridge {
    private static final Gson GSON = new Gson();
    private static final AtomicBoolean INITIALIZED = new AtomicBoolean(false);
    private static final AtomicBoolean RUNNING = new AtomicBoolean(false);
    private static final AtomicBoolean VIAPROXY_INITIALIZED = new AtomicBoolean(false);
    private static final Map<String, String> BACKEND_ADDRESSES = new ConcurrentHashMap<>();
    private static final Map<Integer, BackendRoute> ROUTES_BY_LOCAL_PORT = new ConcurrentHashMap<>();
    private static final List<NetServer> SERVERS = new ArrayList<>();
    private static final RouteEventHandler ROUTE_EVENT_HANDLER = new RouteEventHandler();

    private VialiteBridge() {
    }

    public static void main(String[] args) throws Exception {
        NativeConfig config = NativeConfig.fromArgs(args);
        if (config == null) {
            System.err.println("Usage: vialite --config <config.json>");
            System.exit(2);
        }
        int code = initConfig(GSON.toJson(config));
        if (code != 0) {
            System.exit(code);
        }
        System.exit(runLoop());
    }

    @CEntryPoint(name = "vialite_init")
    public static synchronized int init(IsolateThread thread, CCharPointer configJson) {
        return initConfig(CTypeConversion.toJavaString(configJson));
    }

    private static synchronized int initConfig(String config) {
        try {
            NativeConfig nativeConfig = GSON.fromJson(config, NativeConfig.class);
            if (nativeConfig == null || nativeConfig.backends == null || nativeConfig.backends.isEmpty()) {
                return 3;
            }

            shutdownServers();
            initializeViaProxy(nativeConfig);

            ViaProxy.EVENT_MANAGER.unregister(ROUTE_EVENT_HANDLER);
            ROUTES_BY_LOCAL_PORT.clear();
            BACKEND_ADDRESSES.clear();

            for (NativeBackend backend : nativeConfig.backends) {
                BackendRoute route = BackendRoute.from(backend);
                NetServer server = new NetServer(new Client2ProxyChannelInitializer(clientHandlerSupplier()));
                server.bind(AddressUtil.parse(bindAddress(nativeConfig, backend), null), false);
                SocketAddress localAddress = server.getChannel().localAddress();
                if (!(localAddress instanceof InetSocketAddress inetSocketAddress)) {
                    server.getChannel().close().syncUninterruptibly();
                    return 4;
                }
                String address = dialAddress(inetSocketAddress);
                SERVERS.add(server);
                putBackendAddress(backend.name, address);
                ROUTES_BY_LOCAL_PORT.put(inetSocketAddress.getPort(), route);
            }

            ViaProxy.EVENT_MANAGER.register(ROUTE_EVENT_HANDLER);
            INITIALIZED.set(true);
            return 0;
        } catch (Throwable t) {
            t.printStackTrace(System.err);
            shutdownServers();
            INITIALIZED.set(false);
            return 1;
        }
    }

    @CEntryPoint(name = "vialite_run")
    public static int run(IsolateThread thread) {
        return runLoop();
    }

    private static int runLoop() {
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
        shutdownServers();
        return 0;
    }

    @CEntryPoint(name = "vialite_shutdown")
    public static int shutdown(IsolateThread thread) {
        RUNNING.set(false);
        shutdownServers();
        return 0;
    }

    @CEntryPoint(name = "vialite_status")
    public static int status(IsolateThread thread) {
        return INITIALIZED.get() && RUNNING.get() ? 1 : 0;
    }

    @CEntryPoint(name = "vialite_backend_address")
    public static CCharPointer backendAddress(IsolateThread thread, CCharPointer backendName) {
        String name = CTypeConversion.toJavaString(backendName);
        String address = BACKEND_ADDRESSES.getOrDefault(name, BACKEND_ADDRESSES.getOrDefault(normalizeBackendName(name), ""));
        return CTypeConversion.toCString(address).get();
    }

    private static Supplier<ChannelHandler> clientHandlerSupplier() {
        return () -> ViaProxy.EVENT_MANAGER.call(new Client2ProxyHandlerCreationEvent(new Client2ProxyHandler(), false)).getHandler();
    }

    private static synchronized void initializeViaProxy(NativeConfig nativeConfig) throws Exception {
        if (VIAPROXY_INITIALIZED.get()) {
            return;
        }
        File cwd = Files.createTempDirectory("vialite-viaproxy-").toFile();
        setStatic(ViaProxy.class, "CWD", cwd);
        Logger.setup();
        Method loadNetty = ViaProxy.class.getDeclaredMethod("loadNetty");
        loadNetty.setAccessible(true);
        loadNetty.invoke(null);
        setStatic(ViaProxy.class, "PLUGIN_MANAGER", new PluginManager());
        setStatic(ViaProxy.class, "SAVE_MANAGER", new SaveManager());
        ViaProxyConfig config = ViaProxyConfig.create(new File(cwd, "viaproxy.yml"));
        configureViaProxy(config, nativeConfig);
        setStatic(ViaProxy.class, "CONFIG", config);
        ProtocolTranslator.init();
        VIAPROXY_INITIALIZED.set(true);
    }

    private static void configureViaProxy(ViaProxyConfig config, NativeConfig nativeConfig) {
        ForwardingMode forwardingMode = forwardingMode(nativeConfig);
        config.setProxyOnlineMode(false);
        config.setAuthMethod(ViaProxyConfig.AuthMethod.NONE);
        config.setPassthroughBungeecordPlayerInfo(forwardingMode == ForwardingMode.LEGACY);
        config.setRewriteHandshakePacket(true);
        config.setRewriteTransferPackets(true);
        config.setCompressionThreshold(256);
        config.setIgnoreProtocolTranslationErrors(false);
        config.setSuppressClientProtocolErrors(false);
        config.setAllowLegacyClientPassthrough(false);
    }

    private static void setStatic(Class<?> type, String fieldName, Object value) throws Exception {
        Field field = type.getDeclaredField(fieldName);
        field.setAccessible(true);
        field.set(null, value);
    }

    private static ForwardingMode forwardingMode(NativeConfig nativeConfig) {
        ForwardingMode mode = null;
        for (NativeBackend backend : nativeConfig.backends) {
            ForwardingMode backendMode = ForwardingMode.from(backend.forwarding);
            if (backendMode == ForwardingMode.VELOCITY) {
                throw new UnsupportedOperationException("Velocity forwarding is not supported by vialite native runtime yet");
            }
            if (mode == null) {
                mode = backendMode;
            } else if (mode != backendMode) {
                throw new IllegalArgumentException("Mixed backend forwarding modes are not supported by vialite native runtime");
            }
        }
        return mode == null ? ForwardingMode.NONE : mode;
    }

    private static String bindAddress(NativeConfig nativeConfig, NativeBackend backend) {
        String bind = backend.bind;
        if (bind == null || bind.isBlank()) {
            bind = nativeConfig.bind;
        }
        if (bind == null || bind.isBlank()) {
            return "127.0.0.1:0";
        }
        return bind.trim();
    }

    private static void putBackendAddress(String name, String address) {
        if (name == null || name.isBlank()) {
            throw new IllegalArgumentException("Backend name is required");
        }
        BACKEND_ADDRESSES.put(name, address);
        BACKEND_ADDRESSES.put(normalizeBackendName(name), address);
    }

    private static String normalizeBackendName(String name) {
        return name == null ? "" : name.trim().toLowerCase(java.util.Locale.ROOT);
    }

    private static String dialAddress(InetSocketAddress address) {
        String host = address.getHostString();
        if (address.getAddress() != null && address.getAddress().isAnyLocalAddress()) {
            host = "127.0.0.1";
        }
        if (host.contains(":") && !host.startsWith("[")) {
            host = "[" + host + "]";
        }
        return host + ":" + address.getPort();
    }

    private static synchronized void shutdownServers() {
        for (NetServer server : SERVERS) {
            try {
                Channel channel = server.getChannel();
                if (channel != null) {
                    channel.close().syncUninterruptibly();
                }
            } catch (Throwable ignored) {
            }
        }
        SERVERS.clear();
        ROUTES_BY_LOCAL_PORT.clear();
        BACKEND_ADDRESSES.clear();
        INITIALIZED.set(false);
    }

    public static final class RouteEventHandler {
        @EventHandler(events = PreConnectEvent.class)
        public void onPreConnect(PreConnectEvent event) {
            SocketAddress local = event.getClientChannel().localAddress();
            if (!(local instanceof InetSocketAddress inetSocketAddress)) {
                return;
            }
            BackendRoute route = ROUTES_BY_LOCAL_PORT.get(inetSocketAddress.getPort());
            if (route == null) {
                return;
            }
            event.setServerAddress(route.targetAddress);
            event.setServerVersion(route.targetVersion);
        }
    }

    private static final class BackendRoute {
        private final SocketAddress targetAddress;
        private final ProtocolVersion targetVersion;

        private BackendRoute(SocketAddress targetAddress, ProtocolVersion targetVersion) {
            this.targetAddress = targetAddress;
            this.targetVersion = targetVersion;
        }

        private static BackendRoute from(NativeBackend backend) {
            ProtocolVersion version = protocolVersion(backend.version, backend.detect);
            return new BackendRoute(AddressUtil.parse(backend.address, version), version);
        }

        private static ProtocolVersion protocolVersion(String configured, boolean detect) {
            if (detect || configured == null || configured.isBlank() || configured.equalsIgnoreCase("auto")) {
                return ProtocolTranslator.AUTO_DETECT_PROTOCOL;
            }
            ProtocolVersion version = ProtocolVersionUtil.fromNameLenient(configured);
            if (version == null) {
                throw new IllegalArgumentException("Unknown backend protocol version: " + configured);
            }
            return version;
        }
    }

    private static final class NativeConfig {
        private String bind;
        @SerializedName("gate_protocol")
        private String gateProtocol;
        private List<NativeBackend> backends;

        private static NativeConfig fromArgs(String[] args) throws Exception {
            if (args == null || args.length != 2 || !"--config".equals(args[0])) {
                return null;
            }
            return GSON.fromJson(Files.readString(new File(args[1]).toPath()), NativeConfig.class);
        }
    }

    private static final class NativeBackend {
        private String name;
        private String address;
        private String bind;
        private String version;
        private boolean detect;
        private String forwarding;
    }

    private enum ForwardingMode {
        NONE,
        LEGACY,
        VELOCITY;

        private static ForwardingMode from(String value) {
            if (value == null || value.isBlank() || value.equalsIgnoreCase("none")) {
                return NONE;
            }
            if (value.equalsIgnoreCase("legacy")) {
                return LEGACY;
            }
            if (value.equalsIgnoreCase("velocity")) {
                return VELOCITY;
            }
            throw new IllegalArgumentException("Unknown backend forwarding mode: " + value);
        }
    }
}
