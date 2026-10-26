# Эксплуатация FIX_GateWay в production

Этот runbook относится к `deploy/docker-compose.production.yml`. Основной и local Compose используют development-настройки и симулятор; для реальной торговли их не применять. Поддерживается **один активный gateway** с локальным устойчивым FIX volume. Несколько торговых реплик и автоматический failover требуют отдельной реализации fencing и приёмки.

## Условия допуска к реальной торговле

1. Получить от ARQA утверждённый FIX dictionary, адреса и расписание, правила ExecID/TradeDate, replay и sequence reset, Logon authentication, Reject/BusinessMessageReject, cancel/reject, correction/bust. Выполнить [UAT](arqa-uat-plan.md) и сохранить подписанное свидетельство. Файл `uat_evidence` — ссылка на это свидетельство, а не автоматическая проверка подлинности или замена UAT.
2. Назначить владельцев торговли, БД, PKI, уведомлений и восстановления. Подготовить сертификаты и независимое хранение ключа расшифрования backup.
3. Проверить нагрузку и длительную работу на согласованных ресурсах, определить TPS, задержку, RPO/RTO. Локальный симулятор не подтверждает параметры площадки.
4. Проверить внешний периметр и образы выбранным OCI-сканером. `npm audit` и `govulncheck` не сканируют ОС контейнеров.
5. Получить настоящее тестовое уведомление у дежурного и выполнить восстановление на отдельном узле из off-host копии. Подтвердить время восстановления и сверку с площадкой.

## Подготовка файлов и прав

Использовать приватные операторские каталоги вне Git. В репозитории `deploy/production/secrets`, `pki`, `fix` и production `.env` игнорируются, но `.gitignore` не является контролем доступа. Не записывать секреты в командную строку, Git, логи или ticket.

| Каталог | Файлы / требования |
|---|---|
| `GATEWAY_SECRETS_DIR` | `database-url`, `quik-export-database-url`, `jwt-secret`, `api-token-pepper`, `admin-password`; по одному значению в файле, без внутреннего перевода строки. `quik-export-database-url` — PostgreSQL URL источника текущих заявок `quik_export2` под ролью только с `SELECT` на `public."Orders"` и `public."Params"` (PostgreSQL QUIK на `172.23.10.84`); без него запуск завершается ошибкой. JWT/pepper ≥32 байт; случайные независимые значения. Пароль 12–72 байта. |
| `GATEWAY_PKI_DIR` | `database-ca.crt`, `server.crt`, `server.key`, `ca.crt`. gRPC server SAN совпадает с `GRPC_SERVER_NAME`; `ca.crt` проверяет сервер во внутреннем bridge. При включённом mTLS также нужны `bridge.crt` / `bridge.key` с clientAuth. |
| `EDGE_TLS_DIR` | `tls.crt`, `tls.key` для публичного HTTPS, включая нужную цепочку. |
| `FIX_CONFIG_DIR` | `transport.json`, venue dictionary, client certificate/key, venue CA, реальное свидетельство UAT. Начать с `deploy/production/fix-transport.example.json`; ключи объекта — реальные ID адаптеров. |
| `BACKUP_STORAGE_DIR` | Приватный постоянный каталог с репликацией на другой узел; запись UID 10001. Отдельная сохранность и мониторинг свободного места обязательны. |

Gateway и backup работают как UID/GID `10001:10001`, frontend Nginx — UID 101. Ключи должны быть читаемы только соответствующими сервисами/оператором. На Linux обычно применяются приватные каталоги `0750`, файлы `0640` с подходящей группой. Проверить реальные права mounts внутри контейнера; Windows ACL и Docker Desktop отличаются. Никогда не делать каталоги ключей world-readable.

Новый named volume создаётся с корректным владельцем. При переносе старого root-owned FIX volume сначала остановить старый gateway, сделать согласованную копию и передать владельца **только этого volume** UID 10001. Не удалять session store и не менять sequence numbers ради успешного старта. Не использовать NFS/сетевой диск для inbox: нужны гарантии exclusive OS lock, atomic rename и fsync локальной файловой системы.

