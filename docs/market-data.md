# Текущие торги из БД QUIK

Источник — таблицы `public."Params"`, `public."Securities"` и `public."Classes"`
в БД экспорта QUIK, в том же подключении `QUIK_EXPORT_DATABASE_URL`, что и
`Orders`, только для чтения: PostgreSQL QUIK `172.23.10.84:5432/quik_export`
(DSN хранится в `.env` или секрете `quik-export-database-url`; `192.168.0.89`
не используется). Роли Gateway нужен `SELECT` на `Orders`, `Params`,
`Securities`, `Classes`, `Firms`, `Trdaccs` и `Trades`.

По решению пользователя (26.10.2026, дополнено 02.11.2026) строка содержит только
поля, в этом порядке:

| Поле | Источник |
|---|---|
| `ClassCode`, `SecCode` | `Params` |
| `CouponValue` | `Securities` |
| `status` | `Params` |
| `FaceUnit`, `SecShortName`, `SecFullName` | `Securities` |
| `ClassName` | `Classes` |
| `lotsize`, `tradingstatus`, `pricemin`, `pricemax`, `realminstep` | `Params`: условия заявки |
| `bid`, `biddepth`, `offer`, `offerdepth`, `last`, `qty`, `time` | `Params`: рынок сейчас |
| `open`, `high`, `low`, `prevprice`, `waprice`, `numtrades`, `voltoday`, `valtoday` | `Params`: итоги дня |
| `accruedint`, `yield`, `nextcoupon`, `couponperiod` | `Params`: облигации |

`change` и `lastchange` намеренно не передаются: в данных QUIK они считаются как будто
от нуля (`change` = 95 при `last` = 95). В колонке `time` дата фиктивная
(`1900-01-01T16:40:15`), значимо только время. `realminstep`, `offer`, `offerdepth`,
`open` и `accruedint` по `LIST_GBA` и `LIST_GD` на 02.11.2026 пусты или нулевые.

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
«Классы QUIK» (`/classes`, роль оператора и выше) или через
`PATCH /api/v1/admin/market-data/settings` `{"classes": ["LIST_GBA"]}`; список
хранится в `gateway_settings.market_data_classes`, пустой список (по умолчанию)
отдаёт все классы, изменение пишется в аудит как
`MARKET_DATA_CLASSES_UPDATED`. Список классов для выбора —
`GET /api/v1/admin/classes`. Если настройку прочитать нельзя, строки не
отдаются (503), а не отдаются без фильтра.

По поручению пользователя 02.11.2026 это глобальная настройка системы: она
ограничивает строки `Params`, `Trades`, `Orders`, `Securities` и `Classes` во всех
потребителях (админка, брокерский REST, gRPC/SSE, счётчики панели), а также
`GET /api/v1/instruments`, `/api/v1/orderbook`, подписку FIX Market Data и
приём новых заявок. Заявка вне списка классов отклоняется как
`INSTRUMENT_NOT_FOUND`. Исключения:

- `GET /api/v1/admin/classes` показывает все классы: из этого списка выбирают;
- отмена заявки работает для любого класса: активная заявка не должна остаться
  без возможности снятия после сужения списка.

Настройка не ограничивает позиции (в `MoneyLimits` и `DepoLimits` нет `ClassCode`) и
локальную таблицу Gateway `orders`. После сужения списка строки
вне выборки приходят в потоках `order.updated` и `marketdata.tick` с признаком
удаления. Здесь он означает только «больше не отдаётся», а не отмену или
исполнение заявки.

## Инструменты

По поручению пользователя 02.11.2026 Gateway не хранит инструменты и не загружает
их справочник по FIX: таблица `instruments`, `POST /api/v1/admin/instruments/sync` и
`POST /api/v1/admin/quik/sync` удалены. Единственный источник — `public."Securities"`
БД QUIK, строки за последнюю `TradeDate` таблицы, поэтому инструмент, который QUIK
больше не выгружает, недоступен.

- `GET /api/v1/instruments` и `GET /api/v1/admin/instruments` отдают эти строки как
  есть (`row_to_json`), постранично (`limit`, `offset`, `X-Has-More`, `X-Next-Offset`).
- Новая заявка проверяется по `ClassCode` + `SecCode`, в FIX идут значения из строки;
  валюта FIX 15 — `FaceUnit`.
- Подписка FIX Market Data, питающая `/api/v1/orderbook`, строится по этим строкам.
  FIX 167 выводится из `SecType`: 1 → `CS`, 2 → `GO`, 7 → `FXSPOT`, прочие — без
  тега. На 02.11.2026 это совпадает со значениями прежнего FIX-справочника.

## Страница инструмента

