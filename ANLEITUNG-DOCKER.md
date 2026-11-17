# Explo (Multi-Profil-Fork) als Docker-Container aufsetzen und testen

Diese Anleitung ist für einen Agenten (oder Menschen), der den Fork als Container aufsetzt und testet.
Ziel: prüfen, ob die neuen Funktionen in einem **echten Container** laufen. Lokal auf einem Mac wurden sie
nur teilweise getestet (siehe Abschnitt 7).

## 1. Was ist das?

Explo holt Empfehlungs-Playlists von ListenBrainz, lädt fehlende Songs herunter (YouTube / Soulseek / Lidarr)
und legt die Playlist in einem Musikserver an (hier: **Navidrome**). Der Code in diesem Ordner ist ein Fork von
https://github.com/LumePart/Explo mit diesen Neuerungen:

- **Mehrere Nutzer (Profile) in einem Container.** Jedes Profil hat eigene `.env`, eigene Daten und eigene Cron-Jobs.
  Das bisherige Setup läuft unverändert als Profil `default`.
- Profile in der Web-UI anlegen, **umbenennen, löschen** (Löschen verschiebt nur nach `profiles/.trash/`).
- **Eigene Playlist-Cover** hochladen (⋮-Menü der Playlist-Karte, auch beim Import von Custom-Playlists).
  Navidrome ab Version 0.61 über dessen native API.
- Playlist **ersetzen** (nur eine Playlist pro Name, Songs bleiben) oder **jede Version behalten**.
  Bei Navidrome werden dabei alle gleichnamigen Duplikate gelöscht.
- Neues **Settings-Formular** (Server, Admin ja/nein, ListenBrainz, Download-Quellen, YouTube-Key und Cookies, Audioformat).
- **Wochenplan-Übersicht** und ein **„Restart needed"-Banner mit Button**. Das Banner erscheint nur, wenn sich die Cron-Tabelle
  (Zeitpläne samt Flags, Profile) gegenüber dem Containerstart wirklich geändert hat, und verschwindet, wenn man es zurückändert.

## 2. Voraussetzungen

- Docker (mit Compose v2). Linux amd64 oder arm64.
- Ein erreichbarer **Navidrome** (am besten >= 0.61 für Cover) und ein **Test-Navidrome-User**
  (Wegwerf-Konto, kein echtes Passwort verwenden). Für einen Test mit einem Nicht-Admin zusätzlich ein Admin-Konto.
- Ein beschreibbarer Ordner für Downloads. **Ohne den schlägt jeder Download fehl** (Fehler `mkdir /data: read-only file system`).
- Optional: ListenBrainz-Konto (Name und User-Token) für echte Empfehlungen.

Zugangsdaten **nie in Chats oder Logs posten**, sondern in Dateien auf dem Host oder in die Web-UI eintragen.

## 3. Bauen

Im entpackten Ordner (dort liegt das `Dockerfile`):

```bash
docker build -t explo-multi:test .
```

Das Dockerfile baut das Frontend (Node) und das Backend (Go) selbst, es ist nichts vorzubereiten.
Dauer: ein paar Minuten. Bei Problemen mit `TARGETARCH`: `docker buildx build --load -t explo-multi:test .`.

### Alternative: fertiges Image von GitHub

GitHub baut das Image bei jedem Push selbst (amd64 und arm64): `ghcr.io/wzkiie/explo-multiprofile:latest`.
Das Paket ist privat, auf dem Rechner einmal anmelden (Token mit Recht `read:packages`, **nicht** in Chats posten):

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u Wzkiie --password-stdin
docker pull ghcr.io/wzkiie/explo-multiprofile:latest
```

Dann in `docker-compose.test.yaml` bei `image:` diesen Namen eintragen und den Bau-Schritt überspringen.

## 4. Starten

Die Datei `docker-compose.test.yaml` liegt im Ordner. Einmal die Ordner vorbereiten:

```bash
mkdir -p explo-data/env explo-data/config explo-data/music
touch explo-data/env/.env
docker compose -f docker-compose.test.yaml up -d
docker compose -f docker-compose.test.yaml logs -f
```

Wichtig in der Compose-Datei:

- `restart: unless-stopped` ist **Pflicht** (der Restart-Button beendet den Container, Docker startet ihn neu).
- `./explo-data/env/.env` muss als **Datei** existieren, sonst legt Docker einen Ordner an.
- `./explo-data/music:/data` ist der Download-Ordner. Ist er schreibgeschützt, schlagen alle Downloads fehl.
- `TZ` setzen, sonst laufen Zeitpläne in UTC.
- Für Zugriff von außen: `UI_USERNAME` und `UI_PASSWORD` setzen, sonst ist die UI offen.

Die Web-UI läuft auf `http://<host>:7288`. Beim ersten Öffnen startet der Einrichtungsassistent.

