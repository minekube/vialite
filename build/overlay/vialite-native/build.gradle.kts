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

val sharedImage = providers.gradleProperty("vialite.native.shared")
    .map(String::toBoolean)
    .getOrElse(true)

dependencies {
    compileOnly("org.graalvm.sdk:nativeimage:25.0.3")
    compileOnly(project(":")) {
        isTransitive = false
    }
    if (!sharedImage) {
        implementation(project(":")) {
            isTransitive = false
        }
        implementation("org.bouncycastle:bctls-jdk18on:1.84")
    }
    testImplementation(platform("org.junit:junit-bom:5.11.4"))
    testImplementation("org.junit.jupiter:junit-jupiter")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
    testRuntimeOnly(project(":")) {
        isTransitive = false
    }
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
            imageName.set(if (sharedImage) "libvialite" else "vialite")
            sharedLibrary.set(sharedImage)
            mainClass.set(if (sharedImage) {
                "com.minekube.vialite.bridge.VialiteBridge"
            } else {
                "com.minekube.vialite.bridge.VialiteLauncher"
            })
            val args = mutableListOf(
                "--no-fallback",
                "--enable-url-protocols=http,https",
                "-H:Features=com.minekube.vialite.bridge.VialiteBridgeFeature",
                "-H:IncludeResources=^(assets/.+|mappings/.+|META-INF/services/.+|.+\\.json|.+\\.properties)$",
                "-H:+UnlockExperimentalVMOptions",
                "-O2"
            )
            if (sharedImage) {
                args.add("-H:Name=libvialite")
            } else {
                args.add("-H:Name=vialite")
                args.add("--initialize-at-run-time=org.apache.logging.log4j,org.slf4j,io.netty,org.bouncycastle")
            }
            buildArgs.addAll(args)
        }
    }
}

tasks.test {
    useJUnitPlatform()
}
