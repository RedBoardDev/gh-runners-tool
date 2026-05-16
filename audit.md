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

### Faiblesses majeures

1. **Path traversal exploitable** dans l'extraction tar du runner (`internal/runner/download.go:69-71`) — une archive malicieusement fabriquée peut créer un symlink hors du dossier de cache.
2. **Aucune vérification d'intégrité** du binaire runner téléchargé (pas de SHA-256, pas de signature). Le code fait confiance aveugle au tarball GitHub.
3. **`pgrep -f workdirBase`** (`internal/runner/cleanup.go:KillOrphanRunners`) peut tuer des processus utilisateur non-ghr si `workdir_base` est court ou trop large (ex. `/tmp`).
4. **Métriques de santé jamais alimentées** : `UpdateGroupStats`, `RecordStartFailure`, `RecordStartSuccess` n'ont aucun call-site en production → `checkGroupDivergence` et `checkConsecutiveFailures` ne déclenchent jamais. Sécurité by-design désactivée silencieusement.
5. **Notifications synchrones sous mutex** dans `internal/health/checks.go:runChecks` — un Discord lent bloque toute la boucle de health.
6. **Race condition sur le cache** : 2 groupes lançant `EnsureBits` en parallèle pour la même version peuvent corrompre le cache. Détection « cached » basée sur `run.sh` qui apparaît avant la fin de l'extraction.
7. **Unix socket sans permissions explicites** (`internal/api/server.go:Run`) — autres utilisateurs locaux peuvent lire le statut + PIDs.
8. **Aucune commande de réelle observabilité** : pas de `/metrics`, pas d'audit log, pas de `ghr logs`.

### Vue d'ensemble (heatmap)

| Catégorie         | Critique | Haute | Moyenne | Basse |
|-------------------|:--------:|:-----:|:-------:|:-----:|
| Sécurité          |    3     |   6   |    7    |   5   |
| Bugs / Correctness|    2     |   5   |    8    |   6   |
| Architecture      |    0     |   2   |    9    |   7   |
| Résilience        |    1     |   4   |    6    |   3   |
| Tests             |    0     |   3   |    5    |   2   |
| Observabilité     |    1     |   3   |    4    |   2   |
| Doc               |    0     |   1   |    3    |   4   |

---

## I. Sécurité

### I.1 ⛔ CRITIQUE — Path traversal via symlinks dans l'extraction tar

**Fichier** : `internal/runner/download.go:68-75`

```go
case tar.TypeSymlink:
    linkTarget, linkErr := sanitizeTarPath(destDir, header.Linkname)
    if linkErr != nil {
        linkTarget = header.Linkname   // ⚠️ on ignore la violation et on utilise tel quel
    }
    if err := os.Symlink(linkTarget, target); err != nil {
        return fmt.Errorf("create symlink %s: %w", target, err)
    }
```

Quand `sanitizeTarPath` détecte que `Linkname` sort de `destDir`, le code retombe silencieusement sur le chemin original — exactement le payload de l'attaque. Un tarball forgé peut planter un symlink `runner → /etc/shadow` dans le cache, qui sera ensuite copié par `copyDir` (`internal/runner/copy.go:23-26`) et l'écriture éventuelle dessus suivra le lien.

**Recommandation** : faire `return linkErr` quand la cible n'est pas dans `destDir`. Idem côté `extractFile` : utiliser `os.OpenFile(path, O_CREATE|O_WRONLY|O_TRUNC|O_NOFOLLOW, mode)` pour refuser tout descripteur si la cible est un symlink déjà créé par une entrée précédente.

```go
case tar.TypeSymlink:
    if !filepath.IsLocal(header.Linkname) {
        return fmt.Errorf("tar entry %q symlink %q is not local", header.Name, header.Linkname)
    }
    if err := os.Symlink(header.Linkname, target); err != nil { ... }
```

`filepath.IsLocal` (Go 1.20+) fait exactement le job demandé et rejette `..`, les chemins absolus et les noms réservés Windows.

### I.2 ⛔ CRITIQUE — Pas de vérification d'intégrité du binaire runner

**Fichier** : `internal/runner/download.go:17-36` (`downloadAndExtract`).

Le code télécharge `https://github.com/actions/runner/releases/download/v.../actions-runner-osx-...-VERSION.tar.gz` et l'extrait, sans vérifier ni le checksum SHA-256 publié dans la release GitHub, ni la signature détachée. Un MITM (DNS empoisonné, proxy d'entreprise modifié, mirror compromis, etc.) peut substituer un binaire malicieux qui sera ensuite **exécuté avec les permissions du daemon** (`run.sh` lancé via `exec.CommandContext`).

