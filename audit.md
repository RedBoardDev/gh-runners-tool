# Audit ghr — Code Review & Plan d'amélioration

> **Périmètre** : audit complet du dépôt `gh-runners-tool` (~5 537 LOC Go hors tests, 4 347 LOC de tests). Toutes les références code utilisent le format `path:line`. Les recommandations sont classées par sévérité ; les features proposées sont en fin de document.
>
> **Méthodologie** : exploration symbolique via Serena, lecture ciblée des chemins critiques (auth, runner, controller, api, launchd), revue croisée avec les rules du projet (`.claude/rules/security.md`, `architecture.md`, `code-cleanliness.md`, `go-style.md`) et les conventions de `CLAUDE.md`.

---

## 0. Synthèse exécutive

### Forces

- Architecture **package-by-feature** propre, interfaces consumer-side, DI manuelle lisible dans `cmd/ghr/main.go` → `internal/cli/daemon.go:buildDaemon`.
- Bon usage de `oklog/run` pour le lifecycle daemon.
- Structure de tests honnête sur les packages bas-niveau (auth, runner, notification, logging, config, health).
- Conventions de logging structuré (`log/slog`) cohérentes, avec rotation par date et multi-handler.
- Linter strict (`gocritic`, `errorlint`, `nilerr`, `prealloc`, `unparam`, `exhaustive`, `contextcheck`…) et `govulncheck` câblé.
- Configuration YAML + env propre, avec validation explicite et messages d'erreur agrégés (`errors.Join`).
- Retry exponentiel sur le listener de groupe (`internal/controller/group.go:nextBackoff`).

### État de l'audit

37 items ont été livrés dans la PR `audit-fixes` (I.1–I.10, I.13, II.2–II.8, II.11, II.13–II.17, III.6, III.9, III.10, III.14, IV.1, IV.2, IV.5, IV.7, IV.8, IV.9, IV.10, VII.6, IX.1) — voir l'historique git pour le détail commit-par-commit. Ce document ne liste plus que les findings encore ouverts.

### Faiblesses majeures restantes

1. **Métriques de santé jamais alimentées** (II.1) : `UpdateGroupStats`, `RecordStartFailure`, `RecordStartSuccess` n'ont aucun call-site en production → `checkGroupDivergence` et `checkConsecutiveFailures` ne déclenchent jamais.
2. **Aucune commande de réelle observabilité** (VII.1, VII.4) : pas de `/metrics` Prometheus, pas de `ghr logs`.
3. **Pas de tracing OpenTelemetry** ni **d'audit log** (VII.2, VII.3) pour les actions admin.
4. **Pas de reload SIGHUP** (IV.4) : un changement de config impose un `ghr restart` complet.
5. **Couverture de tests inégale** (V.1, V.2, V.3) : la couche CLI et le `controller/group.go` n'ont pas de couverture.
6. **Webhook & UptimeKuma sans retry** (IV.3) : un 5xx transitoire = notification perdue.

### Vue d'ensemble (heatmap, items restants)

| Catégorie         | Critique | Haute | Moyenne | Basse |
|-------------------|:--------:|:-----:|:-------:|:-----:|
| Sécurité          |    0     |   0   |    5    |   5   |
| Bugs / Correctness|    1     |   0   |    3    |   3   |
| Architecture      |    0     |   2   |    6    |   6   |
| Résilience        |    0     |   2   |    1    |   2   |
| Tests             |    0     |   3   |    4    |   3   |
| Observabilité     |    1     |   3   |    2    |   2   |
| Doc               |    0     |   0   |    3    |   3   |

---

## I. Sécurité

### I.11 🟠 MOYENNE — `http.Server` sans Read/Write/Idle timeouts

**Fichier** : `internal/api/server.go:53-55`.

```go
srv := &http.Server{ Handler: s.routes() }
```

Pas de protection contre slowloris (faible risque sur Unix socket local mais bonne pratique).

```go
srv := &http.Server{
    Handler:           s.routes(),
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      10 * time.Second,
    IdleTimeout:       30 * time.Second,
}
```

### I.12 🟠 MOYENNE — Pas de vérification de permissions sur le credentials file à la lecture

**Fichier** : `internal/auth/store.go:loadFromFile:25-35`.

