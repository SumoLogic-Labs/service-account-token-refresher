FROM --platform=$BUILDPLATFORM golang:1.22.0 as builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /token-refresher

COPY . .

RUN go vet ./... && \
    go test -v -race ./... && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o token-refresher

FROM alpine

WORKDIR /token-refresher

RUN addgroup token-refresher \
    && adduser -u 1000 -S -g 1000 token-refresher --ingroup token-refresher \
    && chown -R token-refresher:token-refresher /token-refresher

USER token-refresher

COPY --from=builder /token-refresher/token-refresher /usr/local/bin/token-refresher

ENTRYPOINT ["token-refresher"]