Строка определяется парой `ClassCode` + `SecCode`. У каждого инструмента в
админке есть своя страница (`/market-data/{ClassCode}/{SecCode}`, ссылка из
колонки `SecCode`): все колонки его строки `Params` за сегодня, строки
`Securities` и `Classes` целиком и его котировки. У инструмента класса вне
выбора страницы нет (404).

Котировки (страница «Котировки» и раздел страницы инструмента) Gateway
читает сам из `public."Orders"` той же БД: только строки с `State` =
`Активна` и `TradeDate` за сегодня (по Бишкеку, UTC+6), всех фирм, в
исходном виде. Заявки прошлых дней, которые QUIK по-прежнему выгружает как
«Активна», в котировки не попадают. QUIK Module API не используется
(сделки тоже читаются из БД QUIK, таблица `public."Trades"`).
Если выгрузка `Orders` остановилась, сегодняшних котировок не будет.

| Метод | Ответ |
|---|---|
| `GET /api/v1/market-data` (брокер, `orders:read`) | JSON-массив строк |
| `GET /api/v1/market-data/{board}/{symbol}` | Одна строка по `ClassCode`/`SecCode` или 404 |
| `GET /api/v1/admin/market-data`, `GET /api/v1/admin/market-data/{board}/{symbol}` | То же для админки |
| `GET`/`PATCH /api/v1/admin/market-data/settings` | Выбранные классы |
| `GET /api/v1/admin/classes` | `ClassCode` и `ClassName` из QUIK `Classes` |
| `GET /api/v1/admin/market-data/{board}/{symbol}/details` | Страница инструмента: вся строка `Params` за сегодня, строки `Securities` и `Classes`, котировки |
| `GET /api/v1/admin/quotes` | Котировки: сегодняшние строки `Orders` с `State` = `Активна` всех фирм |
| `GET /api/v1/quotes?board=&symbol=` (брокер, `orders:read`) | Котировки одного инструмента всех участников без их раскрытия: только `side`, `price`, `quantity` (`Balance`), `volume` = `price` × `quantity` |
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

## График цен

По поручению пользователя 05.11.2026 (запрос брокера на исторические данные
об исполненных сделках) у брокерского API (`orders:read`) есть один метод
`GET /api/v1/market-data/trades?board=&symbol=&from=&to=&limit=&offset=`:
исполненные сделки КФБ всех участников из `public."Trades"`.

- Одна строка на сделку: в `Trades` строка на каждую сторону (ключ
  `TradeNum`, `Operation`, `ClassCode`, `TradeDate`), обе стороны совпадают
  по цене, количеству и времени (проверено на `172.23.10.84` 05.11.2026).
  `TradeNum` не повторяется между классами.
- Поля: `TradeNum`, `ClassCode`, `SecCode`, `TradeDateTime`, `Price`, `Qty`,
  `SettleCurrency`, `Kind`, `CanceledDateTime`. Участников (фирмы, счета,
  коды клиентов) нет, как в котировках. Числа и строки — в исходном виде.
- `TradeDateTime`/`CanceledDateTime` в QUIK — местное время КФБ без пояса:
  заявка, отправленная в 08:32:57 UTC, записана с `OrderDateTime` 14:32:57.
  Gateway отдаёт их строкой с `+06:00` и миллисекундами (`TradeTimeMs`,
  если QUIK его заполнит; на 05.11.2026 пуст). Часовой пояс сервера БД QUIK
  (`Asia/Almaty`) не влияет.
- Порядок — `TradeDateTime`, затем `TradeNum`. `board`/`symbol` — инструмент,
  `from`/`to` — период включительно: дата `ГГГГ-ММ-ДД` (день КФБ) или время
  RFC 3339. Без них — все сделки. Пагинация как у заявок (`limit` 1–1000).
- Выбранные классы ограничивают выдачу.
- Отмена — `CanceledDateTime` (на 05.11.2026 отменённых сделок нет, как
  QUIK выгружает отмену, не проверено). Корректировок сделок в QUIK нет.
- Глубина на 05.11.2026 — с 01.07.2026, 154 сделки. Новые сделки
  появляются в `Trades` с задержкой выгрузки QUIK; брокер повторяет запрос с
  `from` = время последней полученной сделки.

FIX-подписка Market Data (`35=V/W/X`) продолжает работать на сервере, но
наружу через эти методы не отдаётся и в БД не сохраняется (таблицы
`fix_md_snapshots`/`fix_md_quotes` удалены 29.10.2026). Стакан `GET /api/v1/orderbook` по-прежнему строится из FIX:
глубины рынка в Params нет. Котировки `Quotes(symbol)` на странице
«Котировки» не менялись.

Интеграционные тесты используют те же `QUIK_ORDERS_*TEST_DATABASE_URL`, что и
тесты `Orders`; локальный стенд создаёт таблицы `Params`, `Securities` и
`Classes` (`deploy/local-quik-export.sql`).
