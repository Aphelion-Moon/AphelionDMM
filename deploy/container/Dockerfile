FROM golang:1.26.10-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/apheliondmm-hosted ./cmd/apheliondmm-hosted
COPY cmd/apheliondmm-healthcheck ./cmd/apheliondmm-healthcheck
COPY internal ./internal
ARG APHELIONDMM_BUILD=container
ARG APHELIONDMM_REVISION=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.build=${APHELIONDMM_BUILD} -X main.revision=${APHELIONDMM_REVISION}" -o /out/apheliondmm-hosted ./cmd/apheliondmm-hosted
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/apheliondmm-healthcheck ./cmd/apheliondmm-healthcheck

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

COPY --from=build --chown=nonroot:nonroot /out/apheliondmm-hosted /apheliondmm-hosted
COPY --from=build --chown=nonroot:nonroot /out/apheliondmm-healthcheck /apheliondmm-healthcheck
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/apheliondmm-healthcheck"]
ENTRYPOINT ["/apheliondmm-hosted"]
CMD ["-config", "/etc/apheliondmm/config.yaml"]
