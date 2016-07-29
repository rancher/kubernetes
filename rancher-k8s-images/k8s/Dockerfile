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

RUN mkdir -p /opt/cni/bin /etc/cni/net.d
# TODO: Darren needs to fix this line to pick up the binaries from rancher/cni instead
RUN curl -sfSL https://github.com/containernetworking/cni/releases/download/v0.3.0/cni-v0.3.0.tgz | \
    tar xvzf - -C /tmp ./bridge && \
    mv /tmp/bridge /opt/cni/bin/rancher-bridge

RUN curl -sfSL https://github.com/rancher/rancher-cni-ipam/releases/download/v0.0.1/rancher-cni-ipam.tar.gz | \
    tar xvzf - -C /opt/cni/bin

COPY 10-rancher.conf /etc/cni/net.d/10-rancher.conf

ENTRYPOINT ["/usr/bin/entry.sh"]