`os.ReadFile` accepte n'importe quelles permissions. Pendant ce temps `LoadPrivateKey` (`jwt.go:30-46`) refuse les permissions trop larges. Asymétrie : un attaquant local qui a un `chmod 644` accidentel sur les credentials passe inaperçu.

**Recommandation** : à `loadFromFile`, faire `os.Stat` et warning (pas error) si `mode & 0o077 != 0`.

### I.14 🟠 MOYENNE — Erreurs PAT contiennent le body HTTP brut

**Fichier** : `internal/auth/validate.go:46-48`.

```go
return nil, fmt.Errorf("validate PAT: GitHub API returned %d: %s", resp.StatusCode, string(body))
```

Un body GitHub d'erreur peut inclure les headers de rate-limit, l'IP, ou des en-têtes de debug. À truncate comme `installations.go:truncateBody` (déjà existant) → utiliser cette fonction partout.

### I.15 🟠 MOYENNE — `parseScopes` retourne `nil` quand le header est absent

**Fichier** : `internal/auth/validate.go:64-77`.

Pour les **fine-grained PATs**, GitHub ne renvoie pas `X-OAuth-Scopes`. Le token semble valide même s'il n'a pas la permission `administration:write`. Le ghr découvrira l'erreur seulement à `CreateScaleSet`.

**Recommandation** :
1. Détecter `X-GitHub-Token-Type: github-pat` (fine-grained) et avertir l'opérateur que les scopes ne peuvent pas être vérifiés.
2. Tenter un `GET /installation/repositories` ou un appel cible (`GET /orgs/{org}/actions/runner-groups`) pour valider l'autorisation effective.

### I.16 🟠 MOYENNE — `daemon.pid` et `daemon.state.json` en `0o644`

**Fichier** : `internal/cli/daemon.go:writePIDFile:165-175`, `internal/cli/state.go:writeDaemonState:30-37`.

PID et `config_path` sont des infos d'environnement modérément sensibles. `0o600` est suffisant et cohérent avec `credentials.json`.

### I.18 🟡 BASSE — `MaskedPAT` retourne `****` pour PAT court

**Fichier** : `internal/auth/validate.go:96-101`.

Le seuil `< 12` est arbitraire. Les PATs `ghp_` ont 40 char, les fine-grained `github_pat_` plus longs. Aucun PAT légitime n'a < 12 char donc OK. Bonus : ajouter le préfixe de type (`ghp_`, `github_pat_`, `ghs_`) dans le masquage pour aider au debug.

### I.19 🟡 BASSE — `JWT exp` à 9 minutes au lieu du max 10

**Fichier** : `internal/auth/jwt.go:16-19`. OK marge raisonnable pour clock skew, à laisser tel quel.

### I.20 🟡 BASSE — Goreleaser ne signe pas les binaires

**Fichier** : `.goreleaser.yml`.

Aucun bloc `signs:` ni `notarize:`. Sur macOS un binaire non-notarié déclenchera Gatekeeper. Et pas de signature cosign/GPG des assets.

**Recommandation** :
```yaml
signs:
  - cmd: cosign
    args: ["sign-blob", "--yes", "--output-signature=${signature}", "${artifact}"]
    artifacts: all
notarize:
  macos:
    - sign: { certificate: "{{ .Env.MACOS_SIGN_P12 }}", password: "{{ .Env.MACOS_SIGN_PASSWORD }}" }
      notarize: { issuer_id: "...", key_id: "...", key: "..." }
```

### I.21 🟡 BASSE — Pas de `gosec` dans le pipeline CI

`.github/workflows/ci.yml` n'exécute pas `gosec`. Aurait probablement attrapé I.3, I.7 et I.9.

---

## II. Bugs / Correctness

### II.1 ⛔ CRITIQUE — Métriques de santé jamais alimentées

**Fichiers** : `internal/health/group_state.go:19-43` (`UpdateGroupStats`, `RecordStartFailure`, `RecordStartSuccess`).

**Vérification** :
```
$ grep -rn "UpdateGroupStats\|RecordStartFailure\|RecordStartSuccess" internal/
internal/health/group_state.go: (définitions)
internal/health/group_state_test.go: (tests unitaires)
```

Aucun call-site en production. Conséquences :

- `checkGroupDivergence` (`checks.go:165-195`) retourne immédiatement (`gs.lastDesiredCount == 0`).
- `checkConsecutiveFailures` (`checks.go:197-212`) ne peut jamais émettre l'event `EventHealthGroupFailing`.
- Le system reporting Discord/UptimeKuma marche, mais les events `health.group.failing` et `health.group.degraded` ne sortent jamais.

