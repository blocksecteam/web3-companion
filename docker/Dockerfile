# syntax=docker/dockerfile:1

FROM oven/bun:1.3.14 AS web-builder
WORKDIR /src

COPY frontend/package.json frontend/bun.lock ./frontend/
RUN cd frontend && bun install --frozen-lockfile

COPY frontend ./frontend
RUN cd frontend && bun run check

COPY companion/package.json companion/bun.lock ./companion/
RUN cd companion && bun install --frozen-lockfile

COPY companion ./companion
RUN cd companion && bun run check && bun run build

FROM golang:1.26-bookworm AS go-builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
COPY --from=web-builder /src/frontend/dist/client ./internal/frontend/assets
COPY --from=web-builder /src/companion/dist/companion-server.js ./internal/companion/assets/companion-server.js
RUN ./scripts/check-embedded-assets.sh /src

ARG VERSION=dev
RUN CGO_ENABLED=1 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /out/web3-server \
    ./cmd/server/
RUN CGO_ENABLED=1 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /out/web3 \
    ./cmd/cli/

FROM oven/bun:1.3.14 AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates gosu \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system web3 \
    && useradd --system --gid web3 --create-home --home-dir /home/web3 --shell /usr/sbin/nologin web3

WORKDIR /opt/web3

COPY --from=go-builder /out/web3-server /usr/local/bin/web3-server
COPY --from=go-builder /out/web3 /usr/local/bin/web3
COPY --from=go-builder /src/docker/config.docker.toml ./config.toml
COPY --from=go-builder /src/companion ./companion
COPY --from=go-builder /src/skills ./skills
COPY docker/entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN cd companion \
    && bun install --frozen-lockfile --production \
    && cp node_modules/@anthropic-ai/claude-agent-sdk-linux-x64/claude /usr/local/bin/claude \
    && chmod +x /usr/local/bin/claude \
    && mkdir -p /home/web3 \
    && chown -R web3:web3 /home/web3 /opt/web3 \
    && chmod 0755 /usr/local/bin/docker-entrypoint.sh

ENV HOME=/home/web3
ENV WEB3_WORKDIR=/home/web3
ENV WEB3_CONFIG_PATH=/opt/web3/config.toml
ENV WEB3_REPO_SKILLS_DIR=/opt/web3/skills

VOLUME ["/home/web3"]
EXPOSE 8081

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["web3-server"]
