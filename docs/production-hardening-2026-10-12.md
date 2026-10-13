# Исправления после production-аудита — 12 октября 2026

**Кодовые дефекты и production-конфигурация из аудита исправлены; выпуск с реальными заявками остаётся условным до ARQA UAT и эксплуатационной приёмки.** Существующий deployment и реальные secrets не обновлялись. Изменения внесены в текущую рабочую копию FIX_GateWay с сохранением ранее существовавших пользовательских изменений; коммит/публикация не выполнялись.

Исходный [аудит](production-readiness-audit-2026-10-12.md) сохранён как исторический снимок. Этот документ описывает результат исправлений; его нельзя читать как подтверждение проведённого реального ARQA UAT. Использованы ECC `security-review`, `production-audit` и `browser-qa`. По явному разрешению пользователя выполнены npm audit и закреплённый govulncheck; исходники/секреты во внешние сканеры не загружались.

## Закрытие находок

| ID | Изменение | Проверка / остаточное условие |
|---|---|---|
| B1 | Устойчивый FIFO execution inbox с fsync/atomic rename/checksum, exclusive ownership и ограничениями; receipt в одной DB-транзакции с order/trade; ошибки применения сохраняют событие и блокируют торговлю; невозможность записи приводит к fail-stop до callback ACK. | Две реальные аварийные пробы: БД остановлена во время fill; во второй gateway убит до возврата БД. После восстановления и повторных рестартов ровно одна сделка. Replay при отказе диска на реальном ARQA требует UAT. |
| B2 | Приватный operations listener 9080 для health/ready/metrics; trading API требует HTTPS. Production Nginx завершает TLS, доверие к forwarded headers ограничено точным IP, probes и Prometheus используют приватный listener. | TLS fixture: HTTPS/HSTS/CSP, auth, закрытые публичные ops/docs, PostgreSQL verify-full и gRPC mTLS — PASS. |
| B3 | Уникальность ExecID по логической заявке и venue date вместо глобального ограничения; original time fallback и nullable legacy identity. Replay receipts предотвращают двойной учёт после crash. | DB-тесты повторов в одной заявке, разных заявках/днях, повторов через другой adapter и commit-before-remove — PASS. Venue identity/date semantics требуют подтверждения ARQA; неоднозначные legacy сообщения блокируются. |
| B4 | Secrets через `_FILE`; production fail-closed для default/слабых secrets, DB TLS, HTTPS/HMAC, gRPC mTLS; независимый production manifest. Явный admin-password CLI с аудитом и отзывом сессий. | Проверки отсутствующих/conflicting файлов, неверной конфигурации и ротации — PASS. Существующие настоящие credentials должен ротировать оператор по runbook. |
| B5 | Реальные Logon 553/554 из приватных файлов, TLS/CA/server-name и validation dictionary; production требует transport config и UAT evidence, ResetOnLogon выключен. Unsupported correction/bust/replace и протокольные rejects сохраняются и останавливают дальнейшую торговлю для сверки. | Кодовые опасные defaults устранены. **Реальный UAT не проведён**, venue-specific accounting corrections/rejects не выдуман; эта часть выпуска остаётся блокером. |
| H1 | PUT в CORS preflight. | Regression-тест PASS. |
| H2 | CI создаёт отдельную `rate_limit_test` БД и задаёт RATE_LIMIT_TEST_DATABASE_URL; fault test включён в trading release job, добавлены оба vulnerability scanner. | Полный локальный integration run: 306 PASS-записей, включая подтесты; 0 FAIL, 0 SKIP. CI workflow изменён; удалённый GitHub Actions запуск не выполнялся. |
| H3 | Поддерживаемые Go/Alpine/Node/Nginx/Postgres/Prometheus/Alertmanager с digest, non-root, readonly rootfs, cap drop, no-new-privileges, лимиты ресурсов и логов. | Свежие образы собраны, изолированные dev/TLS stacks запущены. **OCI/SBOM scan не выполнялся**; npm/Go scans его не заменяют. |
| H4 | Async exports ограничены по concurrency, времени, строкам, файлам и объёму; persistent directory, TTL, recovery FAILED/EXPIRED, контроль Write/Flush/Sync/Close, CSV formula protection. | Тесты лимитов, искусственных write errors, restart/TTL и корректного DONE-файла — PASS. Физическое заполнение production диска не моделировалось. |
| H5 | Поддерживается явно одна активная торговая реплика; single container identity и OS-lock устойчивого volume. HMAC nonce хранится в PostgreSQL с атомарным claim/TTL, DB failure закрывает доступ. | Тесты второго владельца, restart, конкурентных nonce claims и unavailable DB — PASS. Distributed rate budgets/realtime и автоматический HA failover не реализованы; scaling запрещён моделью deployment. |
| H6 | Prometheus связан с Alertmanager; правила для недоступности gateway/БД/FIX, outbox stall, inbox, persistence, stale backup и delivery failure. Шифрованный hourly DB backup с retention/health, отдельный полный DB+FIX checkpoint и recovery runbook. | Уведомление доставлено в свой локальный приёмник; реальные отказы БД/FIX меняют availability gauges; promtool подтвердил 8 правил; encrypted DB restore и полный checkpoint с restart — PASS. Настоящий канал дежурного/off-host schedule и измеренные RPO/RTO требуют оператора. |

Дополнительно исправлены DB outage semantics авторизации (503/Unavailable вместо неверного пароля/отзыва сессии), nullable scan async export owner, редактирование Logon credentials в логах, экспортные формулы и браузерные проблемы контраста/названий элементов/области нажатия.

