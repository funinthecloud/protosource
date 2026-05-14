# Build context is the repo root; the module path imports protosource
# packages from across the tree (cosmosdbstore, opaquedata/cosmos, the
# example handlers, etc.), so a per-cmd context would not be enough.
#
# Build with:
#   docker build --platform linux/amd64 \
#     -f cmd/testcosmos/Dockerfile \
#     -t <acr>.azurecr.io/testcosmos:latest .
#
# Why --platform is required from arm64 dev hosts:
#   Container Apps runs linux/amd64. Without --platform, the runtime stage
#   pulls the host-arch base image and Docker stamps the final manifest as
#   arm64 even though our Go binary is cross-compiled to amd64 — ACA then
#   rejects the image with "image OS/Arch must be linux/amd64".
#
# The Dockerfile uses $BUILDPLATFORM on the build stage so the Go compiler
# runs natively (no QEMU), and $TARGETPLATFORM on the runtime stage so
# the resulting image's manifest matches the target.

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

# These ARGs are populated automatically by buildkit from --platform.
# Default to linux/amd64 so a plain `docker build` (no --platform) still
# produces a Container Apps-compatible image.
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" \
    -o /out/testcosmos ./cmd/testcosmos

FROM --platform=$TARGETPLATFORM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/testcosmos /testcosmos

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/testcosmos"]
