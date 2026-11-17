# Explo - Music Discovery for Self-Hosted Music Systems


[![Discord](https://img.shields.io/discord/1497141529696014409?style=flat&logo=Discord&labelColor=white&color=black&link=https%3A%2F%2Fdiscord.gg%2FuFWWPaN2zk)](https://discord.gg/uFWWPaN2zk)

<p align="center">
  <img src="visual/dashboard.png" width="850" />
</p>

**Explo** bridges the gap between music discovery and self-hosted music systems. Its main function is to act as a self-hosted alternative to Spotify’s *Discover Weekly*, automating music discovery based on your listening history.

Explo uses the [ListenBrainz](https://listenbrainz.org/) recommendation engine to retrieve personalized tracks and automatically imports them into your music library.

---

## Features

- Automatic music discovery powered by ListenBrainz
- Web UI for setup, scheduling, and playlist import management
- Fetch personalized playlists from ListenBrainz:
  - Weekly Exploration
  - Weekly Jams
  - Daily Jams
  - Fresh Releases
- Import custom playlists from:
  - Apple Music
  - ListenBrainz
  - Spotify
- Automatically request playlist tracks from YouTube and Soulseek, or albums through Lidarr
- Add metadata to downloaded tracks
- Create playlists in your music system
- Optionally keep previous playlists for later listening
---

## Documentation

See the [Wiki Home](https://github.com/LumePart/Explo/wiki) for an overview of supported systems and next steps.

Or jump directly to:
- [Quick Start](https://github.com/LumePart/Explo/wiki/2.-Quick-Start) – Get Explo running with docker
- [Getting Started](https://github.com/LumePart/Explo/wiki/3.-Getting-Started) – Installation and setup guide  
- [Configuration Parameters](https://github.com/LumePart/Explo/wiki/5.-Configuration-Parameters) – Environment variable and flag reference  
- [System Notes](https://github.com/LumePart/Explo/wiki/6.-System-Notes) – Known issues and system-specific tips  
- [FAQ](https://github.com/LumePart/Explo/wiki/8.-FAQ) – Common questions


## Multiple users in one container

One container can serve any number of users (e.g. several Navidrome accounts). Open the web UI and use the
profile dropdown in the header ("+ add profile…"). Each profile has its own `.env` (credentials, playlists,
schedules) and data dir under `<config dir>/profiles/<name>/`. The existing setup keeps working unchanged as
profile `default`.

- Set a **different `DOWNLOAD_DIR`/`SYSTEM_USERNAME`** in each profile's `.env`.
- Config values set via container `environment:` apply to the `default` profile only. Named profiles ignore
  them on purpose; put per-user settings into the profile's `.env`.
- Schedules are registered when the container starts, so restart it after changing a profile's schedule.
- Manual runs are serialized across profiles (one run at a time).

## Custom playlist covers

In the web UI (Home → Cover) you can upload your own cover (JPEG/PNG/GIF, max 10 MB) for any playlist. It is set
automatically after every run, since replacing a playlist creates a new one. Supported: Plex, Jellyfin, Emby and
Navidrome >= 0.61 (via its native API, using `SYSTEM_USERNAME`/`SYSTEM_PASSWORD`; the Subsonic API itself cannot set covers).

For Subsonic/Navidrome, replacing a playlist now removes *all* playlists with that name, which also cleans up old duplicates.

## Kubernetes

```bash
helm repo add explo https://lumepart.github.io/Explo && helm repo update
helm install explo explo/explo --namespace explo --create-namespace
```

See [charts/explo/README.md](charts/explo/README.md).

## Acknowledgements

Explo uses the following 3rd-party libraries:

- [ffmpeg-go](https://github.com/u2takey/ffmpeg-go): Go wrapper for FFmpeg

- [goutubedl](https://github.com/wader/goutubedl): Go wrapper for yt-dlp

- [godotenv](https://github.com/joho/godotenv): Load configuration from `.env` files

- [ytmusicapi](https://github.com/sigma67/ytmusicapi): Unofficial Youtube Music API

- [notify](https://github.com/nikoksr/notify): Module for sending notifications to different services

- [gocron](https://github.com/go-co-op/gocron): Internal cron scheduling

## Contributing

Contributions are welcome! For suggestions, bug reports, or feature requests, please open an issue. Before opening a pull request, please read the [contributing guide](https://github.com/LumePart/Explo?tab=contributing-ov-file)

For discussion regarding development or help, join our [Discord!](https://discord.gg/uFWWPaN2zk)