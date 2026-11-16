FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/mobilevc-relay \
    ./cmd/relay

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=build /out/mobilevc-relay /mobilevc-relay

EXPOSE 9000
USER nonroot:nonroot

ENTRYPOINT ["/mobilevc-relay"]
