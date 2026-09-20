.PHONY: noop
noop:

.PHONY: run
run:
	go run ./cli/pprotein

.PHONY: run-agent
run-agent:
	go run ./cli/pprotein-agent

.PHONY: build
build: pprotein pprotein-agent

# Normally the Go toolchain embeds the VCS revision automatically. Inside a
# Docker build it cannot: `COPY . .` does not bring .git along. VERSION can be
# passed in to supply it explicitly, and is empty otherwise so local builds keep
# using the embedded metadata.
VERSION ?=
VERSION_PKG := github.com/kaz/pprotein/internal/version
LDFLAGS := -w -s $(if $(VERSION),-X $(VERSION_PKG).Version=$(VERSION))

pprotein: view/dist
	go build -trimpath -ldflags="$(LDFLAGS)" ./cli/pprotein

pprotein-agent:
	go build -trimpath -ldflags="$(LDFLAGS)" ./cli/pprotein-agent

view/dist:
	pnpm --dir view install --frozen-lockfile
	pnpm --dir view run build

.PHONY: clean
clean:
	rm -rf pprotein pprotein-agent view/dist
