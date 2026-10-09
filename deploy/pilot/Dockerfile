FROM golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/apheliondmm-hosted ./cmd/apheliondmm-hosted
COPY cmd/apheliondmm-healthcheck ./cmd/apheliondmm-healthcheck
COPY cmd/apheliondmm-oidc-fixture ./cmd/apheliondmm-oidc-fixture
COPY internal ./internal
ARG APHELIONDMM_BUILD=desktop-pilot
ARG APHELIONDMM_REVISION=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.build=${APHELIONDMM_BUILD} -X main.revision=${APHELIONDMM_REVISION}" -o /out/apheliondmm-hosted ./cmd/apheliondmm-hosted
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/apheliondmm-healthcheck ./cmd/apheliondmm-healthcheck
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/apheliondmm-oidc-fixture ./cmd/apheliondmm-oidc-fixture
RUN mkdir /out/tls && chown 65532:65532 /out/tls

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7 AS hosted
COPY --from=build --chown=nonroot:nonroot /out/apheliondmm-hosted /apheliondmm-hosted
COPY --from=build --chown=nonroot:nonroot /out/apheliondmm-healthcheck /apheliondmm-healthcheck
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 CMD ["/apheliondmm-healthcheck"]
ENTRYPOINT ["/apheliondmm-hosted"]
CMD ["-config", "/etc/apheliondmm/config.yaml"]

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7 AS oidc-fixture
COPY --from=build --chown=nonroot:nonroot /out/apheliondmm-oidc-fixture /apheliondmm-oidc-fixture
COPY --from=build --chown=nonroot:nonroot /out/tls /tls
USER nonroot:nonroot
EXPOSE 9443
ENTRYPOINT ["/apheliondmm-oidc-fixture"]
