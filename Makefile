all: jiravars

jiravars: $(shell find . -name '*.go')
	go build -o jiravars

clean:
	rm -f jiravars jiravars-linux

jiravars-linux: $(shell find . -name '*.go')
	GOOS=linux GOARCH=amd64 go build -o jiravars-linux

linux: jiravars-linux

.PHONY: clean
.PHONY: all
.PHONY: linux
