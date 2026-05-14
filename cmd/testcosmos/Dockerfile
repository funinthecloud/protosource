# Build context is the repo root; the module path imports protosource
# packages from across the tree (cosmosdbstore, opaquedata/cosmos, the
# example handlers, etc.), so a per-cmd context would not be enough.
#
# Build with:
#   docker build -f cmd/testcosmos/Dockerfile -t <acr>.azurecr.io/testcosmos:latest .
#
# Cross-architecture note: Container Apps in most regions runs amd64. We
# hard-pin GOARCH so a build from an M-series Mac produces a runnable image
# without relying on buildx's emulation.

FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache dependencies separately from sources so an edit-only rebuild skips
# the network round-trip.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /out/testcosmos ./cmd/testcosmos

# Distroless static: ~2 MB base, no shell, no package manager — minimal
# attack surface and small enough that ACR pulls are quick.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/testcosmos /testcosmos

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/testcosmos"]