`database-url` содержит PostgreSQL URL с `sslmode=verify-full&sslrootcert=/run/pki/database-ca.crt`. Имя сервера должно совпадать с SAN сертификата. Production отказывается от plaintext, непроверенного TLS, слабых/default секретов и выключенного HTTPS. Пароли в URL должны быть percent-encoded. Один database URL используется приложением и `pg_dump`; доступ ограничить БД gateway.

Брокеры используют VPN + TLS + API-токен для REST и gRPC. По умолчанию `HMAC_REQUIRED=false`, `GRPC_MTLS_REQUIRED=false`. Доступ через VPN ограничивается интерфейсами хоста и firewall; приложение не проверяет наличие туннеля. mTLS и HMAC можно включить отдельно.

`FIX_CREDENTIAL_ENV_FILE` — операторский env-файл с именами credentials, например `ARQA_USERNAME_FILE=/run/fix/username` и `ARQA_PASSWORD_FILE=/run/fix/password`. `UsernameEnv`/`PasswordEnv` конкретного адаптера должны ссылаться на `ARQA_USERNAME`/`ARQA_PASSWORD`. Содержимое credentials не помещается в env-файл. При необходимости отдельный QUIK API указывается явно через `QUIK_API_BASE_URL`; другие проекты не должны обнаруживаться автоматически.

## Production env и первый запуск

Приватный env-файл содержит пути, origin и публичные параметры. Обязательные переменные:

```dotenv
ADMIN_EMAIL=operator@example.org
PUBLIC_ORIGIN=https://gateway.example.org
GRPC_SERVER_NAME=grpc.gateway.example.org
GATEWAY_SECRETS_DIR=/srv/fix-gateway/secrets
GATEWAY_PKI_DIR=/srv/fix-gateway/pki
EDGE_TLS_DIR=/srv/fix-gateway/edge-tls
FIX_CONFIG_DIR=/srv/fix-gateway/fix
FIX_CREDENTIAL_ENV_FILE=/srv/fix-gateway/fix-credentials.env
ALERTMANAGER_CONFIG_FILE=/srv/fix-gateway/alertmanager.yml
BACKUP_STORAGE_DIR=/srv/fix-gateway/backups
BACKUP_AGE_RECIPIENT=age1...replace-with-operator-public-recipient
HTTPS_PORT=443
HTTPS_BIND_IP=127.0.0.1
GRPC_BIND_IP=127.0.0.1
GRPC_PORT=8443
HMAC_REQUIRED=false
GRPC_MTLS_REQUIRED=false
QUIK_FIX_HOST=172.23.10.84
QUIK_API_BASE_URL=http://172.23.10.84:5000
```

Тестовый QUIK расположен на `192.168.0.89`, production QUIK — на `172.23.10.84`. Production Compose задаёт `ADAPTER_HOST_OVERRIDE` из `QUIK_FIX_HOST`, поэтому тестовый host из исторических миграций не используется для FIX-соединений. Порты и CompID берутся из записей адаптеров; подтвердить их по production-контракту, а также TLS identity в `transport.json`. На тестовом стенде используются FIX `15101`/`15102` и QUIK API `5000`; доступность этих портов в production не проверялась.

После миграций и до запуска production gateway применить `deploy/production/quik-endpoint.sql` **только к production БД FIX_GateWay**. Он задаёт production host, выключает simulator-адаптеры, отключает ResetOnLogon и сохраняет production origin QUIK API. Это необходимо и для БД, перенесённой с тестового стенда: сохранённый `gateway_settings.quik_api_base_url` имеет приоритет над `QUIK_API_BASE_URL`. Если production API использует другой порт/HTTPS origin, изменить этот origin в SQL и env согласованно до применения. Скрипт не меняет порты, CompID и credentials и не заменяет подготовку PKI/UAT.

