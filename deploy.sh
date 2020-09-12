set -x

cd /home/isucon/isuumo/webapp/go
sudo -u isucon git pull origin main
systemctl stop isuumo.go.service
sudo -u isucon make all
systemctl restart isuumo.go.service
systemctl reload nginx
