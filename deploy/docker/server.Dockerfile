# Central service image (docs/04-kubernetes.md §4.2): owns the Recipe DB,
# Git fetch, and Starlark resolution, plus the embedded UI (webui/). The UI
# must be built on the host first (`task webui:build` — internal/centralserver
# ui.go's go:embed needs it present before `go build` even compiles) since
# this Dockerfile does not run npm itself.
#
# The runtime stage includes `git`: gitsource's local-filesystem transport
# shells out to it (docs/05-recipe-and-crd.md §5.2) even though the
# HTTP(S)/SSH transports used for real Git remotes are pure Go.
#
# The recipe-db path (--recipe-db) is expected to be a mounted volume, not
# baked into the image — this Dockerfile carries no Recipe data.
# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache git \
    && addgroup -g 65532 configblender \
    && adduser -D -u 65532 -G configblender configblender \
    && mkdir -p /data && chown 65532:65532 /data
COPY --from=build /out/server /server
USER 65532:65532
VOLUME ["/data"]
ENTRYPOINT ["/server"]
