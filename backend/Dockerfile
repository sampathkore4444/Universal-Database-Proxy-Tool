FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o udbproxy ./cmd/udbproxy

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/udbproxy .
COPY --from=builder /app/config.yaml .

EXPOSE 3306 5432 6379 27017 8080

ENV UDBP_CONFIG_PATH=/app/config.yaml

ENTRYPOINT ["./udbproxy"]
CMD ["serve"]
