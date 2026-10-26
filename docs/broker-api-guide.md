# API для брокеров: REST + gRPC

REST создаёт и отменяет заявки, изменяет позиции и возвращает текущие данные. gRPC передаёт обновления заявок, сделок, позиций и рынка.

## Подключение

| Интерфейс | Адрес / контракт | Авторизация |
|---|---|---|
| REST | `https://<host>`; JSON; схема `/broker-swagger` | Заголовок `Authorization: Bearer <API-токен>` |
| gRPC | `<host>:<port>`; RPC `/fixgateway.realtime.v1.RealtimeService/Stream` | Metadata `authorization: Bearer <API-токен>`; `x-client-type: broker` необязателен |

Подключение: VPN + TLS + API-токен. Клиентские сертификаты и HMAC по умолчанию не требуются. Оператор ограничивает доступ к API сетью VPN.

Для gRPC нужны TLS 1.3 и проверка сертификата сервера: системное доверие или CA оператора. Адреса и токен выдаёт оператор. Protobuf-контракт: [realtime.proto](../backend/proto/realtime/v1/realtime.proto). При отдельно включённом mTLS также нужны клиентский сертификат и ключ.

Заявки, сделки и счета ограничены текущим брокером. Позиции фильтруются по коду брокера и `quik_firm` токена. Рыночные данные общие.

Цены, суммы и количества в ответах — строки. В REST-запросах десятичные значения принимаются строкой или числом. `created_at`, `updated_at` и другие временные метки — RFC 3339; `clearing_date` и `trade_date` — торговые даты из QUIK.

## Фирма, счета, инструменты

### Изменение контракта 26.10.2026

`GET /api/v1/accounts` возвращает массив строк QUIK `Trdaccs` вместо `{accounts: [...]}`, `GET /api/v1/firm` — строку QUIK `Firms` вместо `{quik_firm, firm_name}`. Счета, заведённые вручную в Gateway, больше не действуют.

Все три метода требуют `orders:read`. gRPC-событий для этих справочников нет.

| REST | Назначение | Ответ `200` |
|---|---|---|
| `GET /api/v1/firm` | Фирма, привязанная к токену | Строка таблицы QUIK `Firms` как есть; 404, если фирмы нет |
| `GET /api/v1/accounts` | Торговые счета фирмы токена | JSON-массив строк таблицы QUIK `Trdaccs` как есть |
| `GET /api/v1/instruments` | Активные инструменты | `{instruments: [...]}`; при отсутствии записей `instruments: null` |

| Объект | Поля |
|---|---|
| `instruments[]` | `id` — числовой ID; `internal_symbol` — символ шлюза; `quik_symbol` — символ QUIK; `board` — класс; `currency` — валюта; `security_type` — тип бумаги; `status` — `ACTIVE` |

Фирма и счета берутся из БД QUIK (`Firms`, `Trdaccs`); имена колонок и значения не меняются. В `account_id` заявки передавайте `TrdAccId` счёта своей фирмы: заявка принимается, только если в `Trdaccs` есть строка с этим `TrdAccId` и `FirmId` фирмы токена. Если источник QUIK недоступен, ответ — 503. Для инструмента используйте `quik_symbol` или `internal_symbol` вместе с `board`.

## Заявки

| REST / gRPC | Назначение | Возвращаемые данные | Право |
|---|---|---|---|
| `POST /api/v1/orders` | Создать заявку | `{order, idempotent}`; `201` — новая, `200` — ранее созданная, `409` — ID занят другой заявкой | `orders:create` |
| `GET /api/v1/orders` | Строки Orders своей фирмы | Массив исходных строк БД | `orders:read` |
| `GET /api/v1/orders/active` | Строки Orders своей фирмы с `State` = `Активна` | Массив исходных строк БД | `orders:read` |
| `GET /api/v1/orders/{id}` | Одна текущая строка Orders | Исходная строка БД без обёрток | `orders:read` |
| `POST /api/v1/orders/{id}/cancel` | Запросить отмену; тело не требуется | `200`, `{order: Order}` | `orders:cancel` |
| gRPC `order.updated` | Изменения строк Orders своей фирмы | `event.data_json` — исходный JSON строки; `event.removed=true` удаляет её из текущего набора | `orders:read` |

