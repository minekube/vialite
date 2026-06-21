ARG GRAALVM_IMAGE=ghcr.io/graalvm/native-image-community:21-ol9
FROM ${GRAALVM_IMAGE} AS build

ENV JAVA_HOME=/usr/lib64/graalvm/graalvm-community-java21
ENV PATH="${JAVA_HOME}/bin:${PATH}"

WORKDIR /src
COPY . .

RUN microdnf install -y git findutils tar gzip unzip which && microdnf clean all
RUN java -version && native-image --version
RUN bash build/apply-overlay.sh

WORKDIR /src/build/.work/ViaProxy
RUN ./gradlew :vialite-native:nativeCompile --no-daemon --no-configuration-cache

FROM build AS shared
WORKDIR /out
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/libvialite.so /out/libvialite-${TARGETOS}-${TARGETARCH}.so || true
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/libvialite.h /out/libvialite.h || true
