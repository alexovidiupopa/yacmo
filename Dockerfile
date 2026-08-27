# Multi-stage build for YACMO — produces a small Linux image with the tools the
# network module needs (tc via iproute2, iptables). The version is stamped in at
# build time via -ldflags, matching what CI does for the released binaries.

# ── Build stage ────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS build

# Build metadata (passed by CI via --build-arg; sane fallbacks for local builds).
ARG VERSION=0.0.0-docker
ARG COMMIT=unknown
ARG DATE=unknown

WORKDIR /src

# Cache module downloads separately from the source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X yacmo/pkg/version.Version=${VERSION} \
      -X yacmo/pkg/version.Commit=${COMMIT} \
      -X yacmo/pkg/version.Date=${DATE}" \
    -o /out/yacmo .

# ── Runtime stage ──────────────────────────────────────────────────────────
FROM alpine:3.20

# iproute2 (tc) + iptables back the network chaos module; ca-certificates for
# outbound HTTPS (health checks, webhooks). Runs as root because the network
# module needs it — grant NET_ADMIN when using that module.
RUN apk add --no-cache ca-certificates iproute2 iptables

COPY --from=build /out/yacmo /usr/local/bin/yacmo

WORKDIR /work

ENTRYPOINT ["yacmo"]
# Default to the config.json in the working directory; override with `-config`.
CMD ["-config", "config.json"]