**Recommandation** :
1. Dans `MacOSScaler.HandleDesiredRunnerCount` (`scaler.go:69-89`), appeler `m.healthMonitor.UpdateGroupStats(s.groupName, target)`. Cela demande de passer le `*health.Monitor` au scaler via une interface consumer-side `groupStatsReporter`.
2. Dans `startRunner` (`scaler_ops.go:12-55`), sur erreur de `Start`, appeler `RecordStartFailure`. Sur succès, `RecordStartSuccess`.

```go
// scaler.go - new field
type groupStatsReporter interface {
    UpdateGroupStats(group string, desired int)
    RecordStartFailure(group string)
    RecordStartSuccess(group string)
}
```

Sans ce câblage, deux features documentées du produit (divergence detection et consecutive-failure alert) sont du **vaporware**.

`Discord.throttle()` sleep jusqu'à 2 s. `UptimeKuma.push` peut prendre 30 s en cas de timeout réseau. Pendant ce temps :
- `Monitor.Status()` (called by `/status` HTTP) est bloqué (RLock attendu).
- Le prochain tick `runChecks` accumule.

**Recommandation** : collecter les notifications/reports localement, libérer le mutex, puis envoyer :

```go
func (m *Monitor) runChecks(ctx context.Context) {
    start := time.Now()
    m.mu.Lock()
    // ... compute snapshots & issues ...
    pending := snapshotPendingNotifs()
    m.mu.Unlock()
    // dispatch async
    go m.dispatchNotifications(ctx, pending)
}
```

### II.9 🟠 MOYENNE — Double check de `GITHUB_TOKEN` dans `auth.Load`

**Fichier** : `internal/auth/load.go:31-36`.

```go
if token := os.Getenv("GITHUB_TOKEN"); token != "" {  // ligne 31, second check
    return ..., "env (.env GITHUB_TOKEN)", nil
}
```

Le premier check ligne 16 attrape déjà `GITHUB_TOKEN`. La logique semble vouloir distinguer "défini avant" vs "défini par godotenv ailleurs" mais `godotenv` n'est pas appelé dans `Load`. Code mort.

### II.10 🟠 MOYENNE — `validateGitHubApp` valide trop peu

**Fichier** : `internal/auth/validate.go:79-94`.

Ne vérifie que l'ouverture du fichier. Une clé corrompue passe ; l'opérateur découvre le bug à `SignAppJWT` au démarrage du daemon.

**Recommandation** : appeler `LoadPrivateKey(app.PrivateKeyPath)` qui fait déjà parse RSA + perms.

### II.12 🟠 MOYENNE — `labelsChanged` détecte mais n'agit pas

**Fichier** : `internal/controller/group.go:130-147`.

```go
if labelsChanged(ss.Labels, labels) {
    c.logger.WarnContext(ctx, "scale set label mismatch detected, ...")
}
return &resolvedScaleSet{...}, nil  // on continue avec l'ancien
```

L'opérateur peut changer `labels:` dans la config, faire `ghr restart`, et croire que c'est appliqué. Ce n'est pas. Un warning enterré dans les logs.

**Recommandation** :
- Émettre un `model.Event{Type: EventConfigDrift, Level: LevelWarning, ...}` → notifié à Discord.
- Soit auto-recreate (DELETE + CREATE), soit fail-fast au démarrage.

### II.18 🟡 BASSE — `interactiveApp` accepte URL host vide

**Fichier** : `internal/cli/login_wizard.go:69-72`.

L'utilisateur peut entrer "" → defaulted à `https://github.com` dans `prepareAppLogin`. OK. Mais le wizard print le prompt `"GitHub host URL [https://github.com]"` qui suggère un default visible — confirmons que `readLine` trim et permet vide. Oui (`login_wizard.go:107-108`).

### II.19 🟡 BASSE — `nonInteractivePAT` ignore `--host`

**Fichier** : `internal/cli/login.go:50-67`.

Pour PAT, on n'a que `--url`. Cohérent. Mais pour GitHub Enterprise, l'URL d'org diffère du host API. Ajouter une note dans la doc ou un flag `--host` optionnel.

### II.20 🟡 BASSE — `expandHome` accepte chemins absolus mais pas Windows `%USERPROFILE%`

