package com.minekube.vialite.bridge;

import org.graalvm.nativeimage.hosted.Feature;
import org.graalvm.nativeimage.hosted.RuntimeClassInitialization;

public final class VialiteBridgeFeature implements Feature {
    @Override
    public void beforeAnalysis(BeforeAnalysisAccess access) {
        RuntimeClassInitialization.initializeAtRunTime(VialiteBridge.class);
    }
}
