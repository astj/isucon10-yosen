set -x

cd /home/isucon/isuumo/webapp/go
sudo -u isucon git pull origin main
systemctl stop isuumo.go.service
make all
systemctl restart isuumo.go.service
systemctl reload nginx
