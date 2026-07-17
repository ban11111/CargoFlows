FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/cargoflow-api ./cmd/server
RUN go build -o /out/cargoflow-worker ./cmd/worker
RUN go build -o /out/cargoflow-migrate ./cmd/migrate
RUN go build -o /out/cargoflow-openai-smoke ./cmd/openai-smoke
RUN go build -o /out/cargoflow-fake-openai ./cmd/fake-openai

FROM alpine:3.22

RUN adduser -D -H cargoflow
USER cargoflow
WORKDIR /app
COPY --from=build /out/cargoflow-api /app/cargoflow-api
COPY --from=build /out/cargoflow-worker /app/cargoflow-worker
COPY --from=build /out/cargoflow-migrate /app/cargoflow-migrate
COPY --from=build /out/cargoflow-openai-smoke /app/cargoflow-openai-smoke
COPY --from=build /out/cargoflow-fake-openai /app/cargoflow-fake-openai
EXPOSE 8080
ENTRYPOINT ["/app/cargoflow-api"]