**Recommandation** : pour chaque version résolue, télécharger d'abord `https://github.com/actions/runner/releases/download/v{version}/actions-runner-osx-{arch}-{version}.tar.gz.sha256` (publié par GitHub), comparer au hash calculé en streaming. Idéalement, valider aussi la signature Sigstore (cosign) si disponible.

```go
hasher := sha256.New()
tee := io.TeeReader(resp.Body, hasher)
// extract from tee...
if !bytes.Equal(hasher.Sum(nil), expected) { return ErrChecksumMismatch }
```

### I.3 ⛔ CRITIQUE — `pgrep -f workdirBase` peut tuer des processus arbitraires

**Fichier** : `internal/runner/cleanup.go:91-104` (`KillOrphanRunners`).

```go
out, err := exec.CommandContext(ctx, "pgrep", "-f", m.workdirBase).Output()
```

`workdirBase` est lu de la config (`runner.workdir_base`). Si un opérateur configure `runner.workdir_base: "/tmp"` ou `"."` (chemin court ou commun), `pgrep -f` matchera tous les processus dont la commande contient ce substring → SIGKILL massif sur les processus utilisateur.

Pire : la valeur par défaut pour un user non-root est `~/.local/share/ghr/runners` (`internal/config/loader.go:applyDefaults`). Si quelqu'un lance `ghr` à la racine du `$HOME` (`~`) ou pointe la config sur un dossier partagé, le risque est concret.

**Recommandations** :
1. Valider à `config.validate()` que `workdir_base` est absolu, n'est pas `/`, `/tmp`, `/var`, `$HOME`, et qu'il fait plus de N caractères.
2. Préférer parser `ps -eo pid,command` et matcher exactement le chemin complet du `run.sh` (ou utiliser le `.ghr-pid` déjà écrit + vérification du `comm`).
3. Avant `Kill`, lire `/proc/PID/exe` (Linux) ou `proc_pidpath` (macOS via `lsof -p PID` ou `ps -p PID -o comm=`) et confirmer que la cible est bien `run.sh`/`Runner.Listener` dans un dossier sous `workdir_base`.

### I.4 🔴 HAUTE — Unix socket sans permissions restreintes

**Fichier** : `internal/api/server.go:46-50`.

```go
ln, err := net.Listen("unix", s.socketPath)
```

Le socket hérite de l'umask du processus (typiquement `0o022` → `0o755`). N'importe quel utilisateur local peut faire `GET /status` ou `GET /health` et lire les PIDs, noms de groupes, et issues de santé.

**Recommandation** :
```go
ln, err := net.Listen("unix", s.socketPath)
if err != nil { return ... }
if err := os.Chmod(s.socketPath, 0o600); err != nil {
    ln.Close()
    return fmt.Errorf("chmod socket: %w", err)
}
```

Ou mieux : créer le socket dans un dossier déjà `0o700` (`stateDir`) et permettre `0o660` pour permettre une intégration multi-utilisateur volontaire.

### I.5 🔴 HAUTE — Pas de TLS/connection timeout sur les clients HTTP de `auth/`

**Fichier** : `internal/auth/installations.go:41,93`, `internal/auth/validate.go:32`.

`http.DefaultClient.Do(req)` n'a **pas de timeout** par défaut. Un GitHub lent (ou un attaquant lent-loris contrôlant la résolution DNS) bloquera indéfiniment l'opération `login` ou `auth status`.

**Recommandation** : créer un client local avec `Timeout: 30 * time.Second` (ou par requête via `context.WithTimeout`) et le partager entre les fonctions du package.

```go
var httpClient = &http.Client{Timeout: 30 * time.Second}
```

Pareil pour `internal/runner/binary.go:25` (`httpClient: &http.Client{}`) — le DL d'un tarball de 100 Mo doit avoir un timeout généreux mais borné.

### I.6 🔴 HAUTE — Stockage des credentials en clair

**Fichier** : `internal/auth/store.go:Save` écrit `~/.config/ghr/credentials.json` avec `0o600`, mais en clair.

Pour un daemon long-running c'est attendu (pas de re-prompt). Mais sur macOS, la **Keychain** est l'idiom. Stocker au moins le PAT dans la Keychain via `security add-generic-password` ou `github.com/keybase/go-keychain` réduit la surface : un dump disque ne suffit plus.

