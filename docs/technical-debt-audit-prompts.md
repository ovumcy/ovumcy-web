# Промпты для полного аудита технического долга Ovumcy

Эти пять промптов рассчитаны на пять отдельных сессий Codex. Все сессии проверяют один и тот же последний запушенный commit текущей ветки, игнорируют локальные modified/untracked-файлы и передают результаты друг другу через `.tmp/technical-debt-audit/`.

Такой подход нужен по двум причинам:

1. аудит не зависит от изменений, которые продолжаются в основной рабочей папке;
2. полный список файлов, находок и доказательств хранится на диске, поэтому контекст одной сессии не обязан вмещать весь аудит.

Первая сессия создаёт неизменяемый snapshot последнего запушенного commit. Сессии 2–5 обязаны использовать только этот snapshot. Не запускайте две разные серии аудита одновременно в одной рабочей папке.

Под «последним запушенным commit» здесь понимается commit, на который указывает настроенный upstream текущей локальной ветки, например `origin/chore/example`. `git fetch` намеренно не выполняется: если непосредственно перед аудитом был выполнен `git push`, remote-tracking ref уже указывает на отправленный commit. Если нужна отдельная проверка актуальности сервера, выполните `git fetch` до запуска первой сессии.

Аудит не может математически доказать отсутствие всех возможных ошибок. Его требование полноты означает другое: каждый tracked-файл получает статус, все обнаруженные находки сохраняются без ограничения top-N, а непроверенные сценарии перечисляются явно.

## Сессия 1 — snapshot, Go и архитектура

Скопируйте весь следующий промпт в первую сессию:

