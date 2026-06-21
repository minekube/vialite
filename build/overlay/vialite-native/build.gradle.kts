plugins {
    java
    id("org.graalvm.buildtools.native") version "1.1.2"
}

repositories {
    maven {
        name = "ViaVersion"
        url = uri("https://repo.viaversion.com")
        content {
            includeGroupByRegex("com\\.viaversion(\\..+)?")
            includeGroupByRegex("net\\.raphimc(\\..+)?")
        }
    }
    maven {
        name = "Lenni0451"
        url = uri("https://maven.lenni0451.net/everything")
        content {
            includeGroupByRegex("net\\.lenni0451(\\..+)?")
            includeGroupByRegex("net\\.raphimc(\\..+)?")
        }
    }
    maven {
        name = "Minecraft Libraries"
        url = uri("https://libraries.minecraft.net")
        content {
            includeGroup("com.mojang")
        }
    }
    maven {
        name = "Jitpack"
        url = uri("https://jitpack.io")
        content {
            includeGroupByRegex("com\\.github\\..+")
        }
    }
    mavenCentral()
}

dependencies {
    compileOnly("org.graalvm.sdk:nativeimage:25.0.3")
    implementation(rootProject)
}

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(21))
    }
}

graalvmNative {
    toolchainDetection.set(false)
    metadataRepository {
        enabled.set(false)
    }
    binaries {
        named("main") {
            imageName.set("vialite")
            sharedLibrary.set(true)
            mainClass.set("com.minekube.vialite.bridge.VialiteBridge")
            buildArgs.addAll(
                "--no-fallback",
                "--enable-url-protocols=http,https",
                "--initialize-at-build-time=org.apache.logging.log4j,org.slf4j",
                "-H:Name=libvialite",
                "-H:Features=com.minekube.vialite.bridge.VialiteBridgeFeature",
                "-H:IncludeResources=^(assets/.+|mappings/.+|META-INF/services/.+|.+\\.json|.+\\.properties)$",
                "-H:+UnlockExperimentalVMOptions",
                "-O2"
            )
        }
    }
}
