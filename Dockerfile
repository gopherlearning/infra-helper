FROM golang:1.26-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/infra-helper ./


FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 app

COPY --from=build /out/infra-helper /usr/local/bin/infra-helper

USER app

ENTRYPOINT ["/usr/local/bin/infra-helper"]