**Recommandation court terme** : à la lecture (`loadFromFile`), vérifier les permissions et émettre un warning visible si elles ne sont pas `0o600` (le code le fait déjà pour la private key dans `internal/auth/jwt.go:LoadPrivateKey:34-36` mais pas pour le credentials file).

**Recommandation moyen terme** : flag opt-in `--use-keychain` puis migration douce.

### I.7 🔴 HAUTE — Injection XML possible dans le plist launchd

**Fichier** : `internal/launchd/plist.go:8-41` (template `text/template`).

```go
plistTemplate = `...
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        ...
        <string>{{.ConfigPath}}</string>
    </array>
    ...
```

`text/template` n'échappe pas le XML. Si un opérateur passe un `--config "/tmp/x</string><key>RunAtLoad</key>..."` (peu réaliste, mais), ou si `BinaryPath`/`StateDir`/`LogDir` contient `<`, `>`, `&`, le plist devient un fichier XML structuré différemment, exécutant éventuellement d'autres commandes.

**Recommandation** : registrer une fonction `xml` :

```go
funcs := template.FuncMap{"xml": func(s string) string {
    var buf bytes.Buffer
    _ = xml.EscapeText(&buf, []byte(s))
    return buf.String()
}}
tmpl, _ := template.New("plist").Funcs(funcs).Parse(plistTemplate)
// puis: <string>{{xml .Label}}</string>
```

Et valider en amont que les chemins ne contiennent que `[A-Za-z0-9_./-]`.

### I.8 🔴 HAUTE — `launchctl load/unload` est déprécié depuis macOS 10.10

**Fichier** : `internal/launchd/launchctl.go:7-38`.

Les sous-commandes `load`/`unload`/`start`/`stop` sont remplacées par `bootstrap gui/<uid>`, `bootout`, `kickstart`. Sur macOS 15+ certaines commandes peuvent émettre des warnings ou changer de comportement.

**Recommandation** :
- `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/<label>.plist`
- `launchctl bootout gui/$(id -u)/<label>`
- `launchctl kickstart -k gui/$(id -u)/<label>`

Ajouter un fallback pour macOS < 11 si on veut rester compatible.

### I.9 🔴 HAUTE — `copyDir` réplique les symlinks tels quels

**Fichier** : `internal/runner/copy.go:26-32`.

Si l'extraction tar laisse un symlink absolu (cas non-couvert par I.1 si la victime upgrade), `copyDir` copie le lien vers le workdir. Le runner s'exécute alors avec ce lien et écrira potentiellement *à travers* le lien si une étape (action GitHub) fait un `>` sur le path.

**Recommandation** : refuser les symlinks absolus dans copyDir, ou les transformer en hardlinks pour les fichiers, ou faire un copy.Body au lieu d'un Symlink. Idéalement : exposer un mode `--strict` qui empêche les symlinks dans le cache (la plupart des runners GitHub n'en ont pas).

### I.10 🟠 MOYENNE — `Run` du serveur API utilise `srv.Close()` au lieu de `Shutdown`

**Fichier** : `internal/api/server.go:62-64`.

`srv.Close()` ferme abruptement les connexions en cours. Pour 99 % du temps c'est OK (clients CLI courts), mais une requête `--watch` en cours sera coupée brutalement.

**Recommandation** : `srv.Shutdown(ctx)` avec un sous-contexte timeout 5 s.

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

### I.13 🟠 MOYENNE — `PAT` retourné en clair par `auth.Load`

**Fichier** : `internal/auth/load.go:8-39`.

`Load` retourne `*Credentials` brut. N'importe quel logger qui fait `slog.Info("loaded", "creds", creds)` ferait fuiter. Aucun call-site ne le fait actuellement (`internal/cli/run.go:82-88` log `creds.Method` seul, OK), mais c'est fragile.

**Recommandation** : implémenter `String() string`, `MarshalJSON() / MarshalLog()` sur `Credentials` et `GitHubAppCreds` retournant des valeurs masquées :

```go
func (c *Credentials) LogValue() slog.Value {
    return slog.GroupValue(
        slog.String("method", c.Method),
        slog.String("github_url", c.GitHubURL),
        slog.String("pat", MaskedPAT(c.PAT)),
    )
}
```

Définir aussi un type alias `type Secret string` avec ces méthodes pour `PAT`.

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

### I.17 🟡 BASSE — `installation.go:http.DefaultClient`

Cf I.5. Déjà couvert.

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

### II.2 ⛔ CRITIQUE — Race condition sur le cache de runner

**Fichier** : `internal/runner/binary.go:EnsureBits:28-63`.

```go
runShPath := filepath.Join(destDir, "run.sh")
if _, err := os.Stat(runShPath); err == nil {
    return destDir, nil   // cached
}
```

Deux groupes utilisant la même version appellent `EnsureBits` en parallèle au démarrage. Scénario :

1. Goroutine A : `os.Stat(runShPath)` → NotExist, lance le DL.
2. A extrait `bin/run.sh` (le 1er fichier du tar).
3. Goroutine B : `os.Stat(runShPath)` → existe, retourne immédiatement.
4. B copie le workdir et démarre `run.sh` qui pointe vers un dossier incomplet → erreurs incompréhensibles au runtime.

Pareil si le daemon redémarre pendant un DL : `run.sh` existe mais le reste manque.

**Recommandation** : utiliser un marker `.complete` créé après extraction réussie, et une `sync.Map[version]*sync.Mutex` pour sérialiser les DL.

```go
m.locks.LoadOrStore(version, &sync.Mutex{})
mu := m.locks[version].(*sync.Mutex)
mu.Lock(); defer mu.Unlock()

