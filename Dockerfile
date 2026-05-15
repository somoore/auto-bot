FROM golang:1.26@sha256:313faae491b410a35402c05d35e7518ae99103d957308e940e1ae2cfa0aac29b AS build
RUN apt-get update && apt-get install -y libopus-dev pkg-config && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY web/ web/
RUN go build -o /app ./cmd/server/

FROM debian:bookworm-slim@sha256:67b30a61dc87758f0caf819646104f29ecbda97d920aaf5edc834128ac8493d3
RUN apt-get update && apt-get install -y libopus0 ca-certificates && rm -rf /var/lib/apt/lists/*
RUN groupadd -r -g 10001 appuser && useradd -r -u 10001 -g appuser -d /srv appuser
WORKDIR /srv
COPY --from=build /app /srv/app
COPY --from=build /src/web /srv/web
RUN mkdir -p /srv/data
RUN chown -R appuser:appuser /srv
USER appuser
EXPOSE 3000
CMD ["/srv/app"]
