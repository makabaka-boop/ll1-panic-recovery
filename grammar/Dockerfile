# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/grammar .

FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /out/grammar /usr/local/bin/grammar
USER app
EXPOSE 8080
ENV PORT=8080
HEALTHCHECK --interval=10s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["grammar"]
