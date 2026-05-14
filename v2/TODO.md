# ghr v2 — TODO

Audit spec-par-spec du 2026-05-14. Chaque item est lié à une spec et un fichier.

## Critical — Bugs / Manques bloquants

- [ ] #1 `StartedAt` jamais peuplé dans `runner.Process` → health timeout checks skip tout (`controller/scaler_ops.go`, `runner/process.go`)
- [ ] #2 `session.Close()` jamais appelé au shutdown (`controller/group.go`)
- [ ] #3 `SetSystemInfo` jamais appelé au démarrage groupe (`controller/group.go`, `github/client.go`)
- [ ] #4 `CleanupStale` jamais appelé au démarrage daemon (`cli/daemon.go`)
- [ ] #5 `daemon.state.json` jamais écrit ni lu (`cli/run.go`, `cli/daemon.go`)
- [ ] #6 Health: checks groupe absents — divergence, connectivité, failures consécutives (`health/checks.go`)
- [ ] #7 Health: actions correctives absentes — zombie/stuck détectés mais pas tués (`health/checks.go`)
- [ ] #8 Health: idle timeout non vérifié (`health/checks.go`)
- [ ] #9 `min_disk_space` jamais parsé/validé via `ParseByteSize` (`config/validate.go`)
- [ ] #10 Uptime Kuma tokens (`GHR_UPTIME_KUMA_DAEMON_TOKEN`, `_TOKEN_{GROUP}`) jamais résolus (`config/loader.go`)

## Significant — Gaps fonctionnels

- [ ] #11 Status output : pas de tableaux Groups/Runners, pas de Recent Events, `--watch` non implémenté (`cli/status.go`)
- [ ] #12 Purge : n'attend pas les busy runners, flags `--timeout`/`--force` déclarés mais non consommés (`cli/purge.go`)
- [ ] #13 Login interactif non implémenté (`cli/login.go`)
- [ ] #14 Discord: rate limiting non implémenté (queue, debounce, Retry-After) (`notification/discord.go`)
- [ ] #15 Discord: footer + avatar_url absents (`notification/discord.go`)
- [ ] #16 Log cleanup une seule fois au startup, pas quotidiennement (`logging/manager.go`)
- [ ] #17 `HandleJobCompleted` : duration (FinishTime - QueueTime) non loguée (`controller/scaler.go`)
- [ ] #18 Scale set update labels si existant (`controller/group.go`)
- [ ] #19 Event types non définis comme constantes dans model (`model/event.go`)
- [ ] #20 `github/resolve.go` : `ResolveScaleSet` jamais utilisé (code mort)

## Architectural — Déviations acceptées

- [ ] #21 DI wiring dans `cli/daemon.go` au lieu de `cmd/ghr/main.go` — acceptable
- [ ] #22 `RunnerStateProvider`/`Reporter`/`Notifier` exportés dans `health/` — nécessaire pour DI
- [ ] #23 `runnerStarter` interface absente dans controller — utilise `*runner.ProcessManager` concret
- [ ] #24 `scaleSetClient` : signatures adaptées au SDK réel
- [ ] #25 Runner output en raw (Option B) vs JSON wrappé (Option A) — acceptable
- [ ] #26 Console log sans préfixe `[ghr]` — cosmétique

## Polish — Contenu manquant

- [ ] #27 Tests pour `runner/`
- [ ] #28 Tests pour `controller/`
- [ ] #29 Tests pour `health/`
- [ ] #30 Tests pour `api/`
- [ ] #31 Tests pour `launchd/`
- [ ] #32 Tests pour `cli/`
- [ ] #33 `config.example.yaml`
- [ ] #34 `env.example`
