FROM golang:1.26 AS build
RUN apt-get update && apt-get install -y libopus-dev pkg-config && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY web/ web/
RUN go build -o /app ./cmd/server/

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y libopus0 ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /srv
COPY --from=build /app /srv/app
COPY --from=build /src/web /srv/web
EXPOSE 3000
CMD ["/srv/app"]
