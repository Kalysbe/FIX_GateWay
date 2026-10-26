# Текущие торги из БД QUIK

Источник — таблицы `public."Params"`, `public."Securities"` и `public."Classes"`
в БД экспорта QUIK, в том же подключении `QUIK_EXPORT_DATABASE_URL`, что и
`Orders`, только для чтения. Для локальной разработки это
`192.168.0.89:5432/quik_export2`, на проде — PostgreSQL QUIK на
`172.23.10.84:5432` (DSN хранится в секрете `quik-export-database-url`).
Роли Gateway нужен `SELECT` на `Orders`, `Params`, `Securities` и `Classes`.

По решению пользователя от 26.10.2026 строка содержит только поля, в этом
порядке:

| Поле | Источник |
|---|---|
| `ClassCode`, `SecCode` | `Params` |
| `CouponValue` | `Securities` |
| `status` | `Params` |
| `FaceUnit`, `SecShortName`, `SecFullName` | `Securities` |
| `ClassName` | `Classes` |

Значения передаются как результат `row_to_json`: числа остаются JSON-числами
с исходной точностью, строки — без обрезки и перевода, `null` сохраняется.
`Securities` сопоставляется по `ClassCode` + `SecCode` (один `SecCode` бывает
в нескольких классах с разными названиями), `Classes` — по `ClassCode`. Оба
справочника хранятся по `TradeDate`; берётся последняя запись. Нет записи в
справочнике — `null`. Рыночные данные общие: строки не фильтруются по фирме
токена.

Отдаются только строки `Params` текущего торгового дня (`TradeDate` —
сегодняшняя дата по Бишкеку, UTC+6; сервер БД QUIK работает в другом поясе)
и только выбранных классов. Классы выбирает администратор на странице
«Текущие торги» (роль оператора и выше) или через
`PATCH /api/v1/admin/market-data/settings` `{"classes": ["LISTBND"]}`; список
хранится в `gateway_settings.market_data_classes`, по умолчанию `LISTBND`
(миграция `000033`), изменение пишется в аудит как
`MARKET_DATA_CLASSES_UPDATED`. Список классов для выбора —
`GET /api/v1/admin/classes`. Если настройку прочитать нельзя, строки не
отдаются (503), а не отдаются без фильтра.

Строка определяется парой `ClassCode` + `SecCode`.

| Метод | Ответ |
|---|---|
| `GET /api/v1/market-data` (брокер, `orders:read`) | JSON-массив строк |
| `GET /api/v1/market-data/{board}/{symbol}` | Одна строка по `ClassCode`/`SecCode` или 404 |
| `GET /api/v1/admin/market-data`, `GET /api/v1/admin/market-data/{board}/{symbol}` | То же для админки |
| `GET`/`PATCH /api/v1/admin/market-data/settings` | Выбранные классы |
| `GET /api/v1/admin/classes` | `ClassCode` и `ClassName` из QUIK `Classes` |
| gRPC/SSE `marketdata.tick` | Одна изменённая строка на событие |

`board` и `symbol` фильтруют `ClassCode` и `SecCode` (с обрезкой пробелов
`char(n)`). Порядок — `ClassCode`, `SecCode`. `limit` — 1–5000, по умолчанию
1000; `offset` — 0–10000000; продолжение — в заголовках `X-Has-More` и
`X-Next-Offset`.

Лента опрашивает таблицу раз в секунду и публикует изменившиеся строки; после
смены классов или даты строки, вышедшие из выборки, приходят с `removed`.
В native gRPC JSON строки находится в `Event.data_json`, `Event.data` пуст;
исчезновение строки — `Event.removed=true`. Browser SSE передаёт строку в
`data`, `removed` — рядом с `type`. Снимка при подписке нет: начальный набор
загружается через REST, затем применяются изменения по ключу `ClassCode` +
`SecCode`. Первый успешный опрос после старта — базовая выборка и не
рассылается.

Ошибка чтения даёт 503 без подстановки FIX-котировок. Фоновая ошибка не
считается пустой таблицей и не порождает удалений.

FIX-подписка Market Data (`35=V/W/X`) продолжает работать на сервере и
сохраняет снимки в `fix_md_snapshots`, но наружу через эти методы не
отдаётся. Стакан `GET /api/v1/orderbook` по-прежнему строится из FIX:
глубины рынка в Params нет. Котировки `Quotes(symbol)` на странице
«Котировки» не менялись.

Интеграционные тесты используют те же `QUIK_ORDERS_*TEST_DATABASE_URL`, что и
тесты `Orders`; локальный стенд создаёт таблицы `Params`, `Securities` и
`Classes` (`deploy/local-quik-export.sql`). Его классы — не `LISTBND`: выберите
`LIST_GBA` или `LISTEDSTOCKK` на странице «Текущие торги».
