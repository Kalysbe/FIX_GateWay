# Руководство для брокеров — этап 1

Шлюз принимает заявки по **HTTPS REST**. WebSocket брокерам не нужен: заявки создаются запросом, статусы и сделки читаются повторным опросом.

```text
Ваша система  →  HTTPS + Bearer-токен  →  Gateway  →  FIX  →  QUIK
```

Интерактивная схема для брокеров: `{BASE_URL}/broker-swagger`
Полная схема (admin + broker): `{BASE_URL}/swagger`  
Пример базового URL: `https://gateway.example.com` (локально `http://localhost:8080`)

---

## 1. Что выдаёт оператор

Перед первым запросом вы получаете:

| Что | Зачем |
|-----|--------|
| Базовый URL шлюза | Все запросы идут сюда |
| API-токен | Авторизация. Показывается **один раз** |
| Код фирмы `quik_firm` | Привязан к токену, уходит в QUIK как FIX tag 5018 |
| Торговый счёт `account_id` | Например `1-3101-006` → FIX tag 1 |
| Код клиента `client_code` | Например `006` → FIX tag 109 |
| Код класса `board` | Например `LISTEDSTOCKK` → FIX tag 100 |
| Список инструментов | Или берите сами: `GET /api/v1/instruments` |

Токен храните как секрет. В логах оставляйте только первые 8 символов токена. При утечке оператор отзывает токен и выдаёт новый.

Новые токены содержат 48 криптографически случайных байт (384 бита), закодированных в Base64URL без padding. Длина составляет 64 символа, без служебного префикса. Код фирмы и другие данные клиента в секрет не включаются. Видимый префикс раскрывает 48 случайных бит; оставшиеся 336 бит секретны.

В базе сохраняется только версионированный HMAC-SHA-256 токена с серверным `API_TOKEN_PEPPER`. Ранее выданные токены с SHA-256 продолжают действовать до истечения срока или отзыва; при ротации выдаётся новый усиленный токен. Pepper храните отдельно от базы и не меняйте без плановой перевыдачи токенов. Уникальность хранимого digest обеспечивается ограничением `UNIQUE` в базе.

Генерация следует рекомендациям [OWASP](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) по CSPRNG, достаточной энтропии и непрозрачному содержимому токенов. Это описание механизма генерации, а не заявление о сертификации всей системы.

---

## 2. Авторизация

Каждый запрос к `/api/v1/…`:

```http
Authorization: Bearer <token>
Content-Type: application/json
```

