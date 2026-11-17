#!/bin/sh

PUID="${PUID:-0}"
PGID="${PGID:-0}"

# The web UI's "Restart now" button creates this file (it may run as a non-root user and cannot
# signal PID 1). The loop at the end of this script notices it and ends the container; the Docker
# restart policy (restart: unless-stopped) brings it back and the cron table is rebuilt.
RESTART_FLAG="${EXPLO_RESTART_FLAG:-/tmp/explo-restart}"
export EXPLO_RESTART_FLAG="$RESTART_FLAG"
rm -f "$RESTART_FLAG"

# PID 1 ignores signals without a handler: install one early, so "docker stop" works at any time.
trap '[ -n "$CROND_PID" ] && kill "$CROND_PID" 2>/dev/null; exit 0' TERM INT


# Update yt-dlp on startup
pip install --disable-pip-version-check --root-user-action=ignore --no-cache-dir --upgrade  yt-dlp
# Clear crontabs
crontab -r

# Update yt-dlp periodically
YTDLP_UPDATE="${YTDLP_UPDATE:-59 23 * * *}"
echo "$YTDLP_UPDATE echo 'Updating yt-dlp...' && pip install --disable-pip-version-check --root-user-action=ignore --no-cache-dir --upgrade yt-dlp >> /proc/1/fd/1 2>&1" >> /etc/crontabs/root
echo "[setup] Registered yt-dlp update job"
echo "        Schedule: $YTDLP_UPDATE"

if [ "$PUID" != "0" ] || [ "$PGID" != "0" ]; then
    groupmod -o -g "$PGID" explo
    usermod -o -u "$PUID" explo
    RUN_USER="explo"
    RUNNER="su-exec explo"
else
    echo "[setup] WARN: running as root. Consider defining PUID & PGID in docker-compose to run as a non-root user"
    RUN_USER="root"
    RUNNER=""
fi

mkdir -p /opt/explo
if [ "$RUN_USER" != "root" ]; then 
  chown -R explo:explo /opt/explo
fi

echo "[setup] Starting web UI..."
# If user incorectly mounts the config path as a directory, we'll try to automatically append it to .env inside it instead of failing.
WEB_ENV_PATH="${WEB_ENV_PATH:-/opt/explo/.env}"
if [ -d "$WEB_ENV_PATH" ]; then
    WEB_ENV_PATH="$WEB_ENV_PATH/.env"
    echo "[setup] Config path is a directory, using $WEB_ENV_PATH"
fi
WEB_UI=true WEB_ENV_PATH="$WEB_ENV_PATH" WEB_ADDR="${WEB_ADDR:-:7288}" $RUNNER ./explo --config "$WEB_ENV_PATH" &
echo "[setup] Web UI available at http://localhost:${WEB_ADDR##*:}"

echo "[setup] Initializing cron jobs..."

# Load *_SCHEDULE and *_FLAGS from .env if not already set in the environment.
# This allows the web UI to configure schedules by writing to the .env file.
_cfg="${WEB_ENV_PATH:-/opt/explo/.env}"
if [ -f "$_cfg" ]; then
  while IFS= read -r _line; do
    case "$_line" in \#*|'') continue ;; esac
    _key="${_line%%=*}"
    case "$_key" in
      *_SCHEDULE|*_FLAGS)
        if [ -z "$(printenv "$_key" 2>/dev/null)" ]; then
          export "$_key=${_line#*=}"
        fi
        ;;
    esac
  done < "$_cfg"
fi


# $CRON_SHCEDULE was deprecated in v0.11.0, keeping this block for backwards compatibility
if [ -n "$CRON_SCHEDULE" ]; then
    echo "$CRON_SCHEDULE cd /opt/explo && $RUNNER ./explo --config \"$_cfg\" >> /proc/1/fd/1 2>&1" >> /etc/crontabs/root
    chmod 600 /etc/crontabs/root
    echo "[setup] Registered single CRON_SCHEDULE job: $CRON_SCHEDULE"
fi

