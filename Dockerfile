FROM golang:1.23.3-alpine as builder

WORKDIR /go/src/github.com/zerok/jiravars
COPY . .

RUN apk add git && \
    CGO_ENABLED=0 go build -ldflags '-s -w' -o /jiravars


FROM alpine:3.21

EXPOSE 9300
ENTRYPOINT ["/jiravars"]

RUN apk --no-cache upgrade && \
    apk add ca-certificates && \
    addgroup -S app && \
    adduser -S app -G app

USER app

COPY --from=builder /jiravars /
