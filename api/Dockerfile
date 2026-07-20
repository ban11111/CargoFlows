FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/cargoflows-api ./cmd/server
RUN go build -o /out/cargoflows-worker ./cmd/worker
RUN go build -o /out/cargoflows-migrate ./cmd/migrate
RUN go build -o /out/cargoflows-openai-smoke ./cmd/openai-smoke
RUN go build -o /out/cargoflows-fake-openai ./cmd/fake-openai

FROM alpine:3.22

RUN adduser -D -H cargoflows
USER cargoflows
WORKDIR /app
COPY --from=build /out/cargoflows-api /app/cargoflows-api
COPY --from=build /out/cargoflows-worker /app/cargoflows-worker
COPY --from=build /out/cargoflows-migrate /app/cargoflows-migrate
COPY --from=build /out/cargoflows-openai-smoke /app/cargoflows-openai-smoke
COPY --from=build /out/cargoflows-fake-openai /app/cargoflows-fake-openai
EXPOSE 8080
ENTRYPOINT ["/app/cargoflows-api"]
