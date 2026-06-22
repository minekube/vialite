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
    }
}
