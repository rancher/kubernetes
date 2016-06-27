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
    nfs-common \
    jq \
    unzip \
    && DEBIAN_FRONTEND=noninteractive apt-get autoremove -y \
    && DEBIAN_FRONTEND=noninteractive apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

RUN curl -sfSL -o /usr/bin/share-mnt https://github.com/rancher/runc/releases/download/share-mnt-v0.1.1/share-mnt && \
    chmod u+x /usr/bin/share-mnt

RUN cp /usr/bin/nsenter /nsenter
COPY entry.sh kubelet-start.sh kubelet kube-proxy kube-apiserver kube-controller-manager kube-scheduler /usr/bin/

ENTRYPOINT ["/usr/bin/entry.sh"]
