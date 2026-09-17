FROM golang:1.25.6 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/presence .
RUN CGO_ENABLED=0 go build -o /out/seed ./cmd/seed
RUN CGO_ENABLED=0 go build -o /out/web ./cmd/web

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/ ./
COPY cmd/web/www ./cmd/web/www
USER 65534:65534
CMD ["./presence"]
