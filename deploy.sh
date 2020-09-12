set -x

cd /home/isucon/isuumo/webapp/go
git pull origin main
systemctl stop isuumo.go.service
make all
systemctl restart isuumo.go.service
systemctl reload nginx
