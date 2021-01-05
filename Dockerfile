FROM golang:1.15-buster
WORKDIR $GOPATH/src/github.com/syscll/ingressd
COPY . .
RUN CGO_ENABLED=0 go install

FROM alpine:3.12
COPY --from=0 /go/bin/ingressd /bin/ingressd
EXPOSE 8081
USER 2000:2000
CMD ["/bin/ingressd"]