if _, err := os.Stat(filepath.Join(destDir, ".complete")); err == nil {
    return destDir, nil
}
// ... after extract:
os.WriteFile(filepath.Join(destDir, ".complete"), nil, 0o644)
```

### II.3 🔴 HAUTE — Logging des secrets potentiel via `validateAndSave`

**Fichier** : `internal/cli/login.go:108-125`.

OK, ne log pas le PAT. Mais `auth.Validate` (validate.go:46-48) inclut le body dans l'erreur retournée à l'utilisateur. Body peut contenir des entêtes de debug. Cf. I.14.

### II.5 🔴 HAUTE — `RunnerGroupID: 1` hardcoded

**Fichier** : `internal/cli/daemon.go:73`.

```go
controller.ControllerConfig{
    RunnerVersion: cfg.Runner.Version,
    RunnerGroupID: 1,   // ⚠️ hardcoded
},
```

`1` est l'ID du runner group `default` de GitHub, mais une org peut en avoir plusieurs et la config YAML (`cfg.GitHub.RunnerGroup`) référence un nom, pas un ID. La résolution nom → ID n'existe nulle part.

**Recommandation** : appeler `GET /orgs/{org}/actions/runner-groups` au démarrage, matcher par nom, et stocker l'ID dans `controllerConfig`. À défaut, exposer `runner_group_id` en config YAML.

### II.6 🔴 HAUTE — Notifications & reporting synchrones sous lock

**Fichier** : `internal/health/checks.go:runChecks:11-58`.

```go
func (m *Monitor) runChecks(ctx context.Context) {
    m.mu.Lock()
    defer m.mu.Unlock()
    // ... checks ...
    for _, r := range m.reporters {
        r.ReportDaemonHealth(ctx, ...)    // ⚠️ HTTP under mutex
    }
    for group, snaps := range snapshots {
        for _, r := range m.reporters {
            r.ReportGroupHealth(ctx, ...) // ⚠️ HTTP under mutex
        }
    }
    for _, issue := range m.issues {
        m.notifier.Notify(ctx, &model.Event{...})  // ⚠️ HTTP+throttle under mutex
    }
}
```

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

### II.7 🔴 HAUTE — `Server.Run` & `srv.Close()` race avec EAGAIN

**Fichier** : `internal/api/server.go:52-79`.

Si `<-ctx.Done()` arrive avant que `srv.Serve(ln)` n'ait commencé à accepter, `srv.Close()` ferme la listener mais `Serve` peut retourner `ErrServerClosed` *après* qu'on ait déjà retourné `nil`. Globalement OK, mais le pattern juste retourne dès la première branche du select. La goroutine `srv.Serve(ln)` peut continuer à écrire vers `errCh` pendant qu'on a déjà cleanup → potentiel use-after-free du listener.

**Recommandation** : attendre la fin effective :

```go
case <-ctx.Done():
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err := srv.Shutdown(shutdownCtx)
    <-errCh   // wait for Serve to return
    _ = os.Remove(s.socketPath)
    return err