Источник чтения — `quik_export2.public."Orders"`. `FirmId` ограничен `quik_firm`
авторизованного токена (при пустом значении — кодом брокера). Параметры клиента
не расширяют этот доступ. Список принимает `limit` 1–1000 (по умолчанию 200)
и `offset`; заголовки `X-Has-More` и `X-Next-Offset` задают продолжение. gRPC не отправляет
начальный список: загрузите его через REST. Изменения проверяются каждые две
секунды, включая заявки, поданные вне Gateway. Ошибка БД даёт 503 без подстановки FIX.

Ответ содержит только исходные столбцы `Orders`: `OrderNum`, `TradeDate`,
`ClassCode`, `FirmId`, `SecCode`, `Operation`, `State`, `Qty`, `Price`, `Balance`
и все остальные поля таблицы. Регистр, типы, числа и `null` сохраняются.
Служебные ID, рассчитанные статусы и объёмы, история и trace не добавляются.
Ключ URL `quik-20261022-LIST_GBA-22455` строится из даты, класса и номера;
в ответ он не включается. В gRPC для сохранения точности PostgreSQL numeric
используется `Event.data_json`, а не `Event.data` с числами double. Поле
`removed` принадлежит конверту события и не изменяет JSON строки.

GET также принимает UUID исходной заявки Gateway и возвращает состояние только
при наличии соответствующей строки QUIK; иначе 404. Отмена принимает UUID или ключ QUIK, но требует
исходной локальной заявки и активной строки QUIK; для внешних заявок без
локального запроса — 409. Исчезновение строки не переписывает локальную историю
и не означает подтверждённую отмену или исполнение. Подробнее: [Заявки](orders.md).

### Создание: тело POST

| Поле | Обязательно | Значение |
|---|---|---|
| `account_id` | Да | `TrdAccId` счёта фирмы токена из QUIK `Trdaccs` |
| `client_code` | Да | Код клиента |
| `client_order_id` | Да | Ваш ID заявки, уникальный у брокера |
| `symbol` | Да | Символ из справочника |
| `board` | Да | Код класса инструмента |
| `side` | Да | `BUY` / `SELL` |
| `order_type` | Да | `LIMIT` / `MARKET` |
| `quantity` | Да | Количество > 0 |
| `price` | Для `LIMIT` | Цена > 0 |
| `time_in_force` | Нет | `DAY` по умолчанию; также `IOC`, `FOK`, `GTC` |

```json
{
  "account_id": "1-3101-006",
  "client_code": "006",
  "client_order_id": "ORD-0001",
  "symbol": "KG0105166315",
  "board": "LISTEDSTOCKK",
  "side": "BUY",
  "order_type": "LIMIT",
  "quantity": "1",
  "price": "400",
  "time_in_force": "DAY"
}
```

`broker_id` и `quik_firm` определяются по токену, `currency` — по инструменту; их в теле нет. Неизвестные поля возвращают `400`.

Идемпотентность: `(broker_id, client_order_id)`. Повторный POST с теми же параметрами возвращает исходную заявку с `idempotent: true`. Если параметры отличаются (инструмент, board, сторона, тип, срок, количество, цена, счёт, код клиента или фирма токена), ответ — `409` с `code: IDEMPOTENCY_CONFLICT`, заявка не создаётся. При таймауте повторяйте тот же запрос с тем же `client_order_id`. Для другой заявки нужен новый ID.

`201` означает принятие шлюзом. Подтверждение рынка приходит в статусе заявки через REST или gRPC.

### Order: подтверждение приёма торговой команды Gateway

Следующие поля относятся к HTTP-ответам создания/отмены. Чтение текущего
состояния и gRPC используют исходную строку `Orders`; адаптер не публикует
его состояние в `order.updated`.

| Поля | Данные |
|---|---|
| `id`, `client_order_id` | UUID шлюза и ID брокера |
| `broker_id`, `instrument_id` | Числовые ID брокера и инструмента |
| `account_id`, `client_code` | Торговый счёт и клиент |
| `symbol`, `board`, `currency` | Инструмент, класс, валюта |
| `side`, `order_type`, `time_in_force` | Направление, тип заявки, срок действия |
| `quantity`, `price` | Исходное количество и цена; `price` может быть `null` |
| `filled_quantity`, `avg_price` | Исполненное количество и средняя цена исполнения |
| `status`, `reject_reason` | Статус и причина отказа; причина может быть `null` |
| `adapter_id`, `fix_cl_ord_id`, `quik_order_id` | Адаптер, FIX ID, ID заявки в QUIK; до назначения — `null` |
| `quik_firm`, `firm_name` | Код и название фирмы |
| `created_at`, `updated_at` | Время создания и изменения |
| `request_id`, `trace_id` | ID запроса и трассировки |

