FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/cargoflow-api ./cmd/server
RUN go build -o /out/cargoflow-worker ./cmd/worker
RUN go build -o /out/cargoflow-migrate ./cmd/migrate

FROM alpine:3.22

RUN adduser -D -H cargoflow
USER cargoflow
WORKDIR /app
COPY --from=build /out/cargoflow-api /app/cargoflow-api
COPY --from=build /out/cargoflow-worker /app/cargoflow-worker
COPY --from=build /out/cargoflow-migrate /app/cargoflow-migrate
EXPOSE 8080
ENTRYPOINT ["/app/cargoflow-api"]
