# syntax=docker/dockerfile:1

##
## Build
##
FROM golang:1.18-buster AS build

WORKDIR /app

COPY . .

RUN go mod download
RUN go mod vendor

RUN go build -o /target-exporter

##
## Deploy
##
FROM gcr.io/distroless/base-debian10

WORKDIR /

COPY --from=build /target-exporter /target-exporter

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/target-exporter"]