# frc-mcp hosted profile (ADR-0005): a static binary on distroless, running
# as an unprivileged user. The signed index is synced into /data at startup
# and every 6 hours; nothing else is written.
#
#   docker build --build-arg VERSION=$(git describe --tags --always) -t frc-mcp .
#
# Base images are pinned by digest; bump them in a PR (Dependabot/Renovate).
FROM golang:1.27.1@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY data ./data
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/frc-mcp ./cmd/frc-mcp && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/frc-mcp /frc-mcp
# A named volume mounted here starts as a copy of this directory, so it must
# already belong to the runtime user or the index sync cannot write.
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
VOLUME ["/data"]
EXPOSE 7424
ENTRYPOINT ["/frc-mcp"]
CMD ["serve", "--transport", "http", "--public", "--addr", "0.0.0.0:7424", "--index", "/data/shards"]
