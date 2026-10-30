FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/osyn ./cmd/${SERVICE}

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S osyn \
    && adduser -S -G osyn osyn
WORKDIR /app
COPY --from=build /out/osyn /usr/local/bin/osyn
COPY --chown=osyn:osyn db /app/db
USER osyn
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/osyn"]
