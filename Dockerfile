FROM golang:1.22-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/vbakup-controller ./cmd/controller

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/vbakup-controller /usr/local/bin/vbakup-controller
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/vbakup-controller"]
