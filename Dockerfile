# --------------------------------------------------

FROM golang:alpine AS pprotein

# Vite+ requires Node >= 24.11, which Alpine's `apk add nodejs` does not
# reliably provide, so the binary is copied from the official image instead.
COPY --from=node:24-alpine /usr/local/bin/node /usr/local/bin/node
COPY --from=node:24-alpine /usr/local/lib/node_modules /usr/local/lib/node_modules

# libstdc++ and libgcc are the shared libraries the node binary links against;
# golang:alpine does not ship them.
#
# pnpm is installed at the exact version pinned in mise.toml and
# package.json#packageManager. `corepack enable` is deliberately avoided: it
# resolves to the latest pnpm at image build time, which then refuses to run
# because it disagrees with the pinned version.
RUN apk add --no-cache make libstdc++ libgcc \
 && ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && npm install -g pnpm@11.8.0 \
 && pnpm --version

WORKDIR $GOPATH/src/app
COPY . .

# `COPY . .` does not carry .git, so the Go toolchain cannot embed the revision
# on its own. Pass it in to have the running build identify itself:
#   docker build --build-arg VERSION=$(git describe --tags --always --dirty) .
ARG VERSION=""
RUN make build VERSION="$VERSION"

# --------------------------------------------------

FROM golang:alpine AS alp

RUN go install github.com/tkuchiki/alp/cmd/alp@latest

# --------------------------------------------------

FROM golang:alpine AS slp

RUN apk add gcc musl-dev
RUN go install github.com/tkuchiki/slp/cmd/slp@latest

# --------------------------------------------------

FROM alpine

RUN apk add --no-cache graphviz

COPY --from=pprotein /go/src/app/pprotein /usr/local/bin/
COPY --from=pprotein /go/src/app/pprotein-agent /usr/local/bin/
COPY --from=alp /go/bin/alp /usr/local/bin/
COPY --from=slp /go/bin/slp /usr/local/bin/

RUN mkdir -p /opt/pprotein
WORKDIR /opt/pprotein

ENTRYPOINT ["pprotein"]
