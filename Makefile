# Gelistirme kisayollari. Windows'ta: `winget install ezwinports.make` (veya Git Bash / WSL).
# Tum hedefler docker compose, go ve npm'in PATH'te olmasini ister.

# `make test-backend` Docker'daki ayri test veritabanina (avatar_test) baglanir; port .env'deki MYSQL_HOST_PORT ile ayni olmali.
export TEST_DATABASE_URL ?= app:app@tcp(127.0.0.1:3306)/avatar_test?parseTime=true&loc=UTC&multiStatements=true

.PHONY: up down reset migrate seed test test-backend test-web lint logs

up:            ## Her seyi derleyip arka planda baslatir (mysql -> migrate -> seed -> api -> web, ml)
	docker compose up --build -d

down:          ## Servisleri durdurur (veri korunur)
	docker compose down

reset:         ## MySQL volume'unu SILER ve sifirdan kurar (migration + seed dahil)
	docker compose down -v
	docker compose up --build -d

migrate:       ## Bekleyen migration'lari uygular
	docker compose run --rm migrate

seed:          ## Tohum verisini yukler (idempotent)
	docker compose run --rm seed

test: test-backend   ## Go testleri

test-backend:  ## Go unit + entegrasyon testleri (MySQL konteyneri gerekir, otomatik baslatilir)
	docker compose up -d --wait mysql
	cd backend && go test -count=1 ./...

test-web:      ## Web tip kontrolu + build
	cd web && npm ci && npm run build

lint:          ## go vet + TypeScript tip kontrolu
	cd backend && go vet ./...
	cd web && npm run typecheck

logs:          ## Tum servis loglari
	docker compose logs -f --tail=100
