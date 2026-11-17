let csrfToken = null

// ---- profiles (multi-user): the selected profile is sent as ?profile=<name> ----
const PROFILE_KEY = 'explo_profile'

export function getProfile() {
  try { return localStorage.getItem(PROFILE_KEY) || 'default' } catch { return 'default' }
}

export function setProfile(name) {
  try { localStorage.setItem(PROFILE_KEY, name) } catch { /* ignore */ }
}

// Adds ?profile=<name> to /api/ui/ URLs that are profile-specific.
export function withProfile(url) {
  const profile = getProfile()
  if (profile === 'default') return url
  if (!url.startsWith('/api/ui/') || /^\/api\/ui\/(csrf|login|logout|auth|account|profiles|profile-labels|shared)/.test(url)) return url
  return url + (url.includes('?') ? '&' : '?') + 'profile=' + encodeURIComponent(profile)
}

export async function fetchProfiles() {
  const res = await apiFetch('/api/ui/profiles')
  return res.ok ? res.json() : ['default']
}

export async function createProfile(name) {
  const res = await apiFetch('/api/ui/profiles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) throw new Error((await res.text()).trim() || 'failed to create profile')
  return res.json()
}

async function ensureCSRF() {
  if (csrfToken) return csrfToken

  const res = await fetch('/api/ui/csrf', { credentials: 'include' })
  if (!res.ok) throw new Error('could not get a CSRF token')
  const data = await res.json()
  csrfToken = data.csrf_token
  return csrfToken
}

async function apiFetch(url, options = {}) {
  const method = (options.method || 'GET').toUpperCase()
  const mutating = ['POST', 'PUT', 'PATCH', 'DELETE'].includes(method)

  const send = async () => {
    const headers = new Headers(options.headers || {})
    if (mutating) headers.set('X-CSRF-Token', await ensureCSRF())
    return fetch(withProfile(url), { credentials: 'include', ...options, headers })
  }

  let res = await send()
  if (mutating && res.status === 403) {
    // the CSRF token is tied to the session: after a session expiry or a server restart it is stale
    csrfToken = null
    res = await send()
  }
  if (method !== 'GET' && res.ok) window.dispatchEvent(new Event('explo-changed'))
  return res
}

export async function checkAuth() {
  const res = await fetch('/api/ui/auth/status', { credentials: 'include' })
  // Older builds return an empty body, so tolerate a failed parse.
  const data = await res.json().catch(() => ({}))
  return { authenticated: res.ok, authDisabled: !!data.auth_disabled }
}

export async function login(username, password) {
  const form = new URLSearchParams()
  form.append('username', username)
  form.append('password', password)

  const res = await apiFetch('/api/ui/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body: form.toString(),
  })

  if (!res.ok) {
    throw new Error(await res.text())
  }
}

export async function fetchConfig() {
  const res = await apiFetch('/api/ui/config')
  return res.json()
}

let rawVersion = ''

export async function fetchConfigRaw() {
  const res = await apiFetch('/api/ui/config/raw')
  rawVersion = res.headers.get('X-Env-Version') || ''
  return res.text()
}

// Sends the edited text together with the version it was loaded from: the server refuses the save
// if the file changed meanwhile, so an old editor snapshot cannot overwrite newer settings.
export async function saveConfig(text) {
  const headers = { 'Content-Type': 'text/plain' }
  if (rawVersion) headers['X-Env-Version'] = rawVersion
  const res = await apiFetch('/api/ui/config', { method: 'POST', headers, body: text })
  if (!res.ok) throw new Error((await res.text()).trim() || 'save failed')
  await fetchConfigRaw().catch(() => {})
}

export async function resetConfig() {
  const res = await apiFetch('/api/ui/config/reset', { method: 'POST' })
  if (!res.ok) throw new Error(await res.text())
}

