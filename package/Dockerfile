FROM ubuntu:16.04
RUN apt-get update && \
    apt-get install -y dnsmasq dnsmasq-utils
COPY dhcp tapd /usr/bin/
