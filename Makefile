BINARY_NAME=codex-companion
PLATFORMS=windows/amd64 linux/amd64 linux/arm64 freebsd/amd64 darwin/amd64 darwin/arm64

GOOS := $(shell go env GOOS)
EXT :=
ifeq ($(GOOS),windows)
	EXT := .exe
endif
BINARY=$(BINARY_NAME)$(EXT)

.PHONY: build cross clean

build:
	go build -o $(BINARY) ./cmd/companion

cross:
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform##*/}; \
		output=$(BINARY_NAME)-$$os-$$arch; \
		if [ $$os = windows ]; then output=$$output.exe; fi; \
		echo "Building $$output"; \
		GOOS=$$os GOARCH=$$arch go build -o $$output ./cmd/companion || exit $$?; \
	done

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe $(BINARY_NAME)-* 2>/dev/null || true
