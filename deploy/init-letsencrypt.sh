#!/bin/bash
# 首次部署时运行此脚本申请 Let's Encrypt 证书
# 用法: sudo bash init-letsencrypt.sh

set -e

DOMAIN="context-canvas.com"
EMAIL="luhaozeng@gmail.com"   # Let's Encrypt 到期提醒邮箱
COMPOSE_FILE="docker-compose.yaml"

echo ">>> 1. 创建临时自签名证书（让 nginx 先启动）"
mkdir -p ./certbot/conf/live/$DOMAIN
docker compose run --rm --entrypoint "\
  openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
    -keyout /etc/letsencrypt/live/$DOMAIN/privkey.pem \
    -out /etc/letsencrypt/live/$DOMAIN/fullchain.pem \
    -subj '/CN=localhost'" certbot

echo ">>> 2. 启动 nginx（使用临时证书）"
docker compose up -d front-end

echo ">>> 3. 删除临时证书"
docker compose run --rm --entrypoint "\
  rm -rf /etc/letsencrypt/live/$DOMAIN && \
  rm -rf /etc/letsencrypt/archive/$DOMAIN && \
  rm -rf /etc/letsencrypt/renewal/$DOMAIN.conf" certbot

echo ">>> 4. 申请真正的 Let's Encrypt 证书"
docker compose run --rm --entrypoint "\
  certbot certonly --webroot -w /var/www/certbot \
    --email $EMAIL \
    -d $DOMAIN \
    --rsa-key-size 4096 \
    --agree-tos \
    --no-eff-email \
    --force-renewal" certbot

echo ">>> 5. 重载 nginx 使用真证书"
docker compose exec front-end nginx -s reload

echo ">>> 完成！HTTPS 已启用: https://$DOMAIN"
