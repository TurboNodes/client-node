# syntax=docker/dockerfile:1

# Builds the headless node (cmd/turbod) only — no cgo, no GTK/WebKit, no tray
# UI. The desktop app (root main.go) is not part of this image.

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

# go.mod replaces fyne.io/systray with a local path; module resolution reads
# that module's go.mod/go.sum even though turbod never links it, so it has to
# be present before `go mod download`.
COPY go.mod go.sum ./
COPY third_party/fyne-systray/go.mod third_party/fyne-systray/go.sum third_party/fyne-systray/
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Only the packages turbod actually needs, so editing the desktop UI or its
# assets never invalidates this layer.
COPY cmd/turbod/ cmd/turbod/
COPY quic/ quic/
COPY platform/ platform/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/turbod ./cmd/turbod

# distroless has no shell, so the data dir has to exist before COPY --chown
# can place it in the final stage.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=build /out/turbod /turbod

# os.UserConfigDir() errors on Linux with neither XDG_CONFIG_HOME nor HOME
# set, which would silently drop the host cache and paired state on every
# restart. TURBO_LOG_FILE=0 keeps logs on stderr instead of duplicating them
# into a file under /data that docker logs can't see.
ENV XDG_CONFIG_HOME=/data
ENV TURBO_LOG_FILE=0

VOLUME /data

ENTRYPOINT ["/turbod"]
