#!/bin/bash

echo "Installing NGINX service....\n"
sudo apt update
sudo apt install nginx -y
sudo ufw app list
sudo ufw allow 'Nginx HTTP'
sudo ufw status
systemctl status nginx


echo "Configuring NGINX...\n"

sudo mkdir -p /var/www/html
sudo chown -R $USER:$USER /var/www/html

sudo chmod -R 755 /var/www/html


echo <<EOL >> /var/www/html/index.html
<html>
    <head>
        <title>Welcome to Example.com!</title>
    </head>
    <body>
        <h1>Success!  The example.com server block is working!</h1>
    </body>
</html>
EOL

echo <<EOL >> /etc/nginx/nginx.conf
user                 nginx;
worker_processes     auto;
worker_rlimit_nofile 10240;
pid                  /var/run/nginx.pid;

events {
    worker_connections 10240;
    accept_mutex       off;
    multi_accept       off;
}

http {
    access_log   off;
    include      /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for" "$ssl_cipher" '
                    '"$ssl_protocol" ';

    sendfile on;

    keepalive_timeout  300s;     
    keepalive_requests 1000000;

    server {
        listen 80;
        root /var/www/html;
    }
}
EOL


sudo nginx -t

sudo systemctl restart nginx

curl http://0.0.0.0


(cd /var/www/html/ && sudo dd if=/dev/zero of=1kb.bin bs=1KB count=1)
curl http://0.0.0.0/1kb.bin
