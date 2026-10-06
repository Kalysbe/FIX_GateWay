# Broker Desk

Фронтенд для имитации **нескольких одновременных брокерских клиентов** против реального FIX Gateway.

Профили хранятся в `localStorage`, токены — в `sessionStorage` текущей вкладки и не попадают в JSON-экспорт. Заявки идут в API gateway (`Authorization: Bearer sk_…`).

## Запуск

1. Поднимите gateway на `:8080`.
2. В Admin UI создайте брокеров и API-токены (`quik_firm` обязателен на токене).
3. Запустите Desk:

```bash
cd broker-app
npm install
npm run dev
```

→ http://localhost:5174 (Vite proxy → `localhost:8080`)

## Как подключать

1. **+ Сессия**
2. Вставьте API-токен (`sk_live_…` / `sk_test_…`)
3. Заполните `account_id` и `client_code`
4. **Connect** — Desk дергает `GET /api/v1/orders` с этим Bearer-токеном
5. Отправка заявок: `POST /api/v1/orders`

`Connect all` / burst — несколько токенов параллельно.

Admin UI: http://localhost:5173
