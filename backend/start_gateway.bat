@echo off
set DATABASE_URL=postgres://postgres:123@localhost:5432/fix_gateway?sslmode=disable
set MIGRATIONS_PATH=C:/Users/krahmanberdiev/Desktop/FIX Gateway/backend/migrations
set JWT_SECRET=dev-jwt-secret-change-me
set API_TOKEN_PEPPER=dev-token-pepper-change-me
set ADMIN_EMAIL=admin@localhost
set ADMIN_PASSWORD=admin123
set HTTP_ADDR=:8080
set LB_STRATEGY=least_connections
set CORS_ORIGINS=http://localhost:5173
cd /d "c:\Users\krahmanberdiev\Desktop\FIX Gateway\backend"
gateway.exe >> gateway.out.log 2>>&1
