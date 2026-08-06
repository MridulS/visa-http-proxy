# Drop-in replacement image for the Node visa-jupyter-proxy.
# Build from this repo's root. To build a linux/amd64 image from an
# Apple-Silicon (arm64) host, with NO emulation:
#   podman build --platform linux/amd64 -t visa-http-proxy:latest .
#   docker buildx build --platform linux/amd64 -t visa-http-proxy:latest --load .

# --- build: runs on the NATIVE host arch ($BUILDPLATFORM) and cross-compiles a
#     static binary for the target arch — no QEMU emulation of the Go compile.
#     `apk add` here also runs natively (build arch), so still no emulation. ---
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
# Build with the image's own toolchain: if it ever drifts below the floor in
# go.mod, fail loudly here instead of silently downloading one mid-build.
ENV GOTOOLCHAIN=local
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /visa-proxy .

# --- runtime: static scratch image. No RUN in the target-arch stage, so the
#     cross-arch build needs no QEMU emulation at all. ---
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /visa-proxy /app/visa-proxy
COPY proxy.conf.json /app/proxy.conf.json
WORKDIR /app
# Same default port and env-var contract as the Node image.
EXPOSE 8088
ENTRYPOINT ["/app/visa-proxy"]