# Loop over all *_SCHEDULE environment variables
for var in $(env | grep "_SCHEDULE=" | cut -d= -f1); do
  job="${var%_SCHEDULE}"                     # Job name (e.g WEEKLY_EXPLORATION)
  schedule="$(printenv "$var")"              # Cron schedule
  flags_var="${job}_FLAGS"
  flags="$(printenv "$flags_var")"           # e.g. --playlist weekly-exploration

  if [ -z "$schedule" ]; then
    echo "[setup] Skipping $job: schedule is empty"
    continue
  fi

  # Default: just run explo if flags are empty
  cmd="cd /opt/explo && $RUNNER ./explo --config \"$_cfg\" $flags >> /proc/1/fd/1 2>&1"

  echo "$schedule $cmd" >> /etc/crontabs/root
  echo "[setup] Registered job: $job"
  echo "        Schedule: $schedule"
  echo "        Command : ./explo --config $_cfg $flags"
done

# Additional user profiles: <data dir>/profiles/<name>/.env (each profile has its own schedules,
# credentials and data dir). Container env vars are NOT applied to these profiles.
_profiles_dir="${WEB_DATA_PATH:-/opt/explo/config}/profiles"
_profiles_dir="${_profiles_dir%/}"
if [ -d "$_profiles_dir" ]; then
  for _pdir in "$_profiles_dir"/*/; do
    _pdir="${_pdir%/}"
    _pname="$(basename "$_pdir")"
    _penv="$_pdir/.env"
    [ -f "$_penv" ] || continue
    # same rule as the web UI: a-z, 0-9, - and _ (not leading), max 32 chars, not "default"
    case "$_pname" in default|[!a-z0-9]*|*[!a-z0-9_-]*) echo "[setup] Ignoring folder profiles/$_pname (invalid profile name)"; continue ;; esac
    [ "${#_pname}" -le 32 ] || { echo "[setup] Ignoring folder profiles/$_pname (name too long)"; continue; }
    echo "[setup] Profile: $_pname"
    # collect <JOB>_SCHEDULE / <JOB>_FLAGS pairs from the profile's .env
    _jobs=""
    while IFS= read -r _line; do
      case "$_line" in \#*|'') continue ;; esac
      case "${_line%%=*}" in *_SCHEDULE) _jobs="$_jobs ${_line%%_SCHEDULE=*}" ;; esac
    done < "$_penv"
    for _job in $(printf '%s\n' $_jobs | sort -u); do
      # last assignment wins (same as the web UI), surrounding whitespace and quotes are removed
      _sched="$(grep "^${_job}_SCHEDULE=" "$_penv" | tail -n1 | cut -d= -f2- | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e "s/^['\"]//" -e "s/['\"]\$//")"
      _flags="$(grep "^${_job}_FLAGS=" "$_penv" | tail -n1 | cut -d= -f2- | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e "s/^['\"]//" -e "s/['\"]\$//")"
      if [ -z "$_sched" ]; then
        echo "[setup] Skipping $_pname/$_job: schedule is empty"
        continue
      fi
      echo "$_sched cd /opt/explo && EXPLO_ISOLATED=1 WEB_DATA_PATH=\"$_pdir/\" $RUNNER ./explo --config \"$_penv\" $_flags >> /proc/1/fd/1 2>&1" >> /etc/crontabs/root
      echo "[setup] Registered job: $_pname/$_job"
      echo "        Schedule: $_sched"
    done
  done
fi

chmod 600 /etc/crontabs/root

echo "[setup] Starting cron..."

if [ "$EXECUTE_ON_START" = "true" ]; then
    echo "[setup] Executing startup task..."
    (cd /opt/explo && $RUNNER ./explo --config "$_cfg" $START_FLAGS) &
    wait $!
fi

crond -f -l 2 &
CROND_PID=$!
while kill -0 "$CROND_PID" 2>/dev/null; do
  if [ -f "$RESTART_FLAG" ]; then
    echo "[setup] Restart requested from the web UI, stopping container..."
    kill "$CROND_PID" 2>/dev/null
    exit 0
  fi
  sleep 2
done
