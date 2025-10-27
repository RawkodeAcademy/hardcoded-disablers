FROM golang:1.22-alpine AS builder
WORKDIR /src
ENV GOTOOLCHAIN=auto

# Install modules separately to maximize Docker layer caching.
COPY go.mod ./
RUN go mod download

# Build the static binary for Linux.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/transliterator .

FROM scratch
COPY --from=builder /out/transliterator /transliterator
EXPOSE 8080
ENTRYPOINT ["/transliterator"]