## Проверки и доказательства

Артефакты: [output/hardening-2026-10-12/README.md](../output/hardening-2026-10-12/README.md). Все динамические аварийные действия выполнялись только в наших disposable проектах `fix-gateway-verification` и `fix-gateway-production-verification`; другие проекты и существующий пользовательский stack не менялись.

| Проверка | Результат |
|---|---|
| Backend | `go test -race -tags integration -count=1 -json ./...` на финальном Go 1.26.9: 306 PASS, 0 FAIL, 0 SKIP. `go vet -tags integration ./...`, `go mod verify` — PASS. После добавления audit trail ротации и operations gauges — дополнительные race integration admin/API/metrics + vet CLI/gateway PASS. |
| Зависимости | `govulncheck@v1.9.0` после Go 1.26.9 / x-net 0.60.0: 0 достижимых и 0 imported-package findings; module-only OpenPGP advisory описана ниже. `npm audit --audit-level=moderate` обоих UI: 0 vulnerabilities после обновления CSS build chain и Tailwind 4. |
| UI / supervisor | Admin UI 44 теста, broker UI 4, supervisor 3 — PASS; TypeScript/Vite/Docker builds — PASS. |
| Миграции | Все 27 `up → down → up` на отдельной чистой БД — PASS. Down на заполненной БД не объявляется безопасным. |
| Торговый релиз | Socket loss, late confirmation, reconciliation, одна wire-заявка, policy history, backup/restore и persistent FIX volume — PASS. |
| Исполнения при отказах | DB outage во время fill; DB outage + SIGKILL до восстановления; два повторных рестарта без double trade — PASS. |
| Production transport | TLS PostgreSQL подтверждён через pg_stat_ssl; HTTPS и gRPC client-certificate enforcement, приватность ops/docs — PASS. |
| Operations failure probes | Availability gauges переходят в 0 при остановке своих PostgreSQL/FIX и возвращаются после restart; 8 production rules проверены promtool — PASS. |
| Эксплуатация | Alertmanager → локальный sink; encrypted dump → восстановленная БД (27 migrations); полный DB/FIX checkpoint → gateway resumed — PASS. |
| Браузер | 15 маршрутов, загрузка font assets, адаптивный dashboard 375/768/1440 px. Axe findings исправлены и повторно проверены; итоговые детали в browser-qa.json. |

Первоначальный Go 1.26.7/x-net 0.58 scan содержал 12 уникальных достижимых advisories (848 symbol traces). Успешный JSON exit ошибочно был принят за отсутствие уязвимостей; вывод исправлен, история сохранена. Go обновлён до 1.26.9 с digest в Dockerfile, x/net до 0.60.0; согласованные crypto/sys/sync/text версии обновлены через go get/tidy. Обычный scanner с exit status и отдельно разобранный JSON подтвердили 0 достижимых и 0 imported-package findings. [Официальная документация govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck#hdr-Exit_codes) объясняет, что JSON mode возвращает exit 0 независимо от наличия уязвимостей. CI использует обычный режим.

Остаётся module-only [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) об устаревшем OpenPGP в x/crypto; исправленной версии нет. Приложение использует bcrypt и не импортирует уязвимый OpenPGP. `go list -deps ./cmd/gateway` подтверждает отсутствие OpenPGP в compiled dependency graph. Заявлять ноль advisories во всех требуемых модулях нельзя. Доказательства: govulncheck-patched.txt/json/summary.json и gateway-compiled-deps.txt.

В первом комбинированном запуске scanner также не достиг advisory DB из intentionally internal Docker network; ошибка сохранена в backend-final.stderr.log. Финальные scans выполнены отдельно с egress. Полный backend race/integration/vet/mod verify повторён успешно после Go security patches: backend-patched.jsonl, 306 PASS, 0 FAIL, 0 SKIP. Торговый релиз и DB-outage/SIGKILL сценарии также повторены на Go 1.26.9: trading-release-patched.log и execution-recovery-patched.log.

Browser QA не является сертификатом WCAG: axe имеет incomplete checks, screen-reader acceptance не проводилась. Visual regression — **INCONCLUSIVE**, поскольку утверждённого baseline нет. Нагрузочные параметры реального production периметра и длительный soak в этом исправлении не подтверждались.

Поддержка веток/версии сверялись с первичными источниками: [Alpine releases](https://www.alpinelinux.org/releases/), [Nginx downloads](https://nginx.org/en/download.html), [Prometheus downloads](https://prometheus.io/download/). CSS build chain обновлён по [Tailwind upgrade guide](https://tailwindcss.com/docs/upgrade-guide). Версии и digests зафиксированы в Dockerfiles/Compose; результаты проверки относятся к текущей сборке и базе advisories, не к будущим версиям.

## Что требуется для выпуска

После проверок оба созданных тестовых stacks и их volumes удалены. Временные инструментальные/verification images и private fixture с тестовыми ключами удалены; доказательства сохранены. Существующий пользовательский deployment не обновлялся.

Следовать [production operations runbook](production-operations.md). Обязательны: реальный ARQA UAT с identity/replay/correction semantics, операторские TLS/secrets и настоящая ротация, проверенная доставка дежурному, независимое backup storage/schedule и recovery drill, capacity/soak и сканирование OCI/внешнего периметра. До этого не разрешать реальные заявки. Автоматический multi-replica HA не входит в подтверждённую конфигурацию.
