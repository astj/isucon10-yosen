.PHONY: deploy
deploy:
	node deploy-all.js

.PHONY: bench
bench:
	ssh isucon-server1 "sudo cp /var/log/nginx/access.log /home/isucon/isuumo/log/access.log.`date '+%Y%m%d%H%M%S'` ; sudo echo '' > /var/log/nginx/access.log"
	ssh isucon-server2 "sudo cp /var/log/mysql/mysql-slow.sql /home/isucon/isuumo/log/mysql-slow.sql.`date '+%Y%m%d%H%M%S'`; sudo echo '' > /var/log/mysql/mysql-slow.sql"