## 5. Einrichten (Profil `default`)

1. UI öffnen, Wizard durchlaufen oder im Tab **Settings** ausfüllen: Music server = Navidrome/Subsonic,
   Adresse (z. B. `http://192.168.x.x:4533`), Nutzername, Passwort.
2. Frage „Is this user an admin?" beantworten. Bei **No** sind die Admin-Felder Pflicht
   (Explo braucht einen Admin nur für den Bibliotheks-Scan).
3. ListenBrainz-Name und Token eintragen, Download-Quelle YouTube angehakt lassen.
4. **Save settings** klicken (feste Leiste unten).

## 6. Test-Checkliste (bitte der Reihe nach, Ergebnis jeweils notieren)

Erwartung steht jeweils in Klammern.

1. **Start:** Container läuft, UI lädt, Logs ohne Fehler. (`docker logs explo-test` zeigt „Explo web UI started")
2. **Lauf ohne Fehler:** Tab Home, bei „Manual Run" eine Playlist wählen (z. B. Weekly Exploration), Modus **Normal**, Run.
   (Songs werden heruntergeladen, in `explo-data/music/` erscheinen Dateien, in Navidrome entsteht eine Playlist.
   Die Anzahl der Songs in Navidrome entspricht der Anzahl der Tracks im Log, nicht nur den schon vorhandenen.)
3. **Replace:** denselben Lauf zweimal starten. (In Navidrome gibt es **eine** Playlist, nicht zwei. Songs bleiben erhalten.)
   Test mit Duplikaten: in Navidrome vorher von Hand 3 gleichnamige Playlists anlegen. (Alle werden beim Lauf entfernt.)
4. **Keep:** Settings → Playlists → „Keep every version", Lauf. (Neue Playlist mit Datum im Namen.)
   Danach wieder auf „Replace" stellen.
5. **Cover:** Home → ⋮ auf einer Playlist-Karte → „Set cover…", Bild wählen, danach einen Lauf starten.
   (In Navidrome hat die Playlist das Cover. Bei Navidrome < 0.61 nur eine Warnung im Log, kein Abbruch.)
6. **Profile:** Header → Dropdown → „+ add profile…", Name `test2`. (Das Banner „Restart needed" erscheint erst, wenn ein
   Profil einen Zeitplan hat, denn nur die Cron-Tabelle braucht den Neustart.)
   Im neuen Profil in Settings einen **anderen** Navidrome-User eintragen und einen Lauf starten.
   (Playlist erscheint beim richtigen User. Prüfen: `docker exec explo-test ls /opt/explo/config/profiles/test2`
   zeigt `.env`, `cache`, `logs`. Container-Variablen aus der Compose-Datei, z. B. `SYSTEM_URL`, gelten nur für `default`.)
7. **Restart-Button:** zuerst bei einer Playlist einen Zeitplan setzen oder ändern (⋮ → Edit Schedule, Save), dann erscheint das Banner.
   Banner → „Restart now" → „Yes, restart". (Container startet neu: `docker ps` zeigt kurze Uptime,
   die Seite lädt selbst neu, das Banner ist weg. **Falls der Container nicht neu startet:** notieren und mit
   `docker restart explo-test` weitermachen. Das ist ein wichtiger Befund.)
8. **Cron:** bei einer Playlist über ⋮ → Edit Schedule einen Zeitpunkt 3 Minuten in der Zukunft setzen
   (Zeitzone beachten), Restart, dann `docker exec explo-test cat /etc/crontabs/root`.
   (Es gibt einen Eintrag pro geplanter Playlist, für Profil `test2` mit `WEB_DATA_PATH=".../profiles/test2/"`.)
   Zur Uhrzeit starten, Logs prüfen. (Lauf startet von selbst und baut die Playlist.)
