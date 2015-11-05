FROM ubuntu:15.10
ADD kube-proxy /usr/bin/
ADD run-kubelet.sh /usr/bin/
ADD kubelet /root/
RUN apt-get update
RUN apt-get install -y iptables util-linux
