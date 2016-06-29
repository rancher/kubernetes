#!/bin/bash

mount --rbind /host/dev /dev

HOST_ID=$(curl -s http://169.254.169.250/latest/self/host/hostId)
HOSTNAME_OVERRIDE=$(curl -s -u $CATTLE_ACCESS_KEY:$CATTLE_SECRET_KEY "$CATTLE_URL/hosts/1h$HOST_ID/ipaddresses" | jq -r '.data[0].address')

CMD=$(eval echo "$@")

ARR_CMD=($CMD)

exec "${ARR_CMD[@]}"