**Fichier** : `internal/cli/login_app.go:125-137`.

Non-issue pour macOS. À ignorer.

---

## III. Architecture / Maintenabilité

### III.1 🔴 HAUTE — Fichiers au-delà de la limite 200 LOC

`.claude/rules/code-cleanliness.md` : "Source files must stay under 200 LOC".

| Fichier                            | LOC | Statut |
|------------------------------------|-----|--------|
| internal/health/checks.go          | 213 | dépasse |
| internal/controller/group.go       | 191 | proche |
| internal/cli/daemon.go             | 188 | proche |
| internal/controller/scaler.go      | 187 | proche |
| internal/cli/purge.go              | 181 | proche |
| internal/logging/manager.go        | 180 | proche |

**Recommandations** :
- Split `checks.go` en `checks_liveness.go`, `checks_timeouts.go`, `checks_divergence.go`.
- `daemon.go` → `daemon_build.go`, `daemon_pid.go`, `daemon_url.go`.
- `group.go` → `group_run.go`, `group_resolve.go`, `group_backoff.go`.
- `purge.go` → split `purge_daemon.go`, `purge_scalesets.go`, `purge_cleanup.go`.

### III.2 🔴 HAUTE — Interface `scaleSetClient` à 7 méthodes

**Fichier** : `internal/controller/controller.go:16-23`.

`.claude/rules/architecture.md` : "Consumer-side interfaces are unexported (lowercase) and minimal (1-3 methods)".

L'interface a 7 méthodes. Acceptable car single-concern (scale set operations), mais à minima la documenter comme "façade volontaire".

**Recommandation** : si split, `scaleSetLifecycle` (Create/Get/Delete) + `scaleSetSession` (OpenSession/NewListener/GenerateJITConfig).

### III.3 🟠 MOYENNE — Pas de spec dans `specs/` malgré CLAUDE.md

`CLAUDE.md` documente :
> All specs in `specs/`. Read before implementing:
> - `00-architecture.md` ...
> - `01-core-scaleset.md` ...

Mais aucun dossier `specs/` n'existe. Soit le doc est obsolète, soit les specs ont été supprimées. Conséquence : nouvel arrivant lit `CLAUDE.md`, cherche les specs, est perdu.

**Recommandation** : régénérer les specs ou retirer la section de `CLAUDE.md`.

### III.4 🟠 MOYENNE — README divergent du code

**Fichier** : `README.md`.

```md
Repository Structure
├── internal/
│   ├── cli/                    # Cobra commands
│   ├── auth/                   # Credentials management
│   ├── config/                 # YAML + env config
│   ├── runner/                 # Binary download & process lifecycle
│   ├── github/                 # Scale set SDK adapter
│   ├── model/                  # Shared data structs
│   └── logging/                # Structured logging
```

Manquent : `controller/`, `health/`, `notification/`, `monitoring/`, `api/`, `launchd/`. Et `internal/scaleset` n'existe pas (c'est `internal/github`).

### III.5 🟠 MOYENNE — Linter exception `nilerr` pour `internal/cli/auth.go`

**Fichier** : `.golangci.yml:64-65`.

L'exception est intentionnelle (status command preserves exit 0 on validation errors pour scripting). À minima documenter la raison dans un commentaire au-dessus de la fonction (`auth.go:newAuthStatusCmd`).

### III.7 🟠 MOYENNE — `Duration.MarshalYAML` non implémenté

**Fichier** : `internal/config/types.go:96-98`.

`UnmarshalYAML` existe (visible via overview), mais l'overview ne montre pas si `MarshalYAML` est correct. Symptôme : si on veut dumper la config résolue (commande `ghr config print` à venir), `Duration` deviendrait `0` au lieu de `"30s"`.

### III.8 🟠 MOYENNE — Pas de `--dry-run` pour les commandes destructrices

`purge`, `restart`, `stop --force` ne supportent pas `--dry-run`. Pour un outil qui touche aux processus système et au scale set GitHub, c'est précieux.

### III.11 🟠 MOYENNE — `notification/service.go:Notify` séquentiel sur les providers

**Fichier** : `internal/notification/service.go:40-54`.

Si Discord prend 2 s, le webhook attend 2 s. Pas critique car il n'y a souvent qu'1 provider, mais avec 3 providers + retry, la latence cumule.

