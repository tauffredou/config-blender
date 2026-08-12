# Per-cluster controller image (docs/04-kubernetes.md §4.2). Never touches
# Git or Starlark directly, so the runtime stage needs nothing beyond the
# binary itself.
# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/manager ./cmd/manager

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/manager /manager
ENTRYPOINT ["/manager"]
