
FROM golang:1.12-alpine as builder

WORKDIR /go/src/github.com/zerok/jiravars
COPY . .

RUN apk add git && \
    export GO111MODULE=on && \ 
    go mod vendor && \
    CGO_ENABLED=0 go build -ldflags '-s -w' -o /jiravars


FROM alpine:3.10

EXPOSE 9300
ENTRYPOINT ["/jiravars"]

RUN apk add ca-certificates && addgroup -S app && adduser -S app -G app

USER app

COPY --from=builder /jiravars /