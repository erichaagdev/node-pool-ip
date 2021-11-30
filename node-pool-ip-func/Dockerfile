FROM golang:1.17.3-alpine3.14 AS build

WORKDIR /go/src/project/
COPY go.mod go.sum /go/src/project/
RUN go mod download

COPY ./node-pool-ip-func/main.go /go/src/project
RUN go build -ldflags "-w -s" -o /bin/project

FROM alpine:3.14.3
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /bin/project /bin/project
ENTRYPOINT ["/bin/project"]