export async function saveSchedule(id, name, enabled, day, hour, minute) {
  const res = await apiFetch('/api/ui/config/schedules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, name, enabled, day, hour, minute }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function wizardStep1(user, userToken, playlists, discovery_mode) {
  const res = await apiFetch('/api/ui/wizard/step1', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user, userToken, playlists, discovery_mode }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function wizardStep2(body) {
  const res = await apiFetch('/api/ui/wizard/step2', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function wizardStep3(body) {
  const res = await apiFetch('/api/ui/wizard/step3', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function fetchBrowse(path) {
  const res = await apiFetch('/api/ui/browse?path=' + encodeURIComponent(path || '/'))
  return res.json()
}

export async function startRun(playlist, download_mode, replace = true) {
  const form = new FormData()
  form.set('playlist', playlist)
  form.set('download_mode', download_mode)
  form.set('replace_playlist', replace ? 'true' : 'false')
  const res = await apiFetch('/api/ui/run', { method: 'POST', body: form })
  if (res.status === 409) throw Object.assign(new Error('already running'), { conflict: true })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function stopRun() {
  const res = await apiFetch('/api/ui/run/stop', { method: 'POST' })
  if (!res.ok) throw new Error(await res.text())
}

export async function fetchRunStatus() {
  const res = await apiFetch('/api/ui/run/status')
  return res.json()
}

export async function fetchLogs() {
  const res = await apiFetch('/api/ui/logs')
  return res.text()
}

export async function prefetchPlaylists(user, playlists, options = {}) {
  await apiFetch('/api/ui/playlists/prefetch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user, playlists, ...options }),
  })
}

export async function logout() {
  await apiFetch('/api/ui/logout', { method: 'POST' })
}

export async function fetchSetupStatus() {
  try {
    const res = await fetch(withProfile('/api/ui/setup-status'))
    if (!res.ok) return null
    return res.json()
  } catch {
    return null
  }
}

export async function fetchCustomPlaylists() {
  const res = await apiFetch('/api/ui/custom-playlists')
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function importCustomPlaylist(url, source, refresh_days) {
  const res = await apiFetch('/api/ui/custom-playlists', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, source, refresh_days }),
  })
  if (res.status === 409) throw Object.assign(new Error('already imported'), { duplicate: true })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function deleteCustomPlaylist(id, { deleteTracks = false } = {}) {
  const qs = deleteTracks ? '?delete_tracks=true' : ''
  const res = await apiFetch(`/api/ui/custom-playlists/${encodeURIComponent(id)}${qs}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(await res.text())
}

export async function refreshCustomPlaylist(id) {
  const res = await apiFetch(`/api/ui/custom-playlists/${encodeURIComponent(id)}/refresh`, { method: 'POST' })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function savePathTemplate(template) {
  const res = await apiFetch('/api/ui/config/path-template', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ template }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function fetchPathTemplatePresets() {
  const res = await apiFetch('/api/ui/path-templates')
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function addPathTemplatePreset(name, template) {
  const res = await apiFetch('/api/ui/path-templates', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, template }),
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function deletePathTemplatePreset(name) {
  const res = await apiFetch(`/api/ui/path-templates/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(await res.text())
}

export async function saveEnrichMetadata(enabled) {
  const res = await apiFetch('/api/ui/config/enrich-metadata', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function saveReplacePlaylist(id, name, replace) {
  const res = await apiFetch('/api/ui/config/replace-playlist', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, name, replace }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function saveCleanDownloads(enabled) {
  const res = await apiFetch('/api/ui/config/clean-downloads', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function saveSuffixRemoval(enabled) {
  const res = await apiFetch('/api/ui/config/suffix-removal', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  })
  if (!res.ok) throw new Error(await res.text())
}

export async function fetchBackgroundArt() {
  try {
    const res = await fetch(withProfile('/api/ui/background-art'))
    if (!res.ok) return null
    const { url } = await res.json()
    return url || null
  } catch {
    return null
  }
}


// ---- custom playlist covers ----
export function coverUrl(id, bust = '') {
  const u = withProfile(`/api/ui/playlists/${encodeURIComponent(id)}/cover`)
  return bust ? u + (u.includes('?') ? '&' : '?') + 't=' + bust : u
}

export async function hasCover(id) {
  const res = await apiFetch(`/api/ui/playlists/${encodeURIComponent(id)}/cover`)
  return res.ok
}

export async function uploadCover(id, file) {
  const form = new FormData()
  form.append('image', file)
  const res = await apiFetch(`/api/ui/playlists/${encodeURIComponent(id)}/cover`, { method: 'POST', body: form })
  if (!res.ok) throw new Error((await res.text()).trim() || 'upload failed')
}

export async function deleteCover(id) {
  const res = await apiFetch(`/api/ui/playlists/${encodeURIComponent(id)}/cover`, { method: 'DELETE' })
  if (!res.ok) throw new Error((await res.text()).trim() || 'delete failed')
}


export async function saveConfigKeys(values) {
  const res = await apiFetch('/api/ui/config/keys', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ values }),
  })
  if (!res.ok) throw new Error((await res.text()).trim() || 'save failed')
}


// ---- YouTube cookies.txt ----
export async function cookiesPresent() {
  const res = await apiFetch('/api/ui/cookies')
  return res.ok ? (await res.json()).present : false
}

export async function uploadCookies(file) {
  const form = new FormData()
  form.append('file', file)
  const res = await apiFetch('/api/ui/cookies', { method: 'POST', body: form })
  if (!res.ok) throw new Error((await res.text()).trim() || 'upload failed')
}

export async function deleteCookies() {
  const res = await apiFetch('/api/ui/cookies', { method: 'DELETE' })
  if (!res.ok) throw new Error((await res.text()).trim() || 'delete failed')
}


// ---- profile rename / delete, restart ----
export async function renameProfile(oldName, newName) {
  const res = await apiFetch(`/api/ui/profiles/${encodeURIComponent(oldName)}/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: newName }),
  })
  if (!res.ok) throw new Error((await res.text()).trim() || 'rename failed')
  return res.json()
}

export async function deleteProfile(name) {
  const res = await apiFetch(`/api/ui/profiles/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error((await res.text()).trim() || 'delete failed')
  return res.json()
}

export async function restartNeeded() {
  try {
    const res = await apiFetch('/api/ui/restart')
    return res.ok ? (await res.json()).needed : false
  } catch { return false }
}

export async function restartNow() {
  const res = await apiFetch('/api/ui/restart', { method: 'POST' })
  if (!res.ok) throw new Error((await res.text()).trim() || 'restart failed')
}

// ---- shared admin account and profile display names ----
export async function fetchShared() {
  const res = await apiFetch('/api/ui/shared')
  if (!res.ok) return {}
  return (await res.json()).values || {}
}

export async function saveShared(values) {
  const res = await apiFetch('/api/ui/shared', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ values }),
  })
  if (!res.ok) throw new Error((await res.text()).trim() || 'save failed')
}

export async function fetchProfileLabels() {
  try {
    const res = await apiFetch('/api/ui/profile-labels')
    return res.ok ? await res.json() : {}
  } catch { return {} }
}

export async function setProfileLabel(name, label) {
  const res = await apiFetch(`/api/ui/profiles/${encodeURIComponent(name)}/label`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label }),
  })
  if (!res.ok) throw new Error((await res.text()).trim() || 'rename failed')
}

// ---- web UI login (name + password of the Explo web interface itself) ----
export async function fetchAccount() {
  const res = await apiFetch('/api/ui/account')
  if (!res.ok) return { username: '', auth_disabled: false }
  return res.json()
}

export async function changeAccount({ current, username, password }) {
  const res = await apiFetch('/api/ui/account', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current_password: current, username, new_password: password }),
  })
  if (!res.ok) throw new Error((await res.text()).trim() || 'could not change the login')
}

// ---- Spotify login cookie (sp_dc): lets Explo read personal playlists such as Blend and Daily Mix ----
export async function spotifyCookiePresent() {
  const res = await apiFetch('/api/ui/spotify-cookie')
  if (!res.ok) return false
  return !!(await res.json()).present
}

export async function saveSpotifyCookie({ file, value }) {
  const form = new FormData()
  if (file) form.set('file', file)
  if (value) form.set('value', value)
  const res = await apiFetch('/api/ui/spotify-cookie', { method: 'POST', body: form })
  if (!res.ok) throw new Error((await res.text()).trim() || 'could not save the cookie')
}

export async function deleteSpotifyCookie() {
  const res = await apiFetch('/api/ui/spotify-cookie', { method: 'DELETE' })
  if (!res.ok) throw new Error((await res.text()).trim() || 'could not remove the cookie')
}
