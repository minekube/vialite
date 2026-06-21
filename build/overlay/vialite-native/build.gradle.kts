plugins {
    java
    id("org.graalvm.buildtools.native") version "0.10.6"
}

repositories {
    mavenCentral()
}

dependencies {
    implementation(rootProject)
}

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(21))
    }
}

graalvmNative {
    binaries {
        named("main") {
            imageName.set("vialite")
            sharedLibrary.set(true)
            mainClass.set("com.minekube.vialite.bridge.VialiteBridge")
            buildArgs.addAll(
                "--no-fallback",
                "--enable-url-protocols=http,https",
                "-H:Name=libvialite",
                "-H:Features=com.minekube.vialite.bridge.VialiteBridgeFeature",
                "-H:IncludeResources=^(assets/.+|mappings/.+|META-INF/services/.+|.+\\.json|.+\\.properties)$",
                "-H:+UnlockExperimentalVMOptions",
                "-O2"
            )
        }
    }
}