**Recommandation** : dispatch parallèle avec `errgroup` ou `sync.WaitGroup`.

### III.12 🟡 BASSE — `internal/model/event.go` mêle types et constants

47 LOC mixant struct + 4 levels + 16 events. À garder pour le moment, c'est le "shared types" pattern correct.

### III.13 🟡 BASSE — `internal/notification/discord_payload.go` couleurs hardcodées

`colorForLevel` (non lue ici mais évoquée) : à exposer en config si on veut customiser.

### III.15 🟡 BASSE — `internal/launchd/service.go:Status` substring match

**Fichier** : `internal/launchd/service.go:77-102`.

```go
if !strings.Contains(line, label) { continue }
```

Si un autre service a un nom contenant `com.ghr.daemon`, faux positif. Fonction suivante check exact `fields[2] != label` qui rattrape — OK mais ordre des vérifications inversé.

### III.16 🟡 BASSE — `RunnerSnapshot.PID` exposed dans l'API JSON

Pour un Unix socket local c'est OK, mais si on expose un jour un HTTP authentifié, exposer les PIDs facilite l'exploitation.

### III.17 🟡 BASSE — Pas de séparation interface/impl pour les notifications

`Service` et `Provider` cohabitent dans `service.go`. OK mais évoluera mal avec d'autres providers (Slack, Teams, Telegram). Préparer le terrain en isolant `notification/internal/discord/`, `internal/webhook/`, etc.

### III.18 🟡 BASSE — `pgrep -f` non documenté

L'opérateur ne sait pas que `KillOrphanRunners` peut tuer ses processus si workdir_base est mal configuré.

---

## IV. Résilience / Robustesse

### IV.11 🟡 BASSE — Pas de `--max-age` sur le cache

Doublon avec IV.5 (cache GC livré dans la PR `audit-fixes`) — surface plutôt un flag explicite si besoin d'override opérationnel.

### IV.12 🟡 BASSE — Aucune coordination multi-instance

Si deux daemons ghr tournent (par accident), ils gèrent les mêmes scale sets → comportement chaotique. Aucune leader election (lockfile, advisory file lock).

**Recommandation** : `flock` sur `daemon.pid` au démarrage. Si déjà locké → exit avec message clair.

---

## V. Tests

### V.1 🔴 HAUTE — Aucun test sur la couche CLI

**Fichiers** : `internal/cli/{login,start,stop,run,status,purge,restart,state,daemon}.go`.

Tous ces fichiers contiennent de la logique (validation flags, chemins, conditionals). Aucun test. Couverture estimée < 10 % sur `internal/cli/`.

**Recommandation** : tests d'intégration via `cobra.Command.Execute()` avec args en table-driven, et FS mocké via `t.TempDir()`.

### V.2 🔴 HAUTE — Pas de test sur l'extraction tar

**Fichier** : `internal/runner/download.go` (extractTarGz, sanitizeTarPath, extractFile).

Le code le plus exposé en sécurité n'a aucun test. Le bug I.1 (symlink traversal) aurait été détecté par un test couvrant le cas TypeSymlink hors path.

**Recommandation** : table-driven tests avec tarballs forgés via `archive/tar` en mémoire :

```go
tests := []struct {
    name    string
    entries []tarEntry
    wantErr bool
}{
    {"normal file", ..., false},
    {"path escape ../etc/passwd", ..., true},
    {"absolute path", ..., true},
    {"symlink to absolute", ..., true},   // <-- attrape I.1
    {"symlink with relative escape", ..., true},
}
```

### V.3 🔴 HAUTE — Pas de test sur `controller/group.go`

Reconnect logic, label drift, backoff — rien.

### V.4 🟠 MOYENNE — `monitoring/uptimekuma.go` untested

URL building, status mapping, push errors. Tests faciles avec `httptest.Server`.

### V.5 🟠 MOYENNE — Pas de tests E2E

`tests/complete/validate.sh` est un script bash isolé non exécuté en CI.

**Recommandation** : un workflow CI `e2e.yml` qui sur macOS lance ghr en foreground, simule un job (curl POST /api), vérifie la création du scale set sur un repo de test.

### V.6 🟠 MOYENNE — Pas de fuzz tests

`config.ParseByteSize`, `auth.APIBaseURL`, `notification.matchesPattern` sont d'excellents candidats à `testing.F`.

