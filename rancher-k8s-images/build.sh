#!/bin/bash
set -e

cd $(dirname $0)

REPO=${REPO:-"rancher"}
TAG=${TAG:="dev"}
echo "Beginning kubernetes build using repo [$REPO] and tag [$TAG]" 

if [ ! -e ../_output/release-tars/kubernetes-server-linux-amd64.tar.gz ]; then
    echo Cleaning up build artifacts from last build
    rm -rf ../_output build
    mkdir build && cd build
    cp -r ../k8s .

    echo Building kubernetes
    # Skip tests faster for development:
    KUBE_RELEASE_RUN_TESTS=n KUBE_FASTBUILD=true ../../build/release.sh
fi

echo Unpacking kubernetes binaries
tar -xvzf ../../_output/release-tars/kubernetes-server-linux-amd64.tar.gz 
echo "Building k8s image $REPO/k8s:$TAG"
for i in kubelet kube-proxy kube-apiserver kube-controller-manager kube-scheduler; do
    cp kubernetes/server/bin/$i k8s
done

cd k8s
docker build -t $REPO/k8s:$TAG .
echo Done building $REPO/k8s:$TAG image
