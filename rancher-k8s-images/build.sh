#!/bin/bash

set -e

REPO=${REPO:-"rancher"}
TAG=${TAG:="dev"}
echo "Beginning kubernetes build using repo [$REPO] and tag [$TAG]" 

echo Cleaning up build artifacts from last build
rm -rf ../_output build
mkdir build && cd build
cp -r ../k8s .

echo Building kubernetes
#../../build/release.sh
# Skip tests faster for development: 
KUBE_RELEASE_RUN_TESTS=n ../../build/release.sh

echo Unpacking kubernetes binaries
tar -xvzf ../../_output/release-tars/kubernetes-server-linux-amd64.tar.gz 
for i in kubernetes/server/bin/*.tar; do
    echo "Loading image $i"
    docker load -i $i
done
echo Done loading images

echo Tagging images
docker images | grep -m 3 kube | while read -r line ; do
    parts=($line)
    IMAGE_NAME=$(echo "${parts[0]}" | awk -F '/' '{print $3}')
    echo "Taggging  ${parts[0]}:${parts[1]} as $REPO/$IMAGE_NAME:$TAG"
    docker tag -f ${parts[0]}:${parts[1]} $REPO/$IMAGE_NAME:$TAG
done

echo "Building k8s image $REPO/k8s:$TAG"
cp kubernetes/server/bin/kubelet k8s
cp kubernetes/server/bin/kube-proxy k8s
cd k8s
docker build -t $REPO/k8s:$TAG .
echo Done building k8s image

docker images | grep "$REPO" | grep "$TAG"