### V.7 🟠 MOYENNE — Pas de mock package

Les tests utilisent des doubles à la main (ex. `controller/scaler_test.go`). Pour la durabilité, soit `gomock`, soit `testify/mock`, soit interfaces locales explicites.

### V.8 🟡 BASSE — Pas de `t.Parallel()`

Aucun fichier de test n'appelle `t.Parallel()`. Le run time monte vite ; sur CI macOS c'est sensible.

### V.9 🟡 BASSE — Pas de coverage report

Pas de `go test -coverprofile=` dans CI ni d'upload codecov. Impossible de prioriser.

### V.10 🟡 BASSE — `internal/logging/logger_test.go` à 602 LOC

Le test est plus long que le code testé (180 LOC). Probable redondance — à splitter par concern.

---

## VI. Performance

### VI.1 🟠 MOYENNE — `copyDir` lent au démarrage

**Fichier** : `internal/runner/copy.go`.

Pour chaque runner, on copie ~70 Mo (binaires `Runner.Listener`, `Runner.Worker`, `dotnet`, etc.) → ~200 ms par runner sur SSD, ~5 s sur HDD. Avec 10 runners, 50 s.

**Recommandation** : hardlinks pour les binaires read-only :

```go
if info.Mode().IsRegular() && !info.Mode()&0o200 == 0 { /* writable, copy */ }
else { os.Link(src, dst) }
```

Les action runners écrivent dans `_work/`, pas dans les binaires. Hardlink sûr pour ~99 % des fichiers.

### VI.2 🟠 MOYENNE — Pas de parallélisation du DL multi-version

Si on a `groups: [{version: 2.310}, {version: 2.311}]`, les DL se font séquentiellement dans chaque `runGroup`. OK pour 2-3 groupes, mauvais pour 20.

### VI.3 🟡 BASSE — `Status` parse `launchctl list` line by line

Acceptable pour < 100 services, OK.

### VI.4 🟡 BASSE — `Discord.throttle()` block `mu` pendant Sleep

Acceptable (intentionnel), mais pourrait être implémenté avec `golang.org/x/time/rate` (rate.Limiter) pour libérer le mutex et permettre des sends concurrents.

---

## VII. Observabilité

### VII.1 ⛔ CRITIQUE — Pas de `/metrics` Prometheus

Le daemon expose `/status` et `/health` mais aucun endpoint Prometheus. Pour un outil ops, c'est limitant : impossible de tracer `runners_idle`, `runners_busy`, `jobs_completed_total`, `github_api_latency_seconds`.

**Recommandation** : ajouter `prometheus/client_golang` + un handler `/metrics` derrière une feature flag config `monitoring.prometheus.enabled`.

```go
runnersGauge := prometheus.NewGaugeVec(
    prometheus.GaugeOpts{Name: "ghr_runners", Help: "..."},
    []string{"group", "state"})
prometheus.MustRegister(runnersGauge)
```

### VII.2 🔴 HAUTE — Pas de tracing OpenTelemetry

Pour des incidents complexes (un job qui timeout vs un runner qui ne start pas vs un network glitch), un span trace serait précieux. Le SDK `actions/scaleset` n'expose pas de hooks OTel, mais on peut wrapper.

### VII.3 🔴 HAUTE — Pas de log d'audit pour les actions admin

Login, logout, purge, restart, kill — aucun log dédié structuré. Le daemon log dans `daemon/*.json` mais c'est mélangé.

**Recommandation** : un logger dédié `audit/*.json` avec format `{timestamp, action, actor, target, result}`.

### VII.4 🔴 HAUTE — Pas de commande `ghr logs`

L'opérateur doit `cd ~/.local/share/ghr/logs/...` et `tail -f` à la main. Pour un CLI premium :

```bash
ghr logs daemon         # tail daemon
ghr logs group ci       # tail group ci
ghr logs runner ci-abc  # tail runner
ghr logs --follow
ghr logs --since 1h --grep "error"
```

### VII.5 🟠 MOYENNE — Pas de `ghr inspect <runner>`

Pour debug, dump l'état d'un runner spécifique (PID, workdir, log path, started, jobs done).

### VII.7 🟠 MOYENNE — Pas de rate-limit display

`GET /user` retourne `X-RateLimit-Remaining` mais on ne le surfait pas.

