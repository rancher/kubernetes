FROM ubuntu:15.10
RUN apt-get update
RUN apt-get install -y iptables util-linux
ADD kubelet /usr/bin/
ADD kube-proxy /usr/bin/
