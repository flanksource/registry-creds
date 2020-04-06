FROM golang:1.12.17-alpine as builder
RUN mkdir /build
RUN apk add --update git
ADD . /build/
ENV GOFLAGS=-mod=vendor GO111MODULE=on
WORKDIR /build
RUN go build -o registry-creds .

FROM alpine:3.4

RUN apk add --update ca-certificates && \
  rm -rf /var/cache/apk/*

COPY --from=builder /build/registry-creds registry-creds

ENTRYPOINT ["/registry-creds"]
