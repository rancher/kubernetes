FROM debian:jessie

RUN DEBIAN_FRONTEND=noninteractive apt-get update -y \
    && DEBIAN_FRONTEND=noninteractive apt-get -yy -q \
    install \
    iptables \
    ca-certificates \
    file \
    util-linux \
    socat \
    curl \
    ethtool \
    && DEBIAN_FRONTEND=noninteractive apt-get autoremove -y \
    && DEBIAN_FRONTEND=noninteractive apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

RUN cp /usr/bin/nsenter /nsenter
ADD kubelet /usr/bin/

ADD kube-proxy /kube-proxy
ADD kubelet /kubelet