```text
Проведи первую часть полного read-only аудита технического долга Ovumcy:
создай зафиксированный snapshot последнего запушенного commit текущей ветки,
после чего исчерпывающе проверь Go-код и архитектуру.

ОСНОВНЫЕ ОГРАНИЧЕНИЯ

- Не изменяй tracked-файлы основной рабочей папки.
- Игнорируй её локальные modified и untracked-файлы.
- Разрешено создавать только audit artifacts внутри
  `.tmp/technical-debt-audit/`.
- Не удаляй и не исправляй исходный код.
- Не переключай основную рабочую ветку.
- Не делай commit, push, fetch, reset, checkout или stash.

СНАЧАЛА ЗАФИКСИРУЙ ОБЪЕКТ АУДИТА

1. Определи текущую ветку и её configured upstream через Git.
2. Если upstream отсутствует, остановись и попроси пользователя указать
   remote branch. Не подменяй её локальным HEAD.
3. Определи SHA upstream. Это последний запушенный commit, известный локальному Git.
4. Создай `.tmp/technical-debt-audit/target.json`, содержащий:
   - local_branch;
   - upstream;
   - sha;
   - created_at;
   - snapshot_dir.
5. Получи tracked-содержимое этого SHA через `git archive`, а не копированием
   текущей рабочей папки. Распакуй его в
   `.tmp/technical-debt-audit/snapshot/`.
6. Запиши полный `git ls-tree -r --name-only <SHA>` в
   `.tmp/technical-debt-audit/manifest.txt`.
7. Проверь, что snapshot не содержит локальные modified/untracked-файлы.
8. Если target.json или snapshot уже существуют, не перезаписывай их молча.
   Сначала проверь, что SHA и upstream совпадают. При несовпадении остановись.

Все дальнейшие команды выполняй относительно snapshot. Основная рабочая папка
служит только источником Git objects и не анализируется как код.

ПЕРЕД АУДИТОМ

- Прочитай полностью инструкции репозитория из snapshot.
- Прочитай architecture, testing и security invariants.
- Создай:
  - `.tmp/technical-debt-audit/reports/01-go-architecture.md`;
  - `.tmp/technical-debt-audit/findings/01-go.jsonl`;
  - `.tmp/technical-debt-audit/coverage/01-go.tsv`;
  - `.tmp/technical-debt-audit/state/01-go.md`.
- TSV должен иметь столбцы:
  `path`, `status`, `method`, `finding_ids`, `note`.
- Допустимые status:
  `checked-clean`, `checked-findings`, `generated-source-checked`,
  `out-of-scope`, `blocked`.
- JSONL должен содержать по одному JSON-объекту на находку с полями:
  `id`, `classification`, `priority`, `category`, `files`, `evidence`,
  `interest`, `impact`, `effort`, `guard`, `confidence`.

ОХВАТ

Проверь все Go-файлы из manifest:

- `cmd/**/*.go`;
- `internal/**/*.go`;
- `migrations/**/*.go`;
- `scripts/**/*.go`;
- `web/**/*.go`;
- production, tests, fuzzers и benchmarks.

Каждый Go-файл обязан получить строку в coverage TSV. Автоматический анализ
может покрыть файл целиком, но поле method должно указывать конкретный анализатор
или ручную проверку.

ПРОВЕРКИ

1. Построй package/import graph через Go tooling.
2. Проверь архитектурные направления, включая отсутствие прямых зависимостей:
   - api → db;
   - services → api;
   - db → services/api;
   - models → верхние слои.
3. Найди протекание Fiber, HTTP, GORM и transport details в domain logic.
4. Найди бизнес-правила с несколькими источниками истины.
5. Запусти существующие Go tests, staticcheck, golangci-lint и go vet на явных
   package trees проекта, не используя `./...`, если оно захватывает node_modules.
6. Запусти `deadcode` отдельно:
   - для production entrypoints;
   - с `-test` для всех project package trees.
7. Через Go AST/types найди top-level types, constants и variables без ссылок.
8. Найди production API, используемый только тестами.
9. Учти interfaces, reflection, GORM, go:embed, generated code и build tags.
10. Измерь cyclomatic/cognitive complexity, nesting, function length и число
    параметров. Не объявляй размер самостоятельным дефектом.
11. Найди функции с несколькими обязанностями, дублирование validation/error
    mapping и abstractions без реальных потребителей.
12. Проверь тестопригодность: global mutable state, hidden side effects,
    неконтролируемые time/network/filesystem seams.
13. Проверь комментарии и документацию рядом с кодом на расхождение с поведением.

КЛАССИФИКАЦИЯ

- `confirmed-debt`: долг доказан конкретными ссылками и постоянной стоимостью.
- `probable-debt`: доказательства сильные, но возможно намеренное решение.
- `protection-gap`: дефект не найден, но отсутствует важный guard.
- `bug-candidate`: наблюдается потенциально неправильное поведение; не называй
  подтверждённым багом без воспроизведения.
- `false-positive`: подозрение проверено и опровергнуто.

Для каждого пункта укажи точные файлы/строки, доказательство, "процентную ставку"
долга, пример затруднённого будущего изменения, влияние, confidence,
стоимость XS/S/M/L/XL и минимальный автоматический guard.

ТРЕБОВАНИЕ ПОЛНОТЫ И ЗАЩИТА ОТ ЛИМИТА КОНТЕКСТА

- Не ограничивай находки top-N.
- Сохраняй каждую проверенную находку в JSONL и Markdown сразу, а не в конце.
- Обрабатывай файлы пакетами и после каждого пакета обновляй coverage TSV и
  state checkpoint.
- В state checkpoint записывай: последний обработанный manifest path,
  завершённые проверки, незавершённые проверки, команды и следующие действия.
- Не держи полный отчёт только в контексте разговора.
- Если контекст близок к пределу, сначала сохрани checkpoint и продолжи работу
  с файлов. Не публикуй преждевременный финальный вывод.
- Не считай сессию завершённой, пока каждый Go-файл не имеет coverage status,
  а blocked-пункты не объяснены.

ФИНАЛ СЕССИИ

В `01-go-architecture.md` должны быть:

- точные upstream и SHA;
- полный register без ограничения количества;
- полный список false positives;
- полный список protection gaps;
- package dependency map;
- полный список complexity hotspots;
- полный список dead/test-only API;
- каскады возможных упрощений;
- места, которые не следует рефакторить;
- coverage summary и полный перечень blocked/out-of-scope.

В ответе пользователю не вставляй весь отчёт. Сообщи только:
- SHA;
- число проверенных файлов;
- число находок по классификациям/приоритетам;
- абсолютные пути к report, JSONL, coverage и checkpoint;
- завершена ли сессия полностью.
```

## Сессия 2 — данные, миграции и хранение

Скопируйте весь следующий промпт во вторую сессию:

