#!/bin/bash

if [ "$#" -ne 2 ]; then
    echo "Must provide exactly two arguments: hostname and port of the api server."
    exit 1
fi

getent hosts $1
output=$(getent hosts $1)
status=$?

if [ "$status" -ne 0 ]; then
	echo "Unable to determine IP of api-server $1"
	exit $status
fi

ip=$(echo $output | awk '{print $1}')

cp /root/kubelet /var/lib/rancher/kubernetes
PATH=$PATH:/var/lib/rancher/kubernetes nsenter -m -t 1 -- /var/lib/rancher/kubernetes/kubelet --api_servers=http://$ip:$2 --allow-privileged=true --containerized=true --register-node=true --cloud-provider=rancher --healthz-bind-address=0.0.0.0