Заменить примеры реальными значениями. Для удалённых брокеров задать `HTTPS_BIND_IP` и `GRPC_BIND_IP` адресом интерфейса хоста, доступным через VPN; loopback из примера допускает только локальные подключения. Firewall разрешает эти порты только VPN-адресам брокеров. Проверить отсутствие конфликта `GATEWAY_SUBNET` (по умолчанию `10.89.0.0/24`); `EDGE_IPV4` должен находиться в этой сети. Gateway доверяет forwarded headers только точному IP edge. Edge сам завершает TLS и перезаписывает X-Forwarded-*; публичные `/health`, `/ready`, `/metrics`, Swagger/OpenAPI закрыты.

```bash
docker compose --env-file /srv/fix-gateway/production.env -p fix-gateway-production -f deploy/docker-compose.production.yml config --quiet
docker compose --env-file /srv/fix-gateway/production.env -p fix-gateway-production -f deploy/docker-compose.production.yml build
docker compose --env-file /srv/fix-gateway/production.env -p fix-gateway-production -f deploy/docker-compose.production.yml up -d
```

Команды выполняются из корня FIX_GateWay. Не объединять production manifest с development overrides. Gateway применяет миграции при старте; перед обновлением заполненной БД обязательно выполнить согласованный checkpoint. Ожидается 27 миграций. Проверить Docker health и **отдельно** внутренний `/ready` на `9080` через `docker compose ... exec -T gateway wget -qO- http://127.0.0.1:9080/ready`. Operations listener доступен только приватной сети; не публиковать его наружу. Production fixture использовала собственные сертификаты только для проверки механики, не для реального выпуска.

`ADMIN_PASSWORD_FILE` создаёт администратора только если он отсутствует. Изменение bootstrap-файла **не ротирует** существующий пароль. Старый `admin123` блокирует production startup; сначала выполнить явную ротацию.

```bash
# /run/secrets/admin-password уже содержит НОВЫЙ приватный пароль
docker compose --env-file /srv/fix-gateway/production.env -p fix-gateway-production -f deploy/docker-compose.production.yml run --rm --no-deps --entrypoint /app/admin-password gateway -new-password-file /run/secrets/admin-password
```

CLI работает только для SUPER_ADMIN, обновляет password hash/version и атомарно пишет `ADMIN_PASSWORD_ROTATED` без пароля в audit log. Старые REST/gRPC sessions отзываются. При переносе старого deployment убедиться, что старый процесс остановлен; one-off CLI не запускает торговые сессии. Ротация JWT secret отзывает JWT; смена pepper требует согласованного перевыпуска API-токенов, поскольку старые хеши не восстанавливаются. Текущие реальные credentials пользователя этими исправлениями не менялись.

## Входящий FIX журнал и остановка торговли

Входящие ER сначала записываются в `/app/fix-sessions/inbox` (fsync + atomic rename); только затем завершается callback FIX. Применение к БД повторяется и receipt коммитится вместе с order/trade. Дубликат после crash между commit и удалением journal не создаёт вторую сделку. ExecID учитывается в пределах логической заявки и venue trade date; при отсутствии даты используется original FIX time, затем nullable legacy identity. Время обработки gateway не подменяет дату исполнения.

При отказе БД FIFO-head сохраняется; новые торговые операции/outbox блокируются при ошибке или задержке. `/health` может быть успешным, а `/ready` — 503. При невозможности устойчивой записи gateway немедленно завершает процесс до подтверждения callback: требуется сохранённая FIX sequence и venue replay. Проверка этого replay на реальной площадке входит в UAT.

Ограничения inbox: 1 GiB, 100000 записей, 1 MiB на запись. Заполнение, checksum corruption, неверные файлы и второй владелец завершают/блокируют работу безопасно. Перезапуск не лечит повреждённый журнал. Alertmanager получает критический сигнал. Сначала устранить БД/диск, затем дождаться опустошения очереди и проверить заявки/сделки/лимиты.

Trade corrections/busts/replaces и протокольные rejects, требующие venue semantics, сохраняются как блокирующие события. Не применять их как обычные fills, не удалять journal вручную и не выполнять blind resend.