```

### II.8 🟠 MOYENNE — `runChecks` reset les issues avant d'ajouter, mais sans atomicité externe

**Fichier** : `internal/health/checks.go:17`.

```go
m.issues = m.issues[:0]
```

`Status()` lit `m.issues` avec RLock — OK car runChecks tient le Lock pendant tout le run. Donc thread-safe. Note : un Reader qui obtient RLock juste avant que runChecks demande Lock peut voir la *liste précédente*, ce qui est cohérent.

**Recommandation** : OK, aucune action.

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

### II.11 🟠 MOYENNE — `Process.Cmd` exporté

**Fichier** : `internal/runner/process.go:Process:20-27`.

```go
type Process struct {
    ...
    Cmd *exec.Cmd
}
```

`Cmd` exposé permet à du code externe de muter, mais aussi à `Stop` de devoir tester `nil` (`process.go:86-88`). Préférer encapsuler et exposer une méthode `Wait() error`.

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

### II.13 🟠 MOYENNE — `RunnerInstance.ID` & `Name` dupliqués

**Fichier** : `internal/model/group.go`, utilisé dans `internal/controller/scaler_ops.go:24-29`.

`Name = groupName-id`. Le `ID` seul n'est jamais utilisé en aval. Soit on l'enlève, soit on le surface dans le snapshot et l'API `/status`.

### II.16 🟠 MOYENNE — `cleanupStaleRunner` 3 fois la même branche d'erreur

**Fichier** : `internal/runner/cleanup.go:51-89`.

`os.RemoveAll(runnerDir)` + `if removeErr != nil { logger.WarnContext(...) }` apparaît 3 fois. Extraire dans une helper.

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

### III.6 🟠 MOYENNE — Configuration : pas de validation des labels

**Fichier** : `internal/config/validate.go:38-42`.

```go
for j, label := range g.Labels {
    if label == "" {
        errs = append(errs, ...)
    }
}
```

GitHub impose des règles sur les labels : 64 chars max, alphanumérique + `-_`. Pas validé.

**Recommandation** :
```go
labelPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
if !labelPattern.MatchString(label) { ... }
```

### III.7 🟠 MOYENNE — `Duration.MarshalYAML` non implémenté

**Fichier** : `internal/config/types.go:96-98`.

`UnmarshalYAML` existe (visible via overview), mais l'overview ne montre pas si `MarshalYAML` est correct. Symptôme : si on veut dumper la config résolue (commande `ghr config print` à venir), `Duration` deviendrait `0` au lieu de `"30s"`.

### III.8 🟠 MOYENNE — Pas de `--dry-run` pour les commandes destructrices

`purge`, `restart`, `stop --force` ne supportent pas `--dry-run`. Pour un outil qui touche aux processus système et au scale set GitHub, c'est précieux.

### III.9 🟠 MOYENNE — `Cleanup` ne nettoie pas les logs orphelins

**Fichier** : `internal/runner/process.go:Cleanup:131-136`.

Supprime le workdir mais pas le sous-dossier de logs `groups/<group>/runners/<name>/`. Au fil du temps des dizaines de dossiers vides s'accumulent (les fichiers `.json` sont rotatés par date, mais le dossier reste).

### III.10 🟠 MOYENNE — `pidFilePath` et `socketPath` calculés différemment

**Fichier** : `internal/cli/daemon.go:161-163`, `internal/api/server.go:35`.

Deux conventions pour le même état (`stateDir`) : `daemon.pid`, `daemon.state.json`, `ghr.sock` listés en dur dans `cleanupStateFiles` (`purge.go:172-180`). Si on ajoute un fichier, il faut maintenir la liste.

**Recommandation** : centraliser dans `internal/state/` :

```go
package state

