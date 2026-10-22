# Archify

В FIX_GateWay установлен [Archify v3.1.0](https://github.com/tt-a1i/archify/tree/v3.1.0/archify)
как локальный навык Codex: `.agents/skills/archify/`. Пакет взят из тега
`v3.1.0`; лицензия MIT и уведомления сторонних компонентов сохранены внутри него.
Для CLI нужен Node.js 18 или новее. Рендереры и валидаторы уже входят в пакет.

## Использование в Codex

Навык будет доступен со следующего сообщения. Например:

> Используй Archify, чтобы построить интерактивную схему архитектуры FIX_GateWay.
> Покажи путь заявки от брокера через API и FIX-адаптер до QUIK, хранилище
> и поток обновлений клиентам. Исследуй только текущий проект.

Инструкции навыка: [SKILL.md](../.agents/skills/archify/SKILL.md).
По умолчанию исходный JSON и самостоятельный HTML сохраняются в отдельной
папке `.archify/<type>-<slug>-<YYYYMMDD-HHMMSS>/` внутри текущего проекта.
HTML можно открыть в браузере или передать другому человеку.

## Проверка установки

Запускать из корня FIX_GateWay:

```powershell
node .agents/skills/archify/bin/archify.mjs doctor
node .agents/skills/archify/bin/archify.mjs demo output/archify-install-check
node .agents/skills/archify/bin/archify.mjs check output/archify-install-check/archify-demo.html
```

Демонстрация использует встроенный пример веб-приложения. Это проверка
установки; схема FIX_GateWay создаётся отдельным запросом. Каталог `output/`
уже исключён из Git правилами проекта.

Если sandbox Codex возвращает `output/target-indeterminate` с причиной
`candidate-publication-link-failed` и `EPERM`, потребуется выполнить эту же
команду с расширенными правами: Archify использует жёсткие ссылки для безопасной
публикации файла. При проверке установки такая команда успешно создала HTML.

Для собственных диаграмм агент следует процедуре `finalize` из `SKILL.md`,
которая включает валидацию, публикацию, проверку HTML и проверку в браузере.
Для последней нужен доступный Chrome или Chromium.