### Статусы и отмена

| Статус | Значение |
|---|---|
| `RECEIVED` / `VALIDATING` / `VALIDATED` | Получена / проверяется / проверена |
| `QUEUED` | В очереди на отправку |
| `ROUTING` | Выбирается адаптер |
| `SENT` | Отправлена в QUIK |
| `ACKNOWLEDGED` | Подтверждена рынком |
| `PARTIALLY_FILLED` | Исполнена частично |
| `CANCEL_PENDING` | Ожидается результат отмены |
| `FILLED` | Исполнена полностью |
| `CANCELLED` | Отменена |
| `REJECTED` | Отклонена |
| `EXPIRED` | Истёк срок действия |
| `FAILED` | Ошибка обработки или отправки |

Конечные статусы: `FILLED`, `CANCELLED`, `REJECTED`, `EXPIRED`, `FAILED`.

Отмена разрешена из `ACKNOWLEDGED` и `PARTIALLY_FILLED`. Повтор при `CANCEL_PENDING` возвращает текущую заявку. `200` подтверждает запрос отмены; результат — `CANCELLED`, исполнение или отказ. При отказе отмены заявка может вернуться в `ACKNOWLEDGED` / `PARTIALLY_FILLED`. Изменение цены или количества: отменить и создать заявку с новым `client_order_id`.

### OrderEvent: история заявки

Локальные события сохранены для аудита. Чтение текущей заявки возвращает только
строку `Orders`; `events[]`, исполнения и FIX-журнал в этот ответ не включаются.

## Сделки

| REST / gRPC | Назначение | Возвращаемые данные | Право |
|---|---|---|---|
| `GET /api/v1/trades` | Последние 100 сделок брокера, новые первыми | `{trades: [Trade]}`; без фильтров и пагинации | `trades:read` |
| gRPC `trade.created` | Получать новые исполнения своего брокера | `event.data` — `Execution` | `trades:read` |

gRPC не отправляет начальный список сделок.

| Поля Execution | Данные |
|---|---|
| `id`, `order_id`, `exec_id` | UUID исполнения, UUID заявки шлюза, ID исполнения из FIX |
| `last_qty`, `last_px` | Количество и цена этого исполнения |
| `cum_qty`, `leaves_qty` | Накопленное исполненное количество и остаток заявки |
| `adapter_id`, `created_at` | Адаптер и время записи; `adapter_id` может отсутствовать |

`Trade` в REST содержит все поля `Execution` и дополнительно: `broker_id`, `client_order_id`, `symbol`, `board`, `side`, `client_code`, `quik_firm`, `firm_name`, `fix_cl_ord_id`, `quik_order_id`. В REST `adapter_id`, `fix_cl_ord_id` и `quik_order_id` могут быть `null`. Сделки читаются отдельно через `GET /api/v1/trades` и не добавляются к строке заявки.

## Рыночные данные и стакан

Все методы и событие требуют `orders:read`.

| REST / gRPC | Назначение | Возвращаемые данные |
|---|---|---|
| `GET /api/v1/market-data` | Текущие торги | JSON-массив инструментов (8 полей, см. ниже); при недоступности источника — HTTP 503 |
| `GET /api/v1/market-data/{board}/{symbol}` | Один инструмент | Строка по `ClassCode` и `SecCode`; 404, если её нет за сегодня или класс не выбран |
| `GET /api/v1/orderbook` | Стаканы: спрос и предложение рынка (FIX) | `{source: "live", live, books: [OrderBook]}`; без доступных данных — `live: false`, `books: []`, `note` |
| gRPC `marketdata.tick` | Изменения строк `Params` | `event.data_json` — исходная строка, `event.removed` — строка исчезла |

Строка содержит только поля `ClassCode`, `SecCode`, `CouponValue`, `status`, `FaceUnit`, `SecShortName`, `SecFullName`, `ClassName` — из таблиц QUIK `Params`, `Securities` (по `ClassCode` + `SecCode`) и `Classes`, значения в исходном виде: числа JSON-числами с исходной точностью, строки и `null`. Разбирайте JSON с сохранением точности чисел. Отдаются только инструменты текущего торгового дня (по Бишкеку, UTC+6) из классов, выбранных администратором шлюза (по умолчанию `LISTBND`). Рыночные данные общие для всех брокеров.