Session Reject (35=3) без RefMsgType (372) сопоставляется по RefSeqNum (45) с журналом последних 8192 исходящих сообщений адаптера. Отклонение heartbeat, test request, запроса позиций и других неторговых сообщений пишется в лог и не останавливает торговлю. Отклонение D/F/G, а также ссылка на неизвестное сообщение (например, отправленное до перезапуска) по-прежнему блокируют inbox.

### Карантин блокирующего отчёта

Страница админки «Входящие FIX» (`/execution-inbox`, API `GET /api/v1/admin/execution-inbox`) показывает состояние очереди, блокирующий отчёт, текст ошибки, число попыток и исходный FIX. Если ошибка не временная (не отказ БД или диска), OPERATOR, ADMIN или SUPER_ADMIN может отложить отчёт с обязательной причиной (`POST /api/v1/admin/execution-inbox/{id}/quarantine`):

- отчёт **не применяется** к заявкам, сделкам и лимитам и не удаляется: файл переносится в `inbox/quarantine/`, копия отчёта и ошибка сохраняются в `execution_inbox_quarantine` вместе с локальной заявкой (если её удалось найти по ClOrdID/OrigClOrdID/OrderID);
- случай фиксируется до переноса файла; после сбоя между этими шагами отчёт с открытым случаем тоже переносится, а не применяется;
- карантин возможен только для текущего блокирующего отчёта (иначе 409); действие пишется в `audit_logs` (`EXECUTION_INBOX_QUARANTINE`);
- следующие отчёты, в том числе по той же заявке, применяются в обычном порядке. Учитывайте это при сверке: состояние заявки после карантина может не отражать отложенный отчёт.

Порядок действий оператора:

1. Сохранить DB + FIX volume и защищённые логи. Записать ID адаптера, заказа, ExecID/date/ref sequence.
2. Убедиться, что ошибка не временная. Отложить отчёт в карантин с причиной; торговля возобновляется после обработки очереди.
3. Получить подтверждённую историю от ARQA/QUIK и сверить quantities/prices/status/sequence. Для legacy ExecID без даты также проверить неоднозначные повторы.
4. Исправления заявок и сделок выполнять согласованным обработчиком/миграцией с audit trail. Закрыть случай с итогом сверки (`POST /api/v1/admin/execution-inbox/cases/{id}/resolve`, `EXECUTION_INBOX_CASE_RESOLVE`). Закрытие не применяет отчёт повторно. Никогда не включать `ResetOnLogon` для обхода инцидента.

Повреждённый (нечитаемый) файл, обнаруженный во время работы, тоже можно отложить. Повреждённый журнал при запуске по-прежнему останавливает старт: перезапуск его не лечит.

## Экспорты и ограничения хранения

Экспорты находятся в постоянном `/app/fix-sessions/exports`: максимум 2 concurrent jobs, 100000 строк, 128 MiB/job, 1 GiB reservation, 32 файлов, deadline 5 минут, TTL 24 часа. Перегрузка отвечает 429 с Retry-After. Ошибки записи/flush/fsync не приводят к DONE. При рестарте PENDING/RUNNING помечаются FAILED; выгрузку можно повторить. Истёкшие файлы удаляются, job получает EXPIRED. CSV защищает формулы в полях. Отдельно следить за свободным местом всего FIX volume; квота экспорта не резервирует физический диск ОС.

## Наблюдение и доставка уведомлений

Заполнить настоящий `ALERTMANAGER_CONFIG_FILE` по примеру `deploy/production/alertmanager.example.yml`; пример с placeholder нельзя использовать для выпуска. Prometheus и Alertmanager не имеют опубликованных портов. Метрики inbox, доступность gateway/БД/FIX, outbox stall, stale backup и ошибки доставки имеют правила в `gateway.rules.yml`; rate-limit rules также подключены. Локальный тест проверил HTTP delivery только в свой приёмник. Оператор обязан проверить настоящий канал и назначить резервный способ обнаружения отказа самой системы мониторинга.

