# AI Chain Node Docker Image
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /aichain ./cmd/aichain-node/

FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl

COPY --from=builder /aichain /usr/local/bin/aichain

EXPOSE 30303 8545

ENTRYPOINT ["/usr/local/bin/aichain"]
