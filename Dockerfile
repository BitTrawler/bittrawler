# syntax=docker/dockerfile:1

# BitTrawler image: a static binary on distroless. The build stage always runs on
# the build platform and cross-compiles for the target platform, so a buildx
# build for linux/arm64 needs no emulation.

FROM --platform=$BUILDPLATFORM golang:1-bookworm AS build

# Set by buildx for every target platform.
ARG TARGETOS
ARG TARGETARCH

# Build metadata, injected into internal/version. The defaults keep a plain
# `docker build .` working; CI and goreleaser pass the real values.
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

WORKDIR /src

# Modules first, so this layer survives changes to the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is off so the result is a static binary that runs on distroless without a
# libc. -buildvcs=false avoids needing the .git directory, which .dockerignore
# excludes. The data directory is created in the same layer: distroless has no
# shell to create it at runtime, and the application stores secrets there, hence
# mode 0700.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
        -trimpath \
        -buildvcs=false \
        -ldflags "-s -w \
            -X github.com/BitTrawler/bittrawler/internal/version.Version=${VERSION} \
            -X github.com/BitTrawler/bittrawler/internal/version.Commit=${COMMIT} \
            -X github.com/BitTrawler/bittrawler/internal/version.Date=${DATE}" \
        -o /out/bittrawler . \
    && install -d -m 0700 -o 65532 -g 65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/bittrawler /bittrawler
COPY --from=build --chown=65532:65532 --chmod=0700 /out/data /data

# The health check runs `bittrawler healthcheck`, which opens the settings
# database read only. Pin the data directory to the volume so the check and the
# server always agree on where it is, whatever the working directory is.
ENV BITTRAWLER_DATA_DIRECTORY=/data

USER 65532:65532

# 6684/tcp is the web interface and Torznab; 6684/udp is the DHT socket.
EXPOSE 6684/tcp
EXPOSE 6684/udp

VOLUME /data

# The process stops on SIGTERM and drains the crawler for up to thirty seconds.
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/bittrawler", "healthcheck"]

ENTRYPOINT ["/bittrawler"]
CMD ["run"]
