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
RUN mkdir -p /src/build/native-out/shared /src/build/native-out/executable \
    && ./gradlew :vialite-native:nativeCompile --no-daemon --no-configuration-cache -Pvialite.native.shared=true \
    && cp vialite-native/build/native/nativeCompile/libvialite.so /src/build/native-out/shared/libvialite.so \
    && cp vialite-native/build/native/nativeCompile/libvialite.h /src/build/native-out/shared/libvialite.h \
    && ./gradlew :vialite-native:nativeCompile --no-daemon --no-configuration-cache -Pvialite.native.shared=false \
    && cp vialite-native/build/native/nativeCompile/vialite /src/build/native-out/executable/vialite

FROM build AS shared
WORKDIR /out
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN cp /src/build/native-out/shared/libvialite.so /out/libvialite-${TARGETOS}-${TARGETARCH}.so
RUN cp /src/build/native-out/shared/libvialite.h /out/libvialite.h
RUN cp /src/build/native-out/executable/vialite /out/vialite-${TARGETOS}-${TARGETARCH}
