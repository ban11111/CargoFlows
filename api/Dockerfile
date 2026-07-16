FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/cargoflow-api ./cmd/server

FROM alpine:3.22

RUN adduser -D -H cargoflow
USER cargoflow
WORKDIR /app
COPY --from=build /out/cargoflow-api /app/cargoflow-api
EXPOSE 8080
ENTRYPOINT ["/app/cargoflow-api"]

