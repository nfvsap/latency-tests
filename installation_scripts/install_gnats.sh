#!/bin/bash

echo "Installing gnats service....\n"

sudo apt install unzip
curl -L https://github.com/nats-io/nats-server/releases/download/v2.0.0/nats-server-v2.0.0-linux-amd64.zip -o nats-server.zip
unzip nats-server.zip -d nats-server
cp nats-server/nats-server-v2.0.0-linux-amd64/nats-server /usr/local/bin


