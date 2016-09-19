#!/bin/bash
set -e -x

while ! curl -s -f http://rancher-metadata/2015-12-19/stacks/Kubernetes/services/kubernetes/uuid; do
    echo Waiting for metadata
    sleep 1
done

UUID=$(curl -s http://rancher-metadata/2015-12-19/stacks/Kubernetes/services/kubernetes/uuid)
ACTION=$(curl -s -u $CATTLE_ACCESS_KEY:$CATTLE_SECRET_KEY "$CATTLE_URL/services?uuid=$UUID" | jq -r '.data[0].actions.certificate')

if [ -n "$ACTION" ]; then
    mkdir -p /etc/kubernetes/ssl
    cd /etc/kubernetes/ssl
    curl -s -u $CATTLE_ACCESS_KEY:$CATTLE_SECRET_KEY -X POST $ACTION > certs.zip
    unzip -o certs.zip
    cd $OLDPWD

    cat > /etc/kubernetes/ssl/kubeconfig << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    api-version: v1
    certificate-authority: /etc/kubernetes/ssl/ca.pem
    server: "https://kubernetes:6443"
  name: "Default"
contexts:
- context:
    cluster: "Default"
    user: "Default"
  name: "Default"
current-context: "Default"
users:
- name: "Default"
  user:
    client-certificate: /etc/kubernetes/ssl/cert.pem
    client-key: /etc/kubernetes/ssl/key.pem
EOF
fi

if [ "$1" == "kubelet" ]; then
    /usr/bin/share-mnt /var/lib/kubelet /sys -- kubelet-start.sh "$@"
elif [ "$1" == "kube-apiserver" ]; then
    CONTAINERIP=$(curl -s http://rancher-metadata/2015-12-19/self/container/ips/0)
    set -- "$@" "--advertise-address=$CONTAINERIP"
    exec "$@"
else
    exec "$@"
fi