```json
{"ClassCode":"LISTBND","SecCode":"GBA02270915","CouponValue":2.500000,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО-2 летние (GBA02270915)","SecFullName":"ГКО-2 летние (GBA02270915)","ClassName":"Листинговые:облигации"}
```

REST-фильтры: `?board=<ClassCode>&symbol=<SecCode>`, оба необязательны; `limit` (1–5000, по умолчанию 1000) и `offset`, продолжение — в заголовках `X-Has-More` и `X-Next-Offset`. Если источник недоступен, ответ — HTTP 503 с `{"error":"market data unavailable"}` и `Retry-After`; пустая успешная выборка — `[]`.

В gRPC снимка при подписке нет: загрузите текущие строки через REST и применяйте события по ключу `ClassCode` + `SecCode`. Таблица опрашивается раз в секунду; событие приходит только для изменившейся строки.

`OrderBook` содержит только `symbol`, `security_id`, `board`, `currency`, `name`, `last`, `last_size`, `last_time`, `bid`, `bid_size`, `offer`, `offer_size`, `bids`, `offers`, `updated_at`. Массивы уровней в REST-стакане присутствуют всегда, в том числе пустые.

### Изменение контракта 26.10.2026

`GET /api/v1/market-data` и gRPC `marketdata.tick` переведены с FIX Market Data на таблицу QUIK `Params`. Вместо `{quotes: [MarketQuote]}` REST возвращает массив исходных строк; в gRPC строка передаётся в `data_json`, без `quotes`, `live` и `replace`, и начальный снимок при подписке больше не приходит. Фильтры `board`/`symbol` относятся к `ClassCode`/`SecCode`. Стакан `/api/v1/orderbook` не изменился.

В тот же день строка сокращена до 8 полей (`ClassCode`, `SecCode`, `CouponValue`, `status`, `FaceUnit`, `SecShortName`, `SecFullName`, `ClassName`); остальные колонки `Params`, включая `TradeDate`, больше не передаются. Отдаются только строки сегодняшнего торгового дня из классов, выбранных администратором (по умолчанию `LISTBND`).

## Позиции

| REST / gRPC | Назначение | Возвращаемые данные | Право |
|---|---|---|---|
| `GET /api/v1/positions` | Последние позиции своей фирмы | `{source, firm_id, cash: [Position], instruments: [Position]}`; при наличии — `snapshot`, `note` | `positions:read` |
| gRPC `positions.updated` | Получать полные снимки позиций своей фирмы | `event.data = {source: "fix", firm_id, cash, instruments}`; без `snapshot` и `raw_fix` | `positions:read` |
| `PUT /api/v1/positions` | Установить деньги и/или бумаги в QUIK | `200`, `PositionWriteResult` | `positions:write` |
| `PUT /api/v1/positions/cash` | Установить одну денежную позицию | `200`, `PositionWriteResult` | `positions:write` |
| `PUT /api/v1/positions/instruments` | Установить одну позицию по бумаге | `200`, `PositionWriteResult` | `positions:write` |
| `DELETE /api/v1/positions/cash` | Удалить денежную позицию в QUIK | `200`, `PositionWriteResult` | `positions:write` |
| `DELETE /api/v1/positions/instruments` | Удалить позицию по бумаге в QUIK | `200`, `PositionWriteResult` | `positions:write` |

`cash` — деньги; `instruments` — бумаги и фьючерсы. REST `source`: `fix`, `manual` или `mixed`. gRPC отправляет полный снимок после `ready` и при обновлениях. Каждый снимок полностью заменяет оба массива. При отсутствии позиций массивы пустые. REST-фильтров по клиенту или инструменту нет.

### Position: поля строки

| Поля | Данные |
|---|---|
| `kind`, `pos_type` | `CASH` / `INSTRUMENT` / `FUTURES`; тип позиции `ML` / `DL` / `FL` / `FP` |
| `firm_id`, `client_code`, `account_code` | Фирма, клиент, счёт |
| `symbol`, `security_id`, `board`, `currency` | Инструмент или денежный тег, идентификатор, класс, валюта |
| `long_qty`, `short_qty`, `net_qty` | Длинная, короткая и чистая позиции |
| `pos_amt_type`, `pos_amt` | Тип и сумма денежной позиции |
| `settl_price` | Расчётная цена |
| `keys`, `items` | Ключи и показатели QUIK, объекты со строковыми значениями; необязательны |
| `adapter_id`, `pos_maint_rpt_id`, `pos_req_id` | Адаптер, ID отчёта и запроса позиций |
| `clearing_date`, `update_action` | Дата расчётов; `0` — новая, `1` — изменение, `2` — удаление |
| `source`, `raw_fix` | Источник строки и исходный FIX; необязательны; `raw_fix` доступен только в REST |