9. **Umbenennen/Löschen:** Profil `test2` umbenennen (`test3`), dann löschen. (Ordner liegt danach unter
   `explo-data/config/profiles/.trash/`, Dropdown zeigt nur noch `default`.)
10. **YouTube-Cookies und Format (optional):** Settings → YouTube → Cookies hochladen; Audioformat M4A oder Opus wählen,
    einen Lauf starten. (Dateien haben die gewählte Endung und werden in Navidrome gespielt.)

## 7. Bekannte Lücken (bitte besonders darauf achten)

Folgendes wurde **nicht** in einem echten Container getestet:

- Der Restart-Button: die Web-UI legt die Datei `/tmp/explo-restart` an, `docker/start.sh` prüft sie alle 2 Sekunden und beendet
  den Container, Docker startet ihn per Restart-Policy neu. Das funktioniert auch mit `PUID`/`PGID`. Im echten Container ungetestet.
- Der Cron-Block für mehrere Profile unter Alpine mit `su-exec`. Cron-Läufe benannter Profile setzen `EXPLO_ISOLATED=1`,
  damit Variablen aus der Compose-Datei die `.env` des Profils nicht überschreiben (prüfen: ein Wert nur in der Compose-Datei
  darf im Profil `test2` nicht ankommen).
- Ein Lauf mit **Nicht-Admin-Nutzer** plus Admin-Account für den Scan.
- Cover-Upload bei Plex, Jellyfin und Emby (nur Navidrome wurde gegen einen echten Server geprüft).
- M4A/Opus-Ausgabe und echte YouTube-Cookies.
- Zwei Profile mit gleicher Cron-Uhrzeit laufen **parallel** (nur manuelle Läufe sind gegeneinander gesperrt).

Bekannte Einschränkungen:

- Zeitplan-Änderungen brauchen einen Container-Neustart (daher der Banner).
- Steht ein `*_SCHEDULE` zusätzlich in der Compose-Datei, hat er für das Profil `default` Vorrang vor der UI. Das Banner merkt das nicht.
- Ist das Download-Verzeichnis nicht beschreibbar, bricht der Lauf jetzt mit einer klaren Fehlermeldung ab. Eine bestehende Playlist wird
  erst unmittelbar vor dem Anlegen der neuen gelöscht, ein fehlgeschlagener Lauf lässt sie also stehen.
- Die Reihenfolge der Download-Quellen ist nicht einstellbar.
- Das Symptom „es kommen nur 32 von 50 Songs" bedeutet meist: Download-Ordner prüfen (siehe oben).

## 8. Fehlersuche

```bash
docker logs -f explo-test                                   # Gesamtlog
docker exec explo-test cat /opt/explo/config/logs/explo.log # Log der UI und der Läufe (Profil default)
docker exec explo-test cat /etc/crontabs/root               # eingetragene Cron-Jobs
docker exec explo-test ls -R /opt/explo/config | head -50   # Profile und Daten
docker exec explo-test touch /data/test && echo schreibbar  # Download-Ordner prüfen
```

Häufige Ursachen:

- `mkdir /data: read-only file system` → Volume `/data` fehlt oder ist schreibgeschützt.
- „failed to schedule a library scan" → der eingetragene User ist kein Navidrome-Admin und es ist kein Admin-Account hinterlegt.
- Cover-Warnung im Log → Navidrome-Version unter 0.61 oder Cover-Upload dort abgeschaltet (`EnableArtworkUpload`).
- Keine Cron-Läufe → `TZ` prüfen, Container nach der Zeitplan-Änderung neu gestartet?

## 9. Was zurückgemeldet werden soll

Bitte pro Punkt der Checkliste (Abschnitt 6): **OK / nicht OK**, bei „nicht OK" die relevanten Logzeilen
(ohne Passwörter, Tokens oder Cookies) und was erwartet wurde. Besonders wichtig sind Punkt 2, 7 und 8.
Das Wegwerf-Konto und die Test-Playlists danach wieder löschen.
