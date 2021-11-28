FROM golang:1.17.3-alpine3.14 AS build
COPY go.mod go.sum main.go /go/src/project/
WORKDIR /go/src/project/
RUN go build -ldflags "-w -s" -o /bin/project

FROM alpine:3.14.3
RUN apk add --update-cache --no-cache ca-certificates
COPY --from=build /bin/project /bin/project
ENTRYPOINT ["/bin/project"]
