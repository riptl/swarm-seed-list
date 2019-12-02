# Builder stage
FROM golang:alpine as builder
RUN apk --no-cache add git
WORKDIR /app

# Get root dependencies
ADD go.* ./
RUN go mod download

# Build app
ADD . /app
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w" \
    -o /go/bin/swarm-seed-list \
    .

# Light stage
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/bin/swarm-seed-list /bin/swarm-seed-list
ENTRYPOINT ["/bin/swarm-seed-list"]
EXPOSE 8080
CMD ["-l", ":8080"]
