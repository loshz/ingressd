FROM golang:1.16-alpine3.13
WORKDIR $GOPATH/src/github.com/syscll/ingressd
COPY . .
RUN CGO_ENABLED=0 go install ./cmd/ingressd

FROM alpine:3.13
COPY --from=0 /go/bin/ingressd /bin/ingressd
EXPOSE 8081
USER 2000:2000
CMD ["/bin/ingressd"]