- `broker_id` берётся из токена, в теле заявки его нет.
- Если включён IP-whitelist, запросы с других адресов вернут `403` (`ip not allowed`). Сообщите оператору исходящие IP.
- Права токена по умолчанию: `orders:create`, `orders:read`, `orders:cancel`, `trades:read`, `positions:read`, `positions:write`.
- HMAC-подпись на этапе 1 **не обязательна в development**. В `APP_ENV=production` шлюз не стартует без `HMAC_REQUIRED=true` (override: `ALLOW_INSECURE_HMAC=true`). См. [приложение](#приложение-hmac).

Проверка связи:

```bash
curl -sS -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/firm"
```

Ожидается:

```json
{ "quik_firm": "MC0031400000", "firm_name": "ТОО Example" }
```

Если `quik_firm` пустой — заявки создавать нельзя. Попросите оператора задать фирму на токене.

---

## 3. Методы этапа 1

| Метод | Путь | Право | Назначение |
|-------|------|-------|------------|
| `GET` | `/api/v1/firm` | — | Код фирмы текущего токена |
| `GET` | `/api/v1/instruments` | `orders:read` | Справочник инструментов |
| `GET` | `/api/v1/orderbook` | `orders:read` | Стакан (активные заявки) по инструментам |
| `GET` | `/api/v1/accounts` | `orders:read` | Счета брокера |
| `POST` | `/api/v1/orders` | `orders:create` | Создать заявку |
| `GET` | `/api/v1/orders` | `orders:read` | Список заявок (до 100) |
| `GET` | `/api/v1/orders/{id}` | `orders:read` | Заявка + события + сделки |
| `POST` | `/api/v1/orders/{id}/cancel` | `orders:cancel` | Снять заявку |
| `GET` | `/api/v1/trades` | `trades:read` | Сделки (до 100) |
| `GET` | `/api/v1/positions` | `positions:read` | Последний снимок позиций |
| `PUT` | `/api/v1/positions` | `positions:write` | Установить деньги и/или бумаги в QUIK (`35=AL`) |
| `PUT` | `/api/v1/positions/cash` | `positions:write` | Установить денежную позицию в QUIK |
| `PUT` | `/api/v1/positions/instruments` | `positions:write` | Установить позицию по бумагам в QUIK |
| `DELETE` | `/api/v1/positions/cash` | `positions:write` | Удалить денежную позицию в QUIK |
| `DELETE` | `/api/v1/positions/instruments` | `positions:write` | Удалить позицию по бумагам в QUIK |

`{id}` — UUID заявки из ответа шлюза (`order.id`), не `client_order_id`.

Замены заявки (`replace`) на этапе 1 нет. Нужно изменить цену или количество — снимите заявку и создайте новую с **другим** `client_order_id`.

---

## 4. Создать заявку

```http
POST /api/v1/orders
```

```json
{
  "account_id": "1-3101-006",
  "client_code": "006",
  "client_order_id": "ORD-20260817-0001",
  "symbol": "KG0105166315",
  "board": "LISTEDSTOCKK",
  "side": "BUY",
  "order_type": "LIMIT",
  "time_in_force": "DAY",
  "quantity": "1",
  "price": "400"
}
```

```bash
curl -sS -X POST "$BASE_URL/api/v1/orders" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: $(uuidgen)" \
  -d '{
    "account_id": "1-3101-006",
    "client_code": "006",
    "client_order_id": "ORD-20260817-0001",
    "symbol": "KG0105166315",
    "board": "LISTEDSTOCKK",
    "side": "BUY",
    "order_type": "LIMIT",
    "time_in_force": "DAY",
    "quantity": "1",
    "price": "400"
  }'
```

### Поля

| Поле | Обязательно | Описание |
|------|-------------|----------|
| `account_id` | да | Торговый счёт → FIX 1. Должен совпадать с `trading_account` или `code` активного счёта брокера из `GET /api/v1/accounts` |
| `client_code` | да | Код клиента → FIX 109 |
| `client_order_id` | да | Ваш уникальный id заявки. Нельзя повторять у того же брокера |
| `symbol` | да | Тикер или ISIN из `GET /api/v1/instruments` → FIX 48 |
| `board` | да | Код класса → FIX 100 |
| `side` | да | `BUY` или `SELL` |
| `order_type` | да | `LIMIT` или `MARKET` |
| `quantity` | да | > 0, строка или число |
| `price` | для `LIMIT` | > 0, строка или число |
| `time_in_force` | нет | `DAY` (по умолчанию), `IOC`, `FOK`, `GTC` |

Не передавайте `broker_id`, `quik_firm`, валюту и неизвестные поля: JSON с лишним ключом вернёт `400`.

Код фирмы всегда берётся с токена. Валюта — из справочника инструмента.

### Ответ

- `201` — заявка создана (`idempotent: false`)
- `200` — повтор с тем же `client_order_id` (`idempotent: true`), возвращается уже существующая заявка

```json
{
  "order": {
    "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "client_order_id": "ORD-20260817-0001",
    "status": "QUEUED",
    "symbol": "KG0105166315",
    "board": "LISTEDSTOCKK",
    "side": "BUY",
    "quantity": "1",
    "price": "400",
    "filled_quantity": "0",
    "quik_firm": "MC0031400000",
    "request_id": "…"
  },
  "idempotent": false
}
```

`201` означает «принято шлюзом», а не «стоит в стакане». Дальше опрашивайте статус.

---

## 5. Идемпотентность

Ключ: пара `(broker_id, client_order_id)`.

Правила этапа 1:

1. Генерируйте `client_order_id` у себя **до** отправки. Не используйте UUID шлюза.
2. При обрыве сети или таймауте **повторите тот же** `POST` с тем же `client_order_id`.
3. `200` + `idempotent: true` — дубль не создан, работайте с возвращённой заявкой.
4. Шлюз **не сравнивает** тело повторного запроса с первым. Другая цена с тем же `client_order_id` не создаст новую заявку — вернётся старая. Новый смысл = новый id.
5. Рекомендуемый формат: `ORD-{yyyyMMdd}-{seq}` или UUID. Длина должна быть разумной (десятки символов, не килобайты).

---

## 6. Статусы и опрос

После `POST` заявка обычно в `QUEUED`, затем уходит в QUIK.

| Статус | Что это значит |
|--------|----------------|
| `QUEUED` | Принята, ждёт отправки в FIX |
| `ROUTING` | Выбирается адаптер |
| `SENT` | Ушла в QUIK, ещё нет ACK |
| `ACKNOWLEDGED` | Принята рынком / QUIK |
| `PARTIALLY_FILLED` | Частичное исполнение |
| `FILLED` | Исполнена полностью |
| `CANCEL_PENDING` | Запрос на снятие отправлен |
| `CANCELLED` | Снята |
| `REJECTED` | Отклонена (смотрите `reject_reason`) |
| `EXPIRED` | Истекла по TIF |
| `FAILED` | Ошибка маршрутизации до принятия рынком |

Конечные: `FILLED`, `CANCELLED`, `REJECTED`, `EXPIRED`, `FAILED`.

На этапе 1 **пуша нет**. Опрашивайте заявку:

```http
GET /api/v1/orders/{id}
```

Рекомендация:

- первые 5 секунд — каждые 200–500 мс;
- далее — каждые 1–2 с, пока статус не конечный;
- параллельно можно читать `GET /api/v1/orders` или `GET /api/v1/trades`.

В `GET /api/v1/orders/{id}` приходят:

- `order` — текущее состояние, `filled_quantity`, `avg_price`, `quik_order_id`, `reject_reason`;
- `events` — журнал переходов;
- `trades` — сделки по этой заявке.

Список заявок и сделок без фильтрации: последние **100** записей.

---

## 7. Снять заявку

```http
POST /api/v1/orders/{id}/cancel
```

Тело не нужно. Снятие ставится в очередь (`CANCEL_PENDING`) и уходит в QUIK.

Снять можно из `ACKNOWLEDGED` и `PARTIALLY_FILLED`. Из `QUEUED` / `SENT` / конечных статусов вернётся `400` (`cannot cancel from status …`).

`200` — запрос принят, не факт, что бумага уже снята. Дождитесь `CANCELLED` или исполнения (`FILLED`), если рынок успел закрыть остаток.

---

## 8. Справочники

**Инструменты** — `GET /api/v1/instruments`:

```json
{
  "instruments": [
    {
      "id": 1,
      "internal_symbol": "KG0105166315",
      "quik_symbol": "KG0105166315",
      "board": "LISTEDSTOCKK",
      "currency": "KZT",
      "security_type": "CS",
      "status": "ACTIVE"
    }
  ]
}
```

В заявке используйте `quik_symbol` (или `internal_symbol`) и тот же `board`. Неизвестная пара symbol+board → `400`.

**Стакан** — `GET /api/v1/orderbook`:

Живые агрегированные заявки рынка по каждому инструменту, на который шлюз подписан по FIX (`35=V`, полный стакан). Это не ваши заявки (`GET /api/v1/orders`), а видимый спрос/предложение для построения стакана торгов.

```http
GET /api/v1/orderbook
GET /api/v1/orderbook?symbol=GBA02280810&board=LIST_GBA
```

```json
{
  "source": "live",
  "live": true,
  "books": [
    {
      "symbol": "GBA02280810",
      "security_id": "GBA02280810",
      "board": "LIST_GBA",
      "currency": "KGS",
      "last": "100.0",
      "bid": "99.5",
      "bid_size": "10",
      "offer": "100.5",
      "offer_size": "12",
      "bids": [
        { "price": "99.5", "size": "10" },
        { "price": "99.0", "size": "5" }
      ],
      "offers": [
        { "price": "100.5", "size": "12" },
        { "price": "101.0", "size": "4" }
      ],
      "updated_at": "2026-09-04T06:00:00Z"
    }
  ]
}
```

- `bids` — спрос, лучшая цена первой (по убыванию).
- `offers` — предложение, лучшая цена первой (по возрастанию).
- `bid` / `offer` — лучшие уровни, те же что `bids[0]` и `offers[0]`.
- Без `symbol`/`board` вернутся все подписанные инструменты.
- Если подписки ещё нет: `live: false`, `books: []`, в `note` пояснение. Опросите снова после того, как оператор нажмёт «Подписаться по FIX».

Опрашивайте этот метод, чтобы держать стакан актуальным. Отдельного WebSocket для брокеров нет.

**Счета** — `GET /api/v1/accounts`. В `account_id` заявки передавайте торговый код (`trading_account`), не внутренний числовой id.

**Позиции** — `GET /api/v1/positions`.

Токен определяет брокера. Код брокера (`2040` у Freedom) — это та же **фирма**, что в колонке «Фирма» снимка QUIK. Шлюз отдаёт только строки этой фирмы: клиенты, счета, бумаги и деньги. Чужие фирмы (`92`, `888`, …) не возвращаются.

Если снимка ещё нет, вернётся пустой список и пояснение. Это нормально до первой синхронизации оператором.

Установка позиций идёт в QUIK через FIX-адаптер: шлюз шлёт Position Maintenance Request (`35=AL`) с блоками ML (деньги) и DL (бумаги) в разметке ARQA §2.33, ждёт `35=AM` и обновляет снимок запросом `35=AN` → `35=AP`.

```http
PUT /api/v1/positions
```

```json
{
  "client_code": "kalys",
  "account_code": "1-3101-2040",
  "cash": [
    { "currency": "KGS", "amount": "1500000", "tag": "EQTV" }
  ],
  "instruments": [
    { "symbol": "KG0105166315", "board": "LISTEDSTOCKK", "quantity": "100" }
  ]
}
```

Нужно `positions:write`. `GET` после этого вернёт актуальный снимок QUIK. Чтобы обнулить строку, вызовите `DELETE /api/v1/positions/cash` или `DELETE /api/v1/positions/instruments` с теми же ключами — в QUIK уйдёт `35=AL` с Cancel/Delete.

---

## 9. Ошибки

Формат:

```json
{ "error": "текст", "details": {} }
```

| HTTP | Когда |
|------|--------|
| `400` | Неверный JSON, валидация, нет инструмента, нельзя снять |
| `401` | Нет токена, неверный или просроченный токен, плохая HMAC-подпись |
| `403` | Нет права на метод или IP не в whitelist |
| `404` | Заявка не найдена или чужая |
| `429` | Лимит запросов или заявок |
| `503` | Шлюз не готов (`GET /ready`) |

Лимиты по умолчанию (могут быть ниже на брокере):

- до **100** запросов в секунду на токен;
- до **20** созданий заявок в секунду на токен.

При `429` сделайте паузу и повторите. Создание заявки повторяйте с тем же `client_order_id`.

При `422` заявка отклонена торговым ограничением и не поставлена в очередь.
Поле `code` содержит `TRADING_PAUSED`, `MAX_ORDER_QUANTITY` или `MAX_OPEN_ORDERS`.
Повторять отказ без изменения условий не нужно. Пауза приёма не блокирует отмены;
повтор уже принятой заявки с тем же `client_order_id` возвращает исходную заявку.
Настройка ограничений описана в [Торговых ограничениях](trading-limits.md).

Необязательные заголовки для разбора инцидентов:

- `X-Request-ID` — ваш id запроса; шлюз вернёт его в ответе;
- `X-Trace-ID` — сквозной id цепочки.

Сохраняйте `order.id`, `client_order_id`, `request_id` и HTTP-код.

---

## 10. Чеклист первого дня

1. `GET /health` и `GET /ready` — шлюз жив, есть хотя бы один FIX-адаптер.
2. `GET /api/v1/firm` — токен принят, фирма задана.
3. `GET /api/v1/instruments` — есть нужный `symbol` + `board`.
4. `GET /api/v1/orderbook?symbol=…&board=…` — есть уровни `bids`/`offers` (или пустой стакан, если заявок нет).
5. `POST /api/v1/orders` с тестовым `client_order_id` — `201`.
6. Повтор того же `POST` — `200`, `idempotent: true`, тот же `order.id`.
7. `GET /api/v1/orders/{id}` — статус доходит до `ACKNOWLEDGED` / `FILLED` / `REJECTED`.
8. Для лимитки: `POST …/cancel` из `ACKNOWLEDGED`, затем опрос до `CANCELLED`.

Тестовые заявки согласуйте с оператором (бумага, цена вне рынка, маленький лот).

---

## 11. Что не входит в этап 1

- WebSocket и webhook — статусы только опросом.
- Входящий FIX от брокера — шлюз сам говорит с QUIK по FIX.
- Replace / amend заявки.
- Подпись HMAC по умолчанию.

Это появится на следующих этапах. Контракт заявок (REST + `client_order_id`) сохранится.

---

## Приложение: HMAC

Нужно, только если оператор включил обязательную подпись или вы сами шлёте `X-Signature`.

Заголовки:

- `X-Timestamp` — Unix time в секундах, окно ±5 минут;
- `X-Nonce` — уникальная строка на запрос (8–128 символов, одноразовый в пределах токена, хранится 10 минут);
- `X-Signature` — hex(HMAC-SHA256(token, payload)).

Строка для подписи (перевод строки `\n`):

```text
{timestamp}\n{nonce}\n{METHOD}\n{path}\n{body}
```

Path — точный request-target: escaped path и исходная query-строка, например `/api/v1/orders?limit=10&status=NEW`. Порядок и кодирование query-параметров должны совпадать с отправленным URL. Без query подпись не меняется. Секрет — **сам API-токен**. Для `GET` тело пустое. При наличии любого HMAC-заголовка требуется полный набор. Повтор nonce отклоняется в течение 10 минут, включая запросы с timestamp из будущего в допустимом окне.
