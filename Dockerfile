# syntax=docker/dockerfile:1.7

# Multi-stage build: compile a static binary in the builder stage,
# copy it into a distroless base for the final image. The final image
# has no shell, no package manager, and no other tooling — bomtique is
# the only thing that runs.

FROM golang:1.26-alpine AS builder

WORKDIR /src

# Prime the module cache first so source changes don't blow the layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/bomtique \
    ./cmd/bomtique

# Distroless static: glibc-less, shell-less, runs as UID 65532 (nonroot).
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/bomtique /usr/local/bin/bomtique

USER nonroot:nonroot
WORKDIR /work

ENTRYPOINT ["/usr/local/bin/bomtique"]
CMD ["--help"]
