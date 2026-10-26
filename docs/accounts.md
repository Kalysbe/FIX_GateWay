# Торговые счета и фирмы из БД QUIK

Решение пользователя от 26.10.2026: счета и фирмы берутся из таблиц QUIK
`public."Trdaccs"` и `public."Firms"` того же подключения
`QUIK_EXPORT_DATABASE_URL` (только чтение), что `Orders` и `Params`.
Ручное заведение счетов в Gateway убрано.

Используемые колонки (остальные передаются как есть):

- `Trdaccs`: `TrdAccId` — торговый счёт (`account_id` заявки, FIX tag 1),
  `FirmId` — фирма. Также `Type`, `LoadDate`, `Description`, `BankAccId`,
  `Status`, `DepAccId`, `TrdAccType`, `MainTrdAccId`, `FirmUse`,
  `FullCoveredSell`, `BankIdT0`, `BankIdTPlus`, `DepUnitId`, `Flags`.
- `Firms`: `FirmId`, `FirmName`, а также `Exchange`, `Status`, `StatusFlag`.

## Проверка заявки

`account_id` принимается, если в `Trdaccs` есть строка с `TrdAccId` = `account_id`
и `FirmId` = `quik_firm` токена (сравнение после обрезки пробелов, с учётом
регистра). Значение `TrdAccId` из БД уходит в FIX tag 1. Иначе — 400
`account_id … не найден в QUIK Trdaccs для фирмы …` (событие
`ACCOUNT_NOT_ALLOWED`). Недоступность QUIK — 503, заявка не создаётся.
Колонка `Status` пока не проверяется: значения статусов не согласованы.

## Методы

| Метод | Ответ |
|---|---|
| `GET /api/v1/accounts` (брокер) | Строки `Trdaccs` фирмы токена |
| `GET /api/v1/firm` (брокер) | Строка `Firms` фирмы токена или 404 |
| `GET /api/v1/admin/brokers/{id}/accounts` | Строки `Trdaccs` фирм активных токенов брокера; фирмы — в заголовке `X-Firms` |
| `GET /api/v1/admin/firms` | Строки `Firms` |

`quik_firm` при создании и изменении токена должен быть `FirmId` из `Firms`
(в админке — выбор из списка); иначе 400, при недоступности QUIK — 503.
Существующие токены не меняются: фирма, которой нет в `Firms`, отмечается в
форме изменения.

Названия фирм в заявках и сделках берутся из локального кеша `quik_firms`,
который каждые 5 минут заменяется содержимым `Firms`; ошибка чтения
сохраняет прежний кеш. Таблица `broker_accounts` остаётся только как история
старых заявок.
