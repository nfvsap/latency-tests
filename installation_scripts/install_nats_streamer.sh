#!/bin/bash

echo "Installing nats streamer service....\n"

sudo apt install unzip

curl -L https://github.com/nats-io/nats-streaming-server/releases/download/v0.14.2/nats-streaming-server-v0.14.2-linux-amd64.zip -o nats-streaming-server.zip
unzip nats-streaming-server.zip -d tmp
cp tmp/nats-streaming-server-v0.14.2-linux-amd64/nats-streaming-server /usr/local/bin

