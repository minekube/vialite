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
    compileOnly(rootProject)
    runtimeOnly(project(":")) {
        isTransitive = false
    }
}

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(21))
    }
}

val sharedImage = providers.gradleProperty("vialite.native.shared")
    .map(String::toBoolean)
    .getOrElse(true)

// A native image records the page size of the machine that built it, and it may
// only run where the run-time page size is <= that value
// (SubstrateOptions.getPageSize(): "images can be executed as long as run-time
// page size <= build-time page size"; -H:PageSize "Define PageSize of a machine
// that runs the image. The default = 0 (== same as host machine page size)").
//
// CI builds these images on 4 KiB-page runners, so an unset page size shipped
// 4096-byte-aligned arm64 artifacts that cannot start on a 16 KiB kernel at all
// - Raspberry Pi 5 firmware's default kernel_2712.img, and every Asahi Linux
// host - failing before main with
//
//     Fatal error: Failed to create the main Isolate. (code 24)
//
// 64 KiB alignment runs on 4K, 16K and 64K kernels, and is the route apple/pkl
// took for the same two platforms (267de3c, "Fix page size for Linux AArch64
// native executables"); coursier#2636 is the same failure. Keep the flag
// explicit even for a GraalVM whose aarch64 default is already 64 KiB: it is
// then deterministic across pins instead of depending on the toolchain default.
//
// Override with -Pvialite.native.pagesize=<bytes>; empty disables the flag.
val isLinuxAarch64: Boolean = System.getProperty("os.name", "").lowercase().contains("linux") &&
    System.getProperty("os.arch", "").lowercase() in setOf("aarch64", "arm64")
val nativePageSize: String = providers.gradleProperty("vialite.native.pagesize")
    .getOrElse(if (isLinuxAarch64) "65536" else "")

graalvmNative {
    toolchainDetection.set(false)
    metadataRepository {
        enabled.set(false)
    }
    binaries {
        named("main") {
            imageName.set(if (sharedImage) "libvialite" else "vialite")
            sharedLibrary.set(sharedImage)
            mainClass.set("com.minekube.vialite.bridge.VialiteBridge")
            val args = mutableListOf(
                "--no-fallback",
                "--enable-url-protocols=http,https",
                "-H:Features=com.minekube.vialite.bridge.VialiteBridgeFeature",
                "-H:IncludeResources=^(assets/.+|mappings/.+|META-INF/services/.+|.+\\.json|.+\\.properties)$",
                "--initialize-at-run-time=net.raphimc.viaproxy,io.netty,org.apache.logging,org.slf4j",
                "-H:+UnlockExperimentalVMOptions",
                "-O2"
            )
            if (sharedImage) {
                args.add("-H:Name=libvialite")
            } else {
                args.add("-H:Name=vialite")
            }
            if (nativePageSize.isNotEmpty()) {
                val bytes = nativePageSize.toIntOrNull()
                    ?: throw GradleException("vialite.native.pagesize must be a byte count, got \"$nativePageSize\"")
                if (bytes < 4096 || (bytes and (bytes - 1)) != 0) {
                    throw GradleException("vialite.native.pagesize must be a power of two >= 4096, got $bytes")
                }
                args.add("-H:PageSize=$bytes")
            }
            buildArgs.addAll(args)
        }
    }
}
