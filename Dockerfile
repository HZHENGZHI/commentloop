# syntax=docker/dockerfile:1
FROM alpine:3.20

ARG TARGETARCH
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S mdlive \
    && adduser -S -D -H -u 10001 -G mdlive -h /data mdlive \
    && mkdir -p /notes /data \
    && chown -R 10001:10001 /notes /data

COPY bin/mdlive-linux-${TARGETARCH} /usr/local/bin/mdlive

ENV HOME=/data
WORKDIR /data
VOLUME ["/notes", "/data"]
EXPOSE 4571
STOPSIGNAL SIGTERM
USER 10001:10001

ENTRYPOINT ["/usr/local/bin/mdlive", "run", "--no-open", "--addr", "0.0.0.0:4571"]
CMD ["/notes"]
