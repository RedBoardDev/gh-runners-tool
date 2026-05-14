# ghr v2 — TODO

Audit spec-par-spec du 2026-05-14. Chaque item est lié à une spec et un fichier.

## Critical — Bugs / Manques bloquants

- [x] #1 `StartedAt` jamais peuplé dans `runner.Process`
- [x] #2 `session.Close()` jamais appelé au shutdown
- [x] #3 `SetSystemInfo` — déjà couvert via SystemInfo dans le constructeur SDK
- [x] #4 `CleanupStale` jamais appelé au démarrage daemon
- [x] #5 `daemon.state.json` jamais écrit ni lu
- [x] #6 Health: checks groupe — divergence, failures consécutives
- [x] #7 Health: actions correctives — RunnerKiller interface
- [x] #8 Health: idle timeout vérifié
- [x] #9 `min_disk_space` validé via `ParseByteSize`
- [x] #10 Uptime Kuma tokens résolus depuis env vars

## Significant — Gaps fonctionnels

- [x] #11 Status output : tableaux Groups/Runners, `--watch` mode
- [x] #12 Purge : attend les busy runners, consomme `--timeout`/`--force`
- [x] #13 Login interactif avec wizard
- [x] #14 Discord: rate limiting (throttle 400ms + Retry-After)
- [x] #15 Discord: footer + avatar_url
- [x] #16 Log cleanup quotidien (5ème acteur oklog/run)
- [x] #17 `HandleJobCompleted` : duration loguée
- [x] #18 Scale set label mismatch warning
- [x] #19 Event types définis comme constantes dans model
- [x] #20 `github/resolve.go` code mort supprimé

## Architectural — Déviations acceptées

- [x] #21 DI wiring dans `cli/daemon.go` — acceptable
- [x] #22 Interfaces exportées dans `health/` — nécessaire pour DI
- [ ] #23 `runnerStarter` interface absente dans controller
- [x] #24 `scaleSetClient` adapté au SDK réel
- [x] #25 Runner output en raw — acceptable
- [x] #26 Console log format slog standard — cosmétique

## Polish — Contenu manquant

- [ ] #27 Tests pour `runner/`
- [ ] #28 Tests pour `controller/`
- [x] #29 Tests pour `health/` (checks_test.go + group_state_test.go)
- [ ] #30 Tests pour `api/`
- [x] #31 Tests pour `launchd/` (plist_test.go + service_test.go)
- [ ] #32 Tests pour `cli/`
- [x] #33 `config.example.yaml`
- [x] #34 `env.example`
