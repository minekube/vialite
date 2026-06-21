ARG GRAALVM_IMAGE=ghcr.io/graalvm/native-image-community:21-ol9
FROM ${GRAALVM_IMAGE} AS build

WORKDIR /src
COPY . .

RUN microdnf install -y git findutils tar gzip unzip which && microdnf clean all
RUN bash build/apply-overlay.sh

WORKDIR /src/build/.work/ViaProxy
RUN ./gradlew :vialite-native:nativeCompile --no-daemon

FROM build AS shared
WORKDIR /out
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/libvialite.so /out/libvialite.so || true
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/libvialite.h /out/libvialite.h || true
