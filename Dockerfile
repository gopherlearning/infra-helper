FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/infra-helper ./


FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 app

COPY --from=build /out/infra-helper /usr/local/bin/infra-helper

# Allow binding to privileged ports (e.g. 53) without running as root.
RUN apk add --no-cache libcap && setcap 'cap_net_bind_service=+ep' /usr/local/bin/infra-helper

USER app

ENTRYPOINT ["/usr/local/bin/infra-helper"]