Настроить инфраструктурные проверки БД, диска, сертификатов и каналов площадки. Правило AllFIXSessionsUnavailable учитывать вместе с утверждённым расписанием: на плановый перерыв площадки назначать согласованный silence. Следить за сроком edge/gRPC/FIX/DB CA и конечных сертификатов; выпускать новые до истечения, проверять SAN и chain, перезапускать сервисы с согласованной остановкой торговли. Логи ограничены 5×20 MiB/container; raw FIX logs выключены, Logon credentials редактируются. Хранить audit logs по утверждённой политике.

## Backup, checkpoint и восстановление

Sidecar каждые 3600 секунд делает `pg_dump --format=custom` и шифрует его age public recipient; private identity в контейнер не передаётся. Публикует файл и `last-success` только после успешного pipeline. Retention — 7 дней; backup health и alert stale >2 часов. Это **копия БД**, она сама по себе не гарантирует согласованность FIX sequence/inbox.

Для полного checkpoint DB + FIX volume использовать операторский scheduler, без Docker socket внутри контейнеров:

```bash
node scripts/backup-production.mjs --env-file /srv/fix-gateway/production.env
```

Команда останавливает gateway, создаёт `checkpoint-TIMESTAMP.dump.age`, `checkpoint-TIMESTAMP.fix.tar.age`, публикует `.sha256` последним и возобновляет gateway в `finally`. Частичные `.pending` не являются backup. Старые завершённые checkpoints удаляются после 7 дней; неудачные pending/orphan файлы проверяет оператор. Запускать только одну задачу checkpoint за раз; обеспечить отсутствие сторонних писателей в БД gateway во время остановки. Расписание согласовать с торговым окном и целевым RPO. Хранить private age identity отдельно, с резервной копией и проверенным доступом дежурного.

Восстановление:

1. Остановить и изолировать старый gateway. Не запускать второй на тех же FIX Sender/Target IDs. Сохранить повреждённое состояние для расследования.
2. Выбрать последний **завершённый полный checkpoint**, проверить оба SHA256; скопировать с независимого узла. Расшифровать в приватном recovery окружении (age identity не передавать приложению).
3. На отдельной БД применить `pg_restore --exit-on-error --no-owner`, восстановить соответствующий FIX tar в новый приватный volume с UID 10001. Не смешивать БД и session store из разных checkpoints.
4. Проверить schema/version, orders/trades/outbox/receipts, inbox checksum и FIX sequences. При остановленном gateway согласовать replay и сверку событий после checkpoint с площадкой. Не менять sequences/выполнять reset самостоятельно.
5. Обновить операторские PKI/secrets/URL при необходимости, запустить один gateway, проверить readiness, обработку inbox, TLS и Bearer-доступ через VPN, настоящий alert channel. Сверить позиции, исполнения, отмены и лимиты перед разрешением новых заявок.
6. Зафиксировать реально измеренные RPO/RTO. Если восстановлена только DB sidecar-копия, восстановление FIX state и replay требуют отдельного плана; такую копию не объявлять автоматически полноценным recovery.

Изолированный тест подтвердил расшифрование и restore БД с 27 миграциями, а также создание полного checkpoint и возобновление gateway. Потеря реального узла, восстановление с внешнего хранилища, venue replay и целевые RPO/RTO ещё требуют эксплуатационной приёмки.

## Обновление и откат

Перед обновлением сделать полный checkpoint, проверить UAT и релизные тесты, остановить старый gateway; сохранить image digests и compose/env без секретных значений в release record. Запустить новую версию с тем же согласованным volume.

Миграция 26 снимает глобальную уникальность ExecID. Down на заполненной БД с допустимыми повторениями ExecID **намеренно откажется** восстанавливать старый constraint. Не удалять сделки для отката. Откат с потерей новых данных допустим только по согласованному recovery плану: восстановить совместимый DB + FIX checkpoint, сверить поздние события с площадкой. Успешный `up → down → up` на пустой тестовой БД не доказывает безопасный destructive rollback production.
