package com.minekube.vialite.bridge;

import org.graalvm.nativeimage.hosted.Feature;
import org.graalvm.nativeimage.hosted.RuntimeReflection;
import org.graalvm.nativeimage.hosted.RuntimeClassInitialization;

public final class VialiteBridgeFeature implements Feature {
    @Override
    public void beforeAnalysis(BeforeAnalysisAccess access) {
        RuntimeClassInitialization.initializeAtRunTime(VialiteBridge.class);
        Class<?> viaProxyClass = access.findClassByName("net.raphimc.viaproxy.ViaProxy");
        if (viaProxyClass != null) {
            try {
                RuntimeReflection.register(viaProxyClass.getDeclaredField("CWD"));
                RuntimeReflection.register(viaProxyClass.getDeclaredField("PLUGIN_MANAGER"));
                RuntimeReflection.register(viaProxyClass.getDeclaredField("SAVE_MANAGER"));
                RuntimeReflection.register(viaProxyClass.getDeclaredField("CONFIG"));
                RuntimeReflection.register(viaProxyClass.getDeclaredMethod("loadNetty"));
            } catch (ReflectiveOperationException e) {
                throw new IllegalStateException("Failed to register ViaProxy reflection metadata", e);
            }
        }
        registerAll(access.findClassByName("com.minekube.vialite.bridge.VialiteBridge$NativeConfig"));
        registerAll(access.findClassByName("com.minekube.vialite.bridge.VialiteBridge$NativeBackend"));
        registerAll(access.findClassByName("com.minekube.vialite.bridge.VialiteBridge$RouteEventHandler"));
        registerAll(access.findClassByName("com.minekube.vialite.bridge.VialiteBridge$BackendRoute"));
    }

    private static void registerAll(Class<?> type) {
        if (type == null) {
            return;
        }
        RuntimeReflection.register(type);
        RuntimeReflection.register(type.getDeclaredConstructors());
        RuntimeReflection.register(type.getDeclaredFields());
        RuntimeReflection.register(type.getDeclaredMethods());
    }
}