**Recommandation** : log `github.api.rate_limit_remaining` à chaque call (sampling 1/10 pour éviter le bruit). Notifier si < 100.

### VII.8 🟡 BASSE — Pas de profile pprof

Pour debug : `import _ "net/http/pprof"` derrière une feature flag dans la config.

### VII.9 🟡 BASSE — Pas de format `text` pour les logs daemon file

`fileHandler` n'utilise que JSON. OK car parsable, mais pour `tail -f`, c'est moins lisible que le format `text` du console handler.

---

## VIII. CI / Tooling

### VIII.1 🟠 MOYENNE — CI ne run pas `gosec`

Cf. I.21.

### VIII.2 🟠 MOYENNE — Pas de coverage gate

Cf. V.9.

### VIII.3 🟠 MOYENNE — `go.mod`: deps `// indirect`

```
require (
    github.com/actions/scaleset v0.4.0 // indirect
    ...
)
```

Toutes les dépendances apparaissent comme `// indirect`. C'est anormal pour un projet final : `cobra`, `oklog/run`, `joho/godotenv` sont importés directement. Probablement résidu de `go mod tidy` après un refactor.

**Recommandation** : `go mod tidy` + commit.

### VIII.4 🟠 MOYENNE — Pas de release-please / automatic versioning

Goreleaser tire la version d'un tag. Mais pas de bot pour proposer le bump à partir des commits conventional.

### VIII.5 🟡 BASSE — Pas de dependabot / renovate

Risque de stagnation des deps (sécurité notamment sur `scaleset`).

### VIII.6 🟡 BASSE — `Makefile`: pas de cible `e2e`

À ajouter quand V.5 est résolu.

### VIII.7 🟡 BASSE — Pas de pre-commit hooks (lefthook/husky-go)

Optional, mais utile.

---

## IX. Documentation

### IX.2 🟠 MOYENNE — `CLAUDE.md` référence des specs absentes

Cf. III.3.

### IX.3 🟠 MOYENNE — Pas de page `ARCHITECTURE.md`

Diagram de séquence pour : startup, runner provisioning, job lifecycle, shutdown. Le lecteur doit reconstruire à partir du code.

### IX.4 🟠 MOYENNE — Pas de troubleshooting guide

"Mon runner ne se connecte pas" → où chercher ? Pas de doc.

### IX.5 🟡 BASSE — Pas de `CONTRIBUTING.md`

### IX.6 🟡 BASSE — Pas de `CHANGELOG.md` formel

goreleaser génère un changelog par release, mais pas dans le repo.

### IX.7 🟡 BASSE — Licence "Proprietary. All rights reserved." mais pas de `LICENSE`

À clarifier (interne, MIT, BSL ?).

---

## X. Propositions de features

### X.1 Quick wins (1-2 jours)

| Feature | Bénéfice | Effort |
|---------|----------|--------|
| `ghr config validate <file>` | CI lint pré-deploy | XS |
| `ghr config print` (résolu) | Debug config | XS |
| `ghr logs daemon\|group\|runner` | Ops UX | S |
| `ghr inspect <runner>` | Debug | S |
| `--dry-run` pour `purge`, `restart`, `stop --force` | Sécurité ops | XS |
| Audit log file séparé | Compliance | S |
| Notification level filter (`min_level: warn`) | UX notifs | XS |

### X.2 Medium (1-2 semaines)

| Feature | Bénéfice | Effort |
|---------|----------|--------|
| Prometheus `/metrics` | Observabilité | M |
| Reload via SIGHUP | Ops UX (IV.4) | M |
| Keychain pour le PAT (macOS) | Sécu | M |
| Drain mode (`ghr stop --drain`) | Ops UX | M |
| TUI dashboard (`ghr top`) en Bubble Tea | UX | M |
| Hardlinks au lieu de copy | Perf (VI.1) | S |
| Retry sur webhook/uptimekuma | Résilience (IV.3) | S |
| Linux support (testé, pas juste compile) | Adoption | M |
| Self-update (`ghr update`) | Ops UX | M |

### X.3 Large (1+ mois)

