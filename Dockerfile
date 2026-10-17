FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# `docker build --target migrate`: applies db/migrations with goose (same version as the Makefile)
FROM golang:1.27-alpine AS migrate
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
COPY db/migrations /migrations
ENTRYPOINT ["goose", "-dir", "/migrations", "postgres"]

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
ENV APP_ENV=production PORT=8080
EXPOSE 8080
ENTRYPOINT ["/api"]