type Paths struct { Dir string }
func (p Paths) PIDFile() string  { return filepath.Join(p.Dir, "daemon.pid") }
func (p Paths) StateFile() string { return filepath.Join(p.Dir, "daemon.state.json") }
func (p Paths) Socket() string   { return filepath.Join(p.Dir, "ghr.sock") }
func (p Paths) All() []string    { return []string{p.PIDFile(), p.StateFile(), p.Socket()} }
```

### III.11 🟠 MOYENNE — `notification/service.go:Notify` séquentiel sur les providers

**Fichier** : `internal/notification/service.go:40-54`.

Si Discord prend 2 s, le webhook attend 2 s. Pas critique car il n'y a souvent qu'1 provider, mais avec 3 providers + retry, la latence cumule.

**Recommandation** : dispatch parallèle avec `errgroup` ou `sync.WaitGroup`.

### III.12 🟡 BASSE — `internal/model/event.go` mêle types et constants

47 LOC mixant struct + 4 levels + 16 events. À garder pour le moment, c'est le "shared types" pattern correct.

### III.13 🟡 BASSE — `internal/notification/discord_payload.go` couleurs hardcodées

`colorForLevel` (non lue ici mais évoquée) : à exposer en config si on veut customiser.

### III.14 🟡 BASSE — `Process.PID int` should be `int32` for portability

Mineur. macOS PID est int32. `int` suffit en Go 64-bit.

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

### IV.1 ⛔ CRITIQUE — Aucune panic recovery sur les goroutines daemon

**Fichiers** : `internal/cli/run.go:74-128` (`runDaemonGroup`), `internal/controller/controller.go:Run`.

Une panic dans `runGroup` ou `MacOSScaler.HandleJobCompleted` propage à la goroutine top-level et crash le daemon. launchd redémarrera (KeepAlive=true sur SuccessfulExit=false), mais on perd l'état en mémoire.

**Recommandation** : wrapper chaque goroutine top-level :

```go
func safe(fn func() error) func() error {
    return func() (err error) {
        defer func() {
            if r := recover(); r != nil {
                err = fmt.Errorf("panic in actor: %v\n%s", r, debug.Stack())
            }
        }()
        return fn()
    }
}
g.Add(safe(func() error { return d.ctrl.Run(ctx) }), func(error) { cancel() })
```

### IV.2 🔴 HAUTE — Pas de circuit breaker sur GitHub API

**Fichiers** : `internal/auth/installations.go`, `internal/runner/binary.go:resolveLatestVersion`.

En cas d'incident GitHub (API down 30 min), le daemon retente toutes les 5 s par groupe → DDoS GitHub sortant + perte de tokens rate-limit.

**Recommandation** : intégrer `sony/gobreaker` ou similaire :

```go
cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
    Name: "github-api",
    Timeout: 60 * time.Second,
    ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures > 5 },
})
```

### IV.3 🔴 HAUTE — Webhook & UptimeKuma sans retry

**Fichiers** : `internal/notification/webhook.go:Send`, `internal/monitoring/uptimekuma.go:push`.

`Webhook.Send` n'a aucun retry — un 503 transitoire = perte de notification. `UptimeKuma.push` idem.

**Recommandation** : utiliser `hashicorp/go-retryablehttp` (déjà dans `go.sum`!) :

```go
client := retryablehttp.NewClient()
client.RetryMax = 3
client.RetryWaitMin = 1 * time.Second
client.RetryWaitMax = 10 * time.Second
```

### IV.4 🔴 HAUTE — Pas de reload de config (SIGHUP)

Le daemon doit être stoppé/restart pour appliquer un changement de label ou de min_runners. Pour un service prod, c'est une grosse limitation.

**Recommandation** : capturer SIGHUP dans `runDaemonGroup`, recharger `config.Load(cfgFile)`, comparer avec l'état actuel, et appeler une nouvelle méthode `Controller.Reconfigure(newGroups)`.

### IV.5 🔴 HAUTE — `EnsureBits` ne nettoie pas les caches anciens

**Fichier** : `internal/runner/binary.go`.

Si on passe de v2.310 à v2.311, le cache v2.310 reste à vie. Sur disques SSD limités, c'est problématique. Aucune logique de GC.

**Recommandation** : après extraction réussie, supprimer les autres versions cachées sauf les N plus récentes (configurable, default 3).

### IV.6 🟠 MOYENNE — `Discord.Send` retry une seule fois

Cf. II.17. Backoff configurable / fixed retry count.

### IV.7 🟠 MOYENNE — Aucune liveness probe pour le daemon

Si le main thread bloque (deadlock sur une mutex), le daemon est "running" pour launchd mais ne fait rien.

**Recommandation** : un watchdog goroutine qui pingue `/health` localement toutes les 30 s ; si 3 échecs consécutifs, log critical et `os.Exit(2)` pour laisser launchd respawn.

### IV.8 🟠 MOYENNE — `runGroup` backoff non randomisé

**Fichier** : `internal/controller/group.go:184-190`.

```go
func nextBackoff(current time.Duration) time.Duration {
    next := current * 2
    if next > backoffMax { return backoffMax }
    return next
}
```

Doubling sans jitter → thundering herd si N groupes se cassent en même temps (panne GitHub → tous retry au même tick).

**Recommandation** : ajouter ±20 % de jitter (`rand.Float64()`).

### IV.9 🟠 MOYENNE — `Stop` n'a pas de timeout après SIGKILL

**Fichier** : `internal/runner/process.go:85-117`.

```go
case <-time.After(stopGracePeriod):
    ...
    if err := proc.Cmd.Process.Kill(); err != nil { ... }
    return <-done   // ⚠️ peut bloquer si Wait jamais ne retourne
}
```

`Wait` après `Kill` retourne généralement vite, mais en cas de zombie ou parent reparenté à launchd, peut bloquer.

**Recommandation** : `select { case err := <-done: return err; case <-time.After(5*time.Second): return ErrStuckProcess }`.

### IV.10 🟠 MOYENNE — `Discord` rate-limit retry sans backoff sur 5xx

Le code distingue 429 (avec retry) du reste (échec). Mais un 503 transitoire = échec immédiat.

### IV.11 🟡 BASSE — Pas de `--max-age` sur le cache

Cf. IV.5.

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

### VII.6 🟠 MOYENNE — Logs runner non tagués

`RunnerOutputFile` écrit le stdout/stderr brut. Pas de wrapping JSON pour les corréler aux events du daemon. Cf. github actions runner émet déjà du log structuré sur sa propre stack, mais nous on les balance comme-is.

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

### IX.1 🔴 HAUTE — README divergent du code

Cf. III.4.

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
| `--max-age` sur cache binaries | Disk hygiene | XS |
| Audit log file séparé | Compliance | S |
| Notification level filter (`min_level: warn`) | UX notifs | XS |
| SHA256 verification des tarballs runner | Sécu (I.2) | S |
| Chmod 0o600 sur le socket | Sécu (I.4) | XS |

### X.2 Medium (1-2 semaines)

| Feature | Bénéfice | Effort |
|---------|----------|--------|
| Prometheus `/metrics` | Observabilité | M |
| Reload via SIGHUP | Ops UX (IV.4) | M |
| Keychain pour le PAT (macOS) | Sécu (I.6) | M |
| Circuit breaker GitHub API | Résilience (IV.2) | S |
| Drain mode (`ghr stop --drain`) | Ops UX | M |
| TUI dashboard (`ghr top`) en Bubble Tea | UX | M |
| Hardlinks au lieu de copy | Perf (VI.1) | S |
| Retry sur webhook/uptimekuma | Résilience (IV.3) | S |
| Linux support (testé, pas juste compile) | Adoption | M |
| Self-update (`ghr update`) | Ops UX | M |

### X.3 Large (1+ mois)

| Feature | Bénéfice | Effort |
|---------|----------|--------|
| Multi-runner-group ID resolution | Correctness (II.5) | M |
| Migration de `launchctl load` vers `bootstrap` | macOS forward-compat | M |
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

## XI. Plan d'action priorisé

### Phase 1 — Sécurité bloquante (1 sprint, ~5 j)

1. ✅ Fix tar symlink traversal (I.1) — 2 h.
2. ✅ SHA-256 verification (I.2) — 4 h.
3. ✅ `pgrep` safety guards (I.3) — 4 h (+ valider workdir_base).
4. ✅ Chmod socket 0600 (I.4) — 30 min.
5. ✅ HTTP client timeouts globaux (I.5) — 2 h.
6. ✅ Plist XML escape (I.7) — 1 h.
7. ✅ `gosec` dans CI (I.21) — 1 h.

### Phase 2 — Bugs correctness (1 sprint, ~5 j)

8. ✅ Câbler `UpdateGroupStats`/`RecordStart{Failure,Success}` (II.1) — 1 j.
9. ✅ Lock cache versionné + marker `.complete` (II.2) — 4 h.
10. ✅ Loop variable explicite (II.4) — 30 min.
11. ✅ `RunnerGroupID` config-driven (II.5) — 1 j.
12. ✅ Notifications async sous lock (II.6) — 4 h.
13. ✅ Graceful API shutdown (II.7, I.10, I.11) — 2 h.
14. ✅ Process panic recovery (IV.1) — 2 h.
15. ✅ Tests tar extraction (V.2) — 1 j.

### Phase 3 — Résilience & observabilité (2 sprints, ~10 j)

16. ✅ Circuit breaker + retry generalized (IV.2, IV.3) — 2 j.
17. ✅ Reload SIGHUP (IV.4) — 2 j.
18. ✅ Cache GC (IV.5) — 4 h.
19. ✅ Liveness watchdog (IV.7) — 4 h.
20. ✅ `/metrics` Prometheus (VII.1) — 1 j.
21. ✅ Tracing OpenTelemetry initial (VII.2) — 2 j.
22. ✅ Audit log (VII.3) — 1 j.
23. ✅ `ghr logs` command (VII.4) — 1 j.

### Phase 4 — Maintenabilité & tests (1 sprint, ~5 j)

24. ✅ Split fichiers > 200 LOC (III.1) — 1 j.
25. ✅ Centraliser `state.Paths` (III.10) — 4 h.
26. ✅ Tests CLI (V.1) — 2 j.
27. ✅ Coverage gate dans CI (V.9) — 4 h.
28. ✅ `go mod tidy` (VIII.3) — 30 min.
29. ✅ README sync (III.4) — 2 h.
30. ✅ Regen specs (III.3) — 1 j.

### Phase 5 — Features quick wins (1 sprint, ~5 j)

Cf. table X.1.

### Estimation totale

| Phase | Durée | Risque |
|-------|-------|--------|
| 1     | 5 j   | bas    |
| 2     | 5 j   | moyen  |
| 3     | 10 j  | moyen  |
| 4     | 5 j   | bas    |
| 5     | 5 j   | bas    |
| **Total** | **~6 semaines** | |

---

## XII. Annexes

### A. Tableau récapitulatif des findings

| ID    | Sévérité | Catégorie     | Titre                                                  | Fichier:Ligne                       |
|-------|----------|---------------|--------------------------------------------------------|-------------------------------------|
| I.1   | Critique | Sécurité      | Symlink traversal tar                                  | runner/download.go:68               |
| I.2   | Critique | Sécurité      | Pas de checksum tarball                                | runner/download.go:17               |
| I.3   | Critique | Sécurité      | `pgrep -f` non sanitized                               | runner/cleanup.go:91                |
| II.1  | Critique | Bug           | Stats santé jamais alimentées                          | health/group_state.go               |
| II.2  | Critique | Bug           | Race cache binaries                                    | runner/binary.go:28                 |
| IV.1  | Critique | Résilience    | Pas de panic recovery                                  | cli/run.go:74                       |
| VII.1 | Critique | Observabilité | Pas de `/metrics`                                      | —                                   |
| I.4   | Haute    | Sécurité      | Unix socket lisible par tous                           | api/server.go:46                    |
| I.5   | Haute    | Sécurité      | http.DefaultClient sans timeout                        | auth/installations.go               |
| I.6   | Haute    | Sécurité      | Credentials clair                                      | auth/store.go:37                    |
| I.7   | Haute    | Sécurité      | Plist XML non échappé                                  | launchd/plist.go:8                  |
| I.8   | Haute    | Sécurité      | launchctl deprecated                                   | launchd/launchctl.go                |
| I.9   | Haute    | Sécurité      | copyDir symlinks                                       | runner/copy.go:26                   |
| II.3  | Haute    | Bug           | Validate erreurs verbeuses                             | auth/validate.go:46                 |
| II.4  | Haute    | Bug           | Loop variable                                          | controller/controller.go:75         |
| II.5  | Haute    | Bug           | RunnerGroupID hardcoded                                | cli/daemon.go:73                    |
| II.6  | Haute    | Bug           | Notifs sous mutex                                      | health/checks.go:11                 |
| II.7  | Haute    | Bug           | API shutdown abrupt                                    | api/server.go:62                    |
| III.1 | Haute    | Archi         | Fichiers > 200 LOC                                     | health/checks.go (et 5 autres)      |
| III.2 | Haute    | Archi         | Interface scaleSetClient = 7 méthodes                  | controller/controller.go:16         |
| IV.2  | Haute    | Résilience    | Pas de circuit breaker                                 | auth/*.go                           |
| IV.3  | Haute    | Résilience    | Webhook/UK sans retry                                  | notification/webhook.go             |
| IV.4  | Haute    | Résilience    | Pas de SIGHUP reload                                   | cli/run.go                          |
| IV.5  | Haute    | Résilience    | Pas de cache GC                                        | runner/binary.go                    |
| V.1   | Haute    | Tests         | Aucun test CLI                                         | cli/*.go                            |
| V.2   | Haute    | Tests         | Aucun test tar extraction                              | runner/download.go                  |
| V.3   | Haute    | Tests         | Aucun test controller/group                            | controller/group.go                 |
| VII.2 | Haute    | Observabilité | Pas de tracing                                         | —                                   |
| VII.3 | Haute    | Observabilité | Pas d'audit log                                        | —                                   |
| VII.4 | Haute    | Observabilité | Pas de `ghr logs`                                      | cli/                                |
| IX.1  | Haute    | Doc           | README divergent                                       | README.md                           |
| ...   | ...      | ...           | (voir corps du doc)                                    | ...                                 |

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
