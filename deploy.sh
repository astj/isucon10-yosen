git pull origin main
cd /home/isuumo/webapp/go
make all
systemctl reload isuumo.go.service
systemctl reload nginx
