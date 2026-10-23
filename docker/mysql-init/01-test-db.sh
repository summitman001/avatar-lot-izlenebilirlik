#!/bin/sh
# Ilk kurulumda ayri bir test veritabani olusturur ve uygulama kullanicisina yetki verir.
# Go testleri (TEST_DATABASE_URL) bu veritabanini kullanir; gelistirme verisine dokunmaz.
set -e
TEST_DB="${MYSQL_TEST_DATABASE:-avatar_test}"
mysql -uroot -p"${MYSQL_ROOT_PASSWORD}" <<SQL
CREATE DATABASE IF NOT EXISTS \`${TEST_DB}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
GRANT ALL PRIVILEGES ON \`${TEST_DB}\`.* TO '${MYSQL_USER}'@'%';
FLUSH PRIVILEGES;
SQL