```text
Продолжи полный read-only аудит технического долга Ovumcy. Эта сессия проверяет
данные, migrations, SQLite/Postgres, export/import, deletion и backup.

ИСТОЧНИК И ПРАВИЛА

- Не анализируй код основной рабочей папки.
- Прочитай `.tmp/technical-debt-audit/target.json` и используй только указанный
  там snapshot.
- Проверь наличие snapshot и manifest и соответствие сохранённому SHA.
- Не делай fetch и не пересчитывай "последний commit": объект аудита уже
  зафиксирован первой сессией.
- Не изменяй исходники snapshot или основной рабочей папки.
- Разрешено писать только audit artifacts в `.tmp/technical-debt-audit/` и
  временные test/cache-файлы.
- Ничего не исправляй и не удаляй, особенно historical migrations.

СОЗДАЙ И ВЕДИ

- `reports/02-data-migrations.md`;
- `findings/02-data.jsonl`;
- `coverage/02-data.tsv`;
- `state/02-data.md`.

Используй те же JSONL fields, TSV columns, classifications и priorities, что
первая сессия. Каждую находку и каждый обработанный файл сохраняй сразу.

ОХВАТ

По manifest найди и проверь все файлы, влияющие на хранение данных:

- `internal/db/**`;
- `internal/models/**`;
- data-oriented части `internal/services/**`, `internal/api/**`, `internal/cli/**`;
- `migrations/**` обоих dialects;
- export/import;
- account deletion, clear data, sessions, OIDC, TOTP, webhook/reminder state;
- database config и test database infrastructure;
- связанные Go, E2E, docs и fixtures.

Каждый файл в этой области обязан получить coverage status.

ПРОВЕРКИ

1. Сопоставь полный порядок, имена и назначение SQLite/Postgres migrations.
2. Проверь clean bootstrap, legacy upgrade, repeat boot, idempotency, missing
   schema_migrations record и частично применённое состояние.
3. Сравни NULL, defaults, unique constraints, foreign keys, indexes, column types
   и transaction semantics двух dialects.
4. Не считай существование старой migration долгом. Ищи только конкретный риск
   обновления, расхождения или неподдерживаемой процедуры.
5. Проверь multi-write operations на общую транзакцию и корректный rollback.
6. Проверь commit/rollback errors, lost update, concurrent write и retry behavior.
7. Проверь owner scoping каждого read/update/delete/bulk repository path.
8. Проверь lookup-before-authorization и небезопасные repository API.
9. Построй карту полного удаления персональных данных: delete account, clear data,
   symptoms, logs, sessions, OIDC, TOTP, webhooks, reminder watermarks и tokens.
10. Проверь export → import → export fidelity, duplicates, unknown/old fields,
    max sizes, malformed input, partial failure, timezone и atomicity.
11. Проверь documented SQLite backup/restore и его ограничения во время записи.
12. Найди N+1, unbounded reads, queries без подходящего индекса, per-row writes,
    широкие GORM Save/Updates, долгие transactions и resource leaks.
13. Проверь реалистичный большой dataset безопасными временными тестами или
    benchmarks. Записывай фактические размеры, время и число запросов.
14. Проверь, какие Postgres tests реально выполняются, а какие могут skip из-за
    отсутствия Docker. Skip не считай успешной проверкой.

ТРЕБОВАНИЕ ПОЛНОТЫ И КОНТЕКСТ

- Не ограничивай полный register top-N.
- Приоритетное резюме является дополнением, не заменой.
- Работай пакетами файлов, обновляя report/JSONL/TSV/checkpoint после каждого.
- Если проверку нельзя выполнить, ставь `blocked` и записывай точную причину.
- Не делай вывод "SQLite/Postgres совместимы" только из одинаковых имён migrations.
- Не завершай сессию без coverage строки для каждого файла области.

ФИНАЛ СЕССИИ

Отчёт должен содержать:

- полный register;
- полный dialect parity matrix;
- полный migration scenario matrix;
- карту критических transactions;
- карту owner-scoped queries;
- карту удаления персональных данных;
- export/import/backup guarantees и gaps;
- все performance/resource findings;
- false positives, accepted intentional behavior и blocked checks;
- coverage summary без ограничения числа находок.

В ответе пользователю выведи только SHA, counts, статус полноты и абсолютные
пути к четырём audit artifacts. Полный отчёт оставь в Markdown-файле.
```

## Сессия 3 — frontend, шаблоны, CSS и локализация

Скопируйте весь следующий промпт в третью сессию:

```text
Продолжи полный read-only аудит технического долга Ovumcy. Эта сессия проверяет
frontend, source/build contracts, templates, DOM, CSS, локали и accessibility.

ИСТОЧНИК И ПРАВИЛА

- Прочитай `.tmp/technical-debt-audit/target.json`.
- Используй только зафиксированный snapshot и manifest первой сессии.
- Не анализируй текущие локальные modified/untracked-файлы.
- Не делай fetch и не меняй объект аудита.
- Не изменяй исходники или committed bundles.
- Для проверки rebuild создай отдельную временную копию snapshot и сравни её
  результат с оригиналом по содержимому/hashes.
- Пиши только в `.tmp/technical-debt-audit/`.

СОЗДАЙ И ВЕДИ

- `reports/03-frontend.md`;
- `findings/03-frontend.jsonl`;
- `coverage/03-frontend.tsv`;
- `state/03-frontend.md`.

ОХВАТ

По manifest проверь все:

- `web/src/js/**`;
- `web/static/js/**`;
- source и generated CSS;
- `internal/templates/**`;
- `internal/i18n/locales/**`;
- JS/CSS build scripts;
- package manifests и ESLint/Playwright config;
- frontend unit tests;
- E2E specs/helpers;
- web manifests, PWA и связанные assets;
- Go-код, регистрирующий templates, funcs, embeds и asset routes.

Каждый файл области обязан получить coverage status. Generated bundle считается
проверенным только вместе с его source и build contract.

ПРОВЕРКИ

1. Построй полный JS fragment/build graph: source → builder → bundle → template/embed.
2. Найди source fragments вне build, двойное включение, скрытую зависимость от
   порядка, неявные globals и bundle/source drift.
3. Запусти ESLint, frontend unit tests и существующие build checks в временной
   копии. Отдельно оцени отсутствие/наличие TypeScript type-check.
4. Построй DOM contract graph:
   - querySelector/querySelectorAll;
   - closest/matches;
   - getElementById;
   - getAttribute/setAttribute;
   - dataset;
   - attributes/classes, создаваемые JS;
   - соответствующая template/created markup.
5. Не считай generated JS отдельным потребителем исходного селектора.
6. Найди селекторы без targets, markup hooks без consumers, duplicate IDs и
   хрупкие связи по presentation classes.
7. Проверь async/state paths: double submit, out-of-order responses, stale
   autosave, retry старого payload, navigation, две вкладки, slow/offline network,
   HTMX swaps и повторную регистрацию listeners/timers.
8. Найди global mutable state, функции с несколькими обязанностями и дублирование
   network/validation/error rendering.
9. Разбери все template definitions/invocations и templateFuncMap identifiers.
10. Проверь template.HTML и JSON-in-attribute escaping, empty/nil states,
    full-page/partial parity и dynamic template calls.
11. Построй locale reference graph по всем шести JSON:
    - parity;
    - literal references;
    - plural base keys;
    - dynamic families;
    - отсутствующие и неиспользуемые ключи.
12. Dynamic families подтверждай конкретным producer expression; не создавай
    широкую allowlist без доказательства.
13. Построй CSS usage graph:
    - @utility → exact class tokens;
    - custom property declarations → var/getPropertyValue/setProperty;
    - light/dark declarations;
    - JS-created classes;
    - chart/runtime static-only consumers.
14. Проверь accessibility debt: keyboard, focus, accessible names, labels, ARIA,
    live regions, hidden tree, duplicate IDs, contrast, zoom, 320px viewport и
    длинные переводы.
15. Найди неожиданные console.error/pageerror/unhandled rejection gaps и flaky
    behavior, но не дублируй test/CI debt без frontend-доказательства.

ТРЕБОВАНИЕ ПОЛНОТЫ И КОНТЕКСТ

- Не ограничивай deadrefs или hotspots top-N.
- Сохраняй полные selector/template/locale/CSS inventories как приложения к
  Markdown либо отдельные файлы рядом с report и ссылайся на них.
- После каждой категории обновляй JSONL, coverage и checkpoint.
- Не объявляй отсутствие строки доказательством без проверки dynamic producers.
- При исчерпании контекста продолжай с checkpoint, а не сокращай полный register.
- Не завершай сессию, пока каждый файл области не классифицирован.

ФИНАЛ СЕССИИ

Отчёт должен содержать полные, а не top-N списки:

- JS/build graph;
- DOM contracts и dead selectors/hooks;
- template graph и unused funcs/templates;
- locale parity/reference/dead-key register;
- CSS utility/property register;
- async/state/HTMX debt;
- accessibility gaps;
- source/bundle/dependency findings;
- false positives и dynamic exclusions с доказательствами;
- coverage summary и blocked checks.

В ответе пользователю выведи только SHA, counts, статус полноты и абсолютные
пути к audit artifacts. Не вставляй полный отчёт в чат.
```

## Сессия 4 — тесты, CI, безопасность и эксплуатация

Скопируйте весь следующий промпт в четвёртую сессию:

```text
Продолжи полный read-only аудит технического долга Ovumcy. Эта сессия проверяет
тестовый код, CI/CD, supply chain, Docker, конфигурацию и эксплуатацию.

ИСТОЧНИК И ПРАВИЛА

- Прочитай `.tmp/technical-debt-audit/target.json`.
- Используй только snapshot/manifest первой сессии.
- Не анализируй локальные modified/untracked-файлы основной папки.
- Не делай fetch и не меняй SHA.
- Не изменяй исходники/workflows. Разрешены только audit artifacts и временные
  outputs внутри `.tmp/technical-debt-audit/`.
- Не устанавливай сетевые инструменты без разрешения. Если tool недоступен,
  выполни максимально близкую статическую проверку и отметь blocked remainder.

СОЗДАЙ И ВЕДИ

- `reports/04-tests-ci-operations.md`;
- `findings/04-tests-ci-operations.jsonl`;
- `coverage/04-tests-ci-operations.tsv`;
- `state/04-tests-ci-operations.md`.

ОХВАТ

По manifest проверь:

- все Go/JS/Playwright test files и helpers;
- testdata, fixtures, fuzzers, benchmarks и mutation configuration;
- `.github/workflows/**`, `.github/actions/**`, Dependabot и repository configs;
- Dockerfile, docker-compose и reverse-proxy examples;
- package/go manifests, security scanners и release workflow;
- TESTING, SECURITY, architecture, self-hosting и operational docs;
- config/env/startup/health/readiness/shutdown/background-worker код;
- остальные tracked-файлы, не покрытые естественным образом сессиями 1–3.

Каждый файл области обязан иметь coverage status.

ПРОВЕРКИ ТЕСТОВ

1. Найди vacuous tests, отсутствие positive anchors, assertions, которые не могут
   упасть, и проверки реализации вместо поведения.
2. Найди mocks/fakes, расходящиеся с реальной БД, Fiber, HTTP, clocks или network.
3. Найди shared state, order dependence, resource leaks и скрытый test cache.
4. Проверь `t.Skip`, `test.skip`, retries и условия, при которых важный тест может
   никогда реально не выполняться.
5. Проверь Playwright isolation, retries, flaky classification, browser console
   errors и artifacts первого сбоя.
6. Построй фактическую matrix: OS, architecture, SQLite/Postgres, browsers,
   viewport, themes, locales, build tags, race, fuzz и real image.
7. Запусти безопасный `go test -shuffle=on -count=1` там, где это практически
   возможно; не скрывай timeout/skip как pass.
8. Оцени test duration и дублирование, но не объявляй медленный тест долгом без
   доказанной стоимости или обхода разработчиками.

ПРОВЕРКИ CI И SUPPLY CHAIN

9. Проверь workflow YAML/expressions/shell через actionlint, если доступен.
10. Проверь GitHub Actions security через zizmor, если доступен.
11. Вручную проверь template injection, untrusted event data in shell,
    permissions, credential persistence, cache poisoning и artifact trust.
12. Проверь все `uses:` на immutable SHA и соответствие комментария версии SHA.
13. Проверь условия `if`/`needs`, required-check skip/fail-open, merge_group и
    публикацию только после всех обязательных checks.
14. Проверь, что тестируется тот же source/image, который публикуется.
15. Проверь pinning Go/npm/Docker/tools, Dependabot coverage, vulnerability scans,
    license handling, SBOM, signing и provenance.
16. Не предлагай новый scanner, если он дублирует существующий без нового класса
    сигнала.

ПРОВЕРКИ ЭКСПЛУАТАЦИИ

17. Проверь Docker non-root, scratch assets, filesystem permissions, healthcheck,
    readycheck, graceful shutdown и signals.
18. Проверь env parsing, invalid/contradictory configurations, SECRET_KEY,
    secure cookies, HSTS, proxy/origin и database configuration.
19. Проверь сценарии filled/read-only disk, unavailable/corrupt DB, exhausted
    connections, DNS/TLS failure и partial external response.
20. Проверь background reminders/webhooks на observable failure, idempotency,
    restart behavior и bounded retry.
21. Сопоставь documented backup/restore/update/rollback с реально проверяемыми
    командами и tests.
22. Составь список необходимых production signals: 5xx, latency, readiness,
    scheduler/webhook failures, DB/disk capacity — учитывая запрет PII.

ТРЕБОВАНИЕ ПОЛНОТЫ И КОНТЕКСТ

- Не ограничивай находки top-N и не возвращай только список новых tools.
- Отделяй существующую защиту от настоящего gap.
- Сохраняй каждую находку и coverage сразу.
- В checkpoint фиксируй test/tool commands, exit status, skips и незавершённые
  области.
- Не завершай сессию, пока каждый файл области не классифицирован.

ФИНАЛ СЕССИИ

Отчёт должен содержать:

- полный test-quality register;
- полный skip/flaky/vacuity register;
- фактическую test matrix;
- полный CI workflow/security register;
- supply-chain coverage map;
- Docker/config/operations risk register;
- таблицу существующих checks и уникального сигнала каждого;
- полный список отсутствующих guards;
- проверки, которые добавлять не нужно;
- false positives, blocked tools и coverage summary.

В ответе пользователю выведи только SHA, counts, статус полноты и абсолютные
пути к audit artifacts. Полный отчёт не вставляй в чат.
```

## Сессия 5 — проверка полноты, дедупликация и единый план

Скопируйте весь следующий промпт в пятую сессию. Предыдущие ответы вставлять в чат не нужно: финальная сессия прочитает отчёты с диска.

```text
Заверши полный read-only аудит технического долга Ovumcy. Используй snapshot и
audit artifacts первых четырёх сессий, перепроверь выводы и создай единый
исчерпывающий register без ограничения top-N.

ИСТОЧНИК И ПРАВИЛА

- Прочитай `.tmp/technical-debt-audit/target.json` и manifest.
- Используй только зафиксированный snapshot.
- Не анализируй текущую рабочую папку и не делай fetch.
- Прочитай последовательно, а не обязательно одновременно:
  - `reports/01-go-architecture.md`;
  - `reports/02-data-migrations.md`;
  - `reports/03-frontend.md`;
  - `reports/04-tests-ci-operations.md`;
  - все JSONL findings;
  - все coverage TSV;
  - все state checkpoints.
- Не изменяй код. Разрешено создавать только финальные audit artifacts.

СОЗДАЙ И ВЕДИ

- `reports/05-final.md`;
- `findings/05-master.jsonl`;
- `coverage/05-master.tsv`;
- `state/05-final.md`.

КОНТЕКСТ-БЕЗОПАСНАЯ КОНСОЛИДАЦИЯ

1. Не загружай все четыре больших отчёта в рассуждение одновременно.
2. Обрабатывай один JSONL/report за раз.
3. Для каждой preliminary finding немедленно создай/обнови master record.
4. Храни mapping `source finding ID → master ID → final disposition` на диске.
5. После каждого отчёта сохраняй checkpoint.
6. Final disposition для каждого source ID обязателен:
   - confirmed;
   - probable;
   - protection-gap;
   - accepted-risk;
   - duplicate-of `<master-id>`;
   - false-positive.
7. Нельзя молча потерять preliminary finding.

ПЕРЕПРОВЕРКА

1. По исходникам snapshot перепроверь все P0/P1, bug candidates, data-loss,
   auth/authz, privacy, cascade removal и архитектурные нарушения.
2. Перепроверь конфликтующие утверждения разных сессий.
3. Не повышай protection gap до подтверждённого дефекта без воспроизведения.
4. Не понижай подтверждённую находку только потому, что она неудобна для плана.
5. Удали вкусовые замечания и размер-файла-без-причины.
6. Объедини находки с одной первопричиной, сохранив все affected files/effects.

ПРОВЕРКА ПОЛНОТЫ ВСЕГО РЕПОЗИТОРИЯ

1. Создай master coverage из `manifest.txt` — ровно одна строка на каждый
   tracked path проверяемого commit.
2. Объедини coverage TSV четырёх сессий.
3. Для каждого manifest path укажи:
   - responsible session;
   - status;
   - method;
   - finding IDs;
   - exclusion/generated reason.
4. Найди файлы без coverage, самостоятельно проверь их и добавь результат.
5. `out-of-scope` допустим только с конкретной причиной. Документация,
   конфигурация и assets не могут быть исключены просто потому, что это не код.
6. Generated files должны быть связаны с проверенным source/build contract.
7. Не объявляй аудит завершённым при наличии необъяснённых manifest paths или
   blocked P0/P1 проверок.

ФИНАЛЬНЫЙ REGISTER

Не ограничивай количество. Для каждого master finding укажи:

- ID;
- final disposition;
- classification: bug / confirmed debt / probable debt / protection gap;
- priority P0–P3;
- category;
- все affected files и точные строки;
- доказательство и способ проверки;
- false-positive analysis;
- пользовательское/операционное влияние;
- "процентную ставку" технического долга;
- confidence;
- effort XS/S/M/L/XL;
- зависимости от других изменений;
- минимальный regression test/guard;
- рекомендуемый отдельный PR;
- rollback strategy.

ПЛАН, НЕ СКРЫВАЮЩИЙ НИЗКИЙ ПРИОРИТЕТ

Составь:

1. краткое priority summary;
2. затем полный register всех P0, P1, P2 и P3;
3. полный список accepted risks;
4. полный список false positives;
5. полный список blocked/unverified scenarios;
6. полный список файлов и областей без находок;
7. roadmap маленьких независимых PR для каждой confirmed finding;
8. ordering graph, если один PR зависит от другого;
9. автоматические guards против возврата долга;
10. список мест, которые сейчас не следует рефакторить.

Определи maintainability contract:

- архитектурные границы;
- complexity budget только для нового/изменённого кода;
- zero known flaky tests;
- regression test для каждого исправленного бага;
- документированные и узкие исключения;
- deadcode/deadrefs;
- migration/data invariants;
- проверяемый backup restore;
- production alerts без PII.

Дай доказательную итоговую оценку:

- контролируется ли долг;
- растёт ли он;
- мешает ли разработке сейчас;
- какие риски нельзя откладывать;
- при каких измеримых условиях кодовую базу можно называть хорошо
  поддерживаемой.

ФИНАЛ СЕССИИ

Не вставляй полный отчёт в чат. В ответе пользователю сообщи:

- upstream и SHA;
- число manifest paths и число полностью классифицированных paths;
- число master findings по disposition/priority;
- наличие или отсутствие unexplained/blocked coverage;
- абсолютные пути к final report, master JSONL, master coverage и checkpoint;
- можно ли считать аудит полным по заявленной методике.
```

## Как продолжать сессию, если она остановилась

Если любая из первых четырёх сессий сообщает, что работа не завершена, не начинайте следующую. Отправьте в ту же сессию:

```text
Продолжай с сохранённого state checkpoint. Не повторяй уже обработанные файлы.
Сначала проверь целостность report, JSONL и coverage TSV, затем продолжи с
первого незавершённого manifest path. Не завершай сессию без полного coverage
manifest для своей области.
```

Если остановилась пятая сессия:

```text
Продолжай финальную консолидацию с `state/05-final.md`. Не загружай заново все
отчёты и не повторяй уже принятые dispositions. Сначала проверь сохранённый
source-ID mapping и master coverage, затем продолжи с первого необработанного
ID или manifest path.
```

## Что считать результатом

Полный результат находится не в пяти сообщениях чата, а в следующих устойчивых артефактах:

```text
.tmp/technical-debt-audit/
├── target.json
├── manifest.txt
├── snapshot/
├── reports/
│   ├── 01-go-architecture.md
│   ├── 02-data-migrations.md
│   ├── 03-frontend.md
│   ├── 04-tests-ci-operations.md
│   └── 05-final.md
├── findings/
│   ├── 01-go.jsonl
│   ├── 02-data.jsonl
│   ├── 03-frontend.jsonl
│   ├── 04-tests-ci-operations.jsonl
│   └── 05-master.jsonl
├── coverage/
│   ├── 01-go.tsv
│   ├── 02-data.tsv
│   ├── 03-frontend.tsv
│   ├── 04-tests-ci-operations.tsv
│   └── 05-master.tsv
└── state/
    ├── 01-go.md
    ├── 02-data.md
    ├── 03-frontend.md
    ├── 04-tests-ci-operations.md
    └── 05-final.md
```

`reports/05-final.md` — читаемый итог. `05-master.jsonl` — полный машинно-обрабатываемый register. `05-master.tsv` — доказательство файлового охвата. Остальные файлы позволяют продолжить аудит после остановки или проверить, почему конкретная находка попала в итог.
