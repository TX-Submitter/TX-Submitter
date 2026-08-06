# syntax=docker/dockerfile:1

FROM golang:1.23-bookworm AS builder

WORKDIR /app

# Cache module deps
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 go build -o /stellar-tx-submitter ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /stellar-tx-submitter .

USER nonroot:nonroot

EXPOSE 8080 9002

CMD ["./stellar-tx-submitter"]