`snapshot` в REST: `id`, `adapter_id`, `pos_req_id`, `clearing_date`, `requested_at`, `completed_at`, `status`, `error`, `report_count`. Содержит служебные данные синхронизации; `completed_at` и `error` необязательны. `snapshot` отсутствует, если снимка нет.

### Установка и удаление позиций

| Метод | Входные данные |
|---|---|
| `PUT /api/v1/positions` | JSON: `client_code`, необязательный `account_code`, массивы `cash` и/или `instruments`; хотя бы один непустой. Клиент и счёт наследуются элементами, если не заданы в них |
| `PUT /api/v1/positions/cash` | JSON: обязательные `client_code`, `currency`, `amount`; необязательные `account_code`, `tag`, `open_bal`, `limit`, `locked` |
| `PUT /api/v1/positions/instruments` | JSON: обязательные `client_code`, `symbol` и `quantity` либо `long_qty` / `short_qty`; необязательные `account_code`, `security_id`, `board`, `currency`, `settl_price` |
| `DELETE /api/v1/positions/cash` | Query: обязательные `client_code`, `currency`; необязательные `account_code`, `tag` |
| `DELETE /api/v1/positions/instruments` | Query: обязательные `client_code`, `symbol`; необязательные `account_code`, `board` |

`firm_id` необязателен для всех операций: по умолчанию используется код брокера; можно указать только свою фирму.

Деньги: `amount` — текущий остаток; `open_bal` по умолчанию равен `amount`; `limit` и `locked` — `0`; `tag` — `EQTV`. Бумаги: `security_id` по умолчанию равен `symbol`; `quantity = long_qty - short_qty`; `long_qty` и `short_qty` ≥ 0. Элементы пакетного запроса используют те же поля.

```json
{
  "client_code": "006",
  "account_code": "1-3101-006",
  "cash": [{"currency": "KGS", "amount": "1500000"}],
  "instruments": [{"symbol": "KG0105166315", "board": "LISTEDSTOCKK", "quantity": "100"}]
}
```

`PositionWriteResult`: `{ok: true, source: "fix", firm_id, adapter_id, pos_req_id, result}`. `result` содержит `source`, `cash`, `instruments` и при наличии `snapshot`; `firm_id` находится на верхнем уровне ответа.

`ok: true` означает принятие изменения QUIK. Обновление снимка выполняется после изменения; если оно не удалось, `result` может содержать прежние данные. Актуальные позиции доступны через GET или следующий gRPC-снимок.

## gRPC: команды и сообщения

Один двунаправленный RPC: `/fixgateway.realtime.v1.RealtimeService/Stream`. Для всех четырёх событий используется этот путь.

Первое сообщение — `subscribe`, в течение 5 секунд после открытия потока:

```json
{"subscribe":{"event_types":["order.updated","trade.created","positions.updated","marketdata.tick"]}}
```

Пустой `event_types` выбирает все события, разрешённые токену. Повторный `subscribe` полностью заменяет список.

| Сообщение | Данные |
|---|---|
| Клиент: `subscribe.event_types` | Массив типов событий |
| Клиент: `ping.id` | ID проверки связи, до 128 байт |
| Сервер: `ready.event_types` | Подтверждённый список подписок |
| Сервер: `event.sequence` | `uint64`, общий счётчик процесса; не курсор возобновления |
| Сервер: `event.type` | Один из четырёх брокерских типов событий |
| Сервер: `event.data` | `google.protobuf.Struct`; поля описаны выше |
| Сервер: `event.occurred_at` | `google.protobuf.Timestamp`, время формирования события |
| Сервер: `pong.id` | ID из `ping`; сервер также отправляет пустой `pong` каждые 20 секунд |

Числа внутри `Struct` представлены как `double`; цены, суммы и количества остаются строками. В примерах используется JSON с protobuf-именами полей; стандартный ProtoJSON также использует `eventTypes` и `occurredAt`, а `sequence` кодирует строкой.

