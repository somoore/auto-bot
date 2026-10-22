FROM golang:1.26@sha256:672c944cfd07599b5f1bb179869270cdc94c6254e41db4d431259693cb286b82 AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
    libopus0=1.5.2-2 \
    libopus-dev=1.5.2-2 \
    pkg-config=1.8.1-4 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/ web/
RUN go build -o /app ./cmd/server/

FROM debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8
RUN apt-get update && apt-get install -y --no-install-recommends \
    libopus0=1.3.1-3 \
    libssl3=3.0.20-1~deb12u1 \
    openssl=3.0.20-1~deb12u1 \
    ca-certificates=20230311+deb12u1 \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -r -g 10001 appuser && useradd -r -u 10001 -g appuser -d /srv appuser
WORKDIR /srv
COPY --from=build /app /srv/app
COPY --from=build /src/web /srv/web
RUN mkdir -p /srv/data
RUN chown -R appuser:appuser /srv
USER appuser
EXPOSE 3000
CMD ["/srv/app"]