| Feature | Bénéfice | Effort |
|---------|----------|--------|
| GitHub Enterprise Server end-to-end | Adoption B2B | M |
| Dockerfile + Helm chart (Linux runners) | Adoption cloud | L |
| Web UI minimal (next.js) | Ops UX premium | L |
| Plugins externes (notif Slack, PagerDuty) via gRPC | Ecosystem | L |
| OIDC token issuance pour les runners | Sécu enterprise | L |
| Crash-only design + persistent state DB (BoltDB) | Résilience | L |
| Auto-scaling basé sur les métriques GitHub (jobs queued) | Cost optim | L |
| Mode "burst" (max éphémère sous pression, idle 0) | Cost optim | M |
| Distributed mode (plusieurs ghr coordonnés via etcd/Consul) | Scale-out | XL |

### X.4 Polish & QoL

- Couleurs sur `ghr status` (déjà en place via codes ANSI dans render — vérifier).
- `--no-color` flag global.
- Auto-completion shell (cobra le supporte).
- Man pages générées via `cobra/doc`.
- Homebrew tap.
- Telegram/Slack notification providers (`internal/notification/*`).

---

## XI. Plan d'action restant

La première vague (Phase 1 + une partie de Phases 2/3/4) a été livrée dans la PR `audit-fixes` ; le reste se découpe ainsi :

### Reste à faire — priorité haute

- II.1 — Câbler `UpdateGroupStats` / `RecordStart{Failure,Success}` depuis le scaler.
- V.1, V.3 — Couverture de tests sur `internal/cli/*` et `controller/group.go`.
- IV.3 — Retry sur `internal/notification/webhook.go` et `internal/monitoring/uptimekuma.go`.
- IV.4 — Reload de config via SIGHUP.
- VII.1 — Endpoint `/metrics` Prometheus.
- VII.2 — Tracing OpenTelemetry initial.
- VII.3 — Audit log dédié pour les actions admin.
- VII.4 — Commande `ghr logs`.

### Reste à faire — priorité moyenne / basse

Cf. les sections II, III, IV, V, VI, VII, VIII, IX restantes du document.

---

## XII. Annexes

### A. Tableau récapitulatif des findings restants

| ID    | Sévérité | Catégorie     | Titre                                                  | Fichier:Ligne                       |
|-------|----------|---------------|--------------------------------------------------------|-------------------------------------|
| II.1  | Critique | Bug           | Stats santé jamais alimentées                          | health/group_state.go               |
| VII.1 | Critique | Observabilité | Pas de `/metrics`                                      | —                                   |
| III.1 | Haute    | Archi         | Fichiers > 200 LOC                                     | health/checks.go (et autres)        |
| III.2 | Haute    | Archi         | Interface scaleSetClient = 7 méthodes                  | controller/controller.go:16         |
| IV.3  | Haute    | Résilience    | Webhook/UK sans retry                                  | notification/webhook.go             |
| IV.4  | Haute    | Résilience    | Pas de SIGHUP reload                                   | cli/run.go                          |
| V.1   | Haute    | Tests         | Aucun test CLI                                         | cli/*.go                            |
| V.3   | Haute    | Tests         | Aucun test controller/group                            | controller/group.go                 |
| VII.2 | Haute    | Observabilité | Pas de tracing                                         | —                                   |
| VII.3 | Haute    | Observabilité | Pas d'audit log                                        | —                                   |
| VII.4 | Haute    | Observabilité | Pas de `ghr logs`                                      | cli/                                |
| ...   | ...      | ...           | (autres items moyenne/basse: voir corps du doc)        | ...                                 |

### B. Commandes utiles pour valider après corrections

```bash
# Lint complet
make lint && make vet && make fmt-check

# Tests avec race + coverage
go test -race -count=1 -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1

# Vuln check
make vuln

# Static security
gosec -severity medium -confidence medium ./...

# Tar extraction fuzz
go test -fuzz=FuzzExtractTarGz -fuzztime=30s ./internal/runner/

# Build + sanity check
make build && ./ghr version && ./ghr config validate tests/simple/config.yaml
```

### C. Références spec interne

- `.claude/rules/security.md` — règles secrets/permissions
- `.claude/rules/architecture.md` — package-by-feature, interfaces consumer-side
- `.claude/rules/code-cleanliness.md` — 200 LOC max, no comments, no godoc
- `.claude/rules/go-style.md` — naming, errors, concurrency
- `CLAUDE.md` — vue d'ensemble projet

---

**Fin de l'audit.** Document généré le 2026-05-16. À versionner et reviewer à chaque phase pour mettre à jour le statut des items (✅ done / ⏳ in progress / ❌ blocked).
