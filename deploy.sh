git pull origin main
cd /home/isuumo/webapp/go
systemctl stop isuumo.go.service
make all
systemctl restart isuumo.go.service
systemctl reload nginx
