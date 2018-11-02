FROM golang:1.11 as builder

RUN set -ex; \
    wget -qO dep https://github.com/golang/dep/releases/download/v0.5.0/dep-linux-amd64; \
    echo '287b08291e14f1fae8ba44374b26a2b12eb941af3497ed0ca649253e21ba2f83 dep' | sha256sum -c -; \
    mv dep /usr/local/bin/dep; \
    chmod +x /usr/local/bin/dep


WORKDIR src/github.com/zerok/jiravars
COPY . .

RUN set -ex; \
    dep ensure -vendor-only; \
    CGO_ENABLED=0 go build -ldflags '-s -w' -o /jiravars


FROM gcr.io/distroless/base

EXPOSE 9300
ENTRYPOINT ["/jiravars"]

COPY --from=builder /jiravars /
