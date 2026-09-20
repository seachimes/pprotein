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

pprotein: view/dist
	go build -trimpath -ldflags="-w -s" ./cli/pprotein

pprotein-agent:
	go build -trimpath -ldflags="-w -s" ./cli/pprotein-agent

view/dist:
	pnpm --dir view install --frozen-lockfile
	pnpm --dir view run build

.PHONY: clean
clean:
	rm -rf pprotein pprotein-agent view/dist
