FROM golang:stretch

RUN apt-get update && \
    apt-get install -y curl && \
    curl https://public.dhe.ibm.com/ibmdl/export/pub/software/websphere/messaging/mqdev/redist/9.1.0.3-IBM-MQC-Redist-LinuxX64.tar.gz -o mq.tar.gz          && \
    mkdir -p /opt/mqm             && \
    tar -C /opt/mqm -xzf mq.tar.gz

ENV MQ_INSTALLATION_PATH="/opt/mqm"
ENV CGO_LDFLAGS_ALLOW="-Wl,-rpath.*"
ENV CGO_LDFLAGS="-L$MQ_INSTALLATION_PATH/lib64 -Wl,-rpath,$MQ_INSTALLATION_PATH/lib64"
ENV CGO_CFLAGS="-I$MQ_INSTALLATION_PATH/inc"

RUN mkdir -p $GOPATH/src/github.com/triggermesh/ibm-mq-source
WORKDIR $GOPATH/src/github.com/triggermesh/ibm-mq-source
COPY . .
RUN go get
RUN go build -o mq


FROM debian:stretch-slim
WORKDIR /opt/mqm/
COPY --from=0 /opt/mqm .
COPY --from=0 /go/src/github.com/triggermesh/ibm-mq-source/mq .

ENTRYPOINT ["./mq"]
