
install_redis:
	sudo bash install_redis.sh
install_nginx:
	sudo bash install_nginx.sh

install_nats:
	sudo bash install_gnats.sh

install_nats_streamer:
	sudo bash install_nats_streamer.sh


NATS_PORT=4222
run_nats:
	nats-server --addr 127.0.0.1 --port ${NATS_PORT}

NATS_STREAMER_PORT=4222
run_nats_streamer:
	nats-streaming-server -mc 0 --port ${NATS_STREAMER_PORT}

REDIS_PORT=6380
run_redis:
	redis-server --port ${REDIS_PORT}