Права и токен перепроверяются каждые 5 секунд. Отзыв, истечение срока, смена прав или фирмы закрывают поток. Для токена без срока одно подключение ограничено 24 часами.

Пропущенные события не воспроизводятся. После переподключения обновите заявки и сделки через REST; позиции и рынок приходят начальными gRPC-снимками. Пропуски `sequence` допустимы из-за фильтрации событий.

Входящее сообщение — до 16 КиБ, исходящее — до 8 МиБ; очередь — 64 события на подписку. Переполнение очереди закрывает поток с `ResourceExhausted`. Создание заявок, отмена и изменение позиций выполняются через REST.

## HMAC для REST и gRPC (необязательно)

По умолчанию `HMAC_REQUIRED=false`: достаточно Bearer-токена. При обязательной подписи передавайте `X-Timestamp`, `X-Nonce`, `X-Signature`; в gRPC — metadata с теми же именами в нижнем регистре. Если передан хотя бы один из них, нужны все три и подпись проверяется.

`timestamp` — Unix time в секундах, допустимое отклонение ±5 минут. `nonce` — уникальная строка 8–128 байт на запрос или открытие потока; повтор отклоняется в течение 10 минут в пределах токена.

```text
payload = timestamp + "\n" + nonce + "\n" + method + "\n" + path + "\n" + body
signature = hex(HMAC-SHA256(API-токен, payload))
```

REST: `method` — HTTP-метод, `path` — точный escaped path с исходной query-строкой, `body` — точные байты тела. gRPC: `method = POST`, `path = /fixgateway.realtime.v1.RealtimeService/Stream`, `body` — пустая строка. Для каждого переподключения используйте новый nonce.

## Ошибки

REST возвращает `{error}`; дополнительно могут быть `details` или `code`.

| HTTP | Значение |
|---|---|
| `400` | Неверный JSON, параметры, неизвестный инструмент или недопустимая отмена; также отказ QUIK при изменении позиции |
| `401` | Нет или недействителен токен; неверная подпись / повтор nonce |
| `403` | Нет права, IP запрещён, требуется HTTPS или указана чужая фирма |
| `404` | GET заявки: заявка отсутствует или принадлежит другому брокеру |
| `422` | Торговое ограничение: `TRADING_PAUSED`, `MAX_ORDER_QUANTITY`, `MAX_OPEN_ORDERS`; заявка не создана |
| `429` | Лимит запросов; пауза указана в `Retry-After`. При ограничении операции также возвращаются `code: "RESOURCE_EXHAUSTED"`, `operation`, `retry_after_ms` |
| `500` | Внутренняя ошибка |
| `502` | Ошибка FIX / QUIK при изменении позиций |
| `503` | Временная недоступность авторизации, хранения, обработки или перегрузка |

Лимиты задаёт оператор для брокера и операций. При повторе создания после сетевой ошибки сохраняйте `client_order_id`. `X-Request-ID` и `X-Trace-ID` — необязательные ID для поиска запроса.

| gRPC status | Значение |
|---|---|
| `Unauthenticated` | Токен, подпись или сессия недействительны |
| `PermissionDenied` | Нет права на событие или тип события неизвестен |
| `InvalidArgument` | Первое сообщение не `subscribe`, команда неизвестна или ID `ping` слишком длинный |
| `DeadlineExceeded` | Нет первой подписки за 5 секунд или клиент не принимает данные |
| `ResourceExhausted` | Лимит подключения, команды, размера сообщения или очереди; при ограничении операции — `RetryInfo` |
| `Unavailable` | Поток, авторизация или снимок позиций временно недоступны |
| `Internal` | Ошибка сериализации позиций |

## Проверка доступности REST

Эти методы не требуют API-токена; доступность внешних путей определяется развёртыванием.

| REST | Назначение | Ответ |
|---|---|---|
| `GET /health` | Проверка процесса | `200`, `{status: "ok"}` |
| `GET /ready` | Проверка БД, доступного FIX-адаптера и обработки исполнений | `200` / `503`; `status` — `ready` / `not_ready`; `database` и `queue` — доступность БД; `outbox_pending` — очередь отправки; `adapters_available` — число доступных адаптеров; `execution_inbox_pending` — очередь исполнений; `execution_inbox_healthy` — её работоспособность |
