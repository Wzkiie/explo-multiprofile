/**
 * Settings.jsx
 *
 * Main app view after initial setup. Three tabs: Home, Settings, Logs.
 * Each section fetches its own data directly from the API — no prop-drilling.
 *
 * Sections:
 *   HomeSection    — scheduled playlists, manual run, live output
 *   ConfigSection  — raw .env editor, wizard re-run, reset
 *   LogsSection    — full server log viewer
 */

import { useState, useEffect, useCallback, useRef } from 'react'
import {
  fetchConfig, fetchConfigRaw, saveConfig, resetConfig,
  saveSchedule, startRun, stopRun, fetchRunStatus, fetchLogs,
  fetchCustomPlaylists, deleteCustomPlaylist, savePathTemplate, saveEnrichMetadata,
  saveReplacePlaylist, saveCleanDownloads, saveSuffixRemoval, saveConfigKeys, fetchShared, saveShared, fetchProfileLabels, setProfileLabel, fetchAccount, changeAccount, spotifyCookiePresent, saveSpotifyCookie, deleteSpotifyCookie,
  cookiesPresent, uploadCookies, deleteCookies,
  renameProfile, deleteProfile, restartNeeded, restartNow,
  fetchPathTemplatePresets, addPathTemplatePreset, deletePathTemplatePreset,
  getProfile, setProfile, withProfile, fetchProfiles, createProfile,
} from '../lib/api'
import { parseSlogLine, cronToFields, highlightEnv } from '../lib/utils'
import { fetchPlaylistTracks, clearPlaylistCache } from '../lib/listenbrainz'
import { motion, AnimatePresence } from 'motion/react'
import { Toggle } from './ui/Toggle'
import { Button, SectionLabel, Panel, LogRow } from './ui/common'
import { PlaylistCard, TracklistDropdown } from './ui/PlaylistCard'
import { ImportModal } from './ui/ImportModal'
import { SEED_PRESETS, PathLine, PathTemplateModal } from './ui/PathTemplateModal'
import { UpdateNotification } from './ui/UpdateNotification'

const tabBtnCls = active =>
  `bg-transparent border-none border-b-2 pb-2 px-3.5 text-[13px] leading-none cursor-pointer transition-colors
  ${active ? 'text-accent border-accent' : 'text-muted border-transparent hover:text-white'}`

// ── Home Tab ──────────────────────────────────────────────────────────────────
// Manages scheduled playlists, manual runs, and live run output.
// Fetches its own config on mount to initialise schedule state.

// Streams live run output from /api/ui/run/events
function useSSE({ onLine, onDone }) {
  const abortRef = useRef(null)

  const connect = useCallback(async () => {
    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller
    try {
      const res = await fetch(withProfile('/api/ui/run/events'), { credentials: 'include', signal: controller.signal })
      if (!res.ok) { onDone(null); return }
      const reader = res.body.getReader()
      const dec = new TextDecoder()
      let buf = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += dec.decode(value, { stream: true })
        const parts = buf.split('\n\n')
        buf = parts.pop()
        for (const part of parts) {
          let ev = '', data = ''
          for (const l of part.split('\n')) {
            if (l.startsWith('event: ')) ev = l.slice(7).trim()
            if (l.startsWith('data: ')) data = l.slice(6)
          }
          if (ev === 'done') { onDone(parseInt(data)); return }
          else if (data) onLine(data)
        }
      }
    } catch (e) {
      if (e.name !== 'AbortError') onDone(null)
    } finally {
      if (abortRef.current === controller) abortRef.current = null
    }
  }, [onLine, onDone])

  const disconnect = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  return { connect, disconnect }
}

const PLAYLISTS = [
  { value: 'weekly-exploration', name: 'Weekly Exploration', scheduleKey: 'WEEKLY_EXPLORATION_SCHEDULE', flagsKey: 'WEEKLY_EXPLORATION_FLAGS', defaultDay: 2,  defaultHour: 0,  defaultMinute: 15 },
  { value: 'weekly-jams',        name: 'Weekly Jams',        scheduleKey: 'WEEKLY_JAMS_SCHEDULE',        flagsKey: 'WEEKLY_JAMS_FLAGS',        defaultDay: 1,  defaultHour: 0,  defaultMinute: 30 },
  { value: 'daily-jams',         name: 'Daily Jams',         scheduleKey: 'DAILY_JAMS_SCHEDULE',         flagsKey: 'DAILY_JAMS_FLAGS',         defaultDay: -1, defaultHour: 1,  defaultMinute: 15 },
  { value: 'on-repeat',          name: 'On Repeat',          scheduleKey: 'ON_REPEAT_SCHEDULE',          flagsKey: 'ON_REPEAT_FLAGS',          defaultDay: 100, defaultHour: 12, defaultMinute: 0, fixedSchedule: true },
  { value: 'fresh-releases',     name: 'Fresh Releases',     scheduleKey: 'FRESH_RELEASES_SCHEDULE',     flagsKey: 'FRESH_RELEASES_FLAGS',     defaultDay: 3,  defaultHour: 8,  defaultMinute: 30 },
]

const SCHEDULE_DAYS = [
  { value: -2,   label: 'Never',           summary: 'No schedule' },
  { value: -1,   label: 'Every day',       summary: 'Every day' },
  { value: 0,    label: 'Sunday',          summary: 'Every Sunday' },
  { value: 1,    label: 'Monday',          summary: 'Every Monday' },
  { value: 2,    label: 'Tuesday',         summary: 'Every Tuesday' },
  { value: 3,    label: 'Wednesday',       summary: 'Every Wednesday' },
  { value: 4,    label: 'Thursday',        summary: 'Every Thursday' },
  { value: 5,    label: 'Friday',          summary: 'Every Friday' },
  { value: 6,    label: 'Saturday',        summary: 'Every Saturday' },
  { value: 100,  label: 'Monthly (1st)',   summary: 'Every 1st of the month' },
]

const selectCls = 'bg-surface border border-ui-border text-white rounded-[6px] px-2.5 py-1.5 text-[13px] cursor-pointer outline-none focus:border-accent'

function TracklistSlide({ show, slideKey, children }) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          key={slideKey}
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: 'auto' }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.28, ease: 'easeInOut' }}
          style={{ overflow: 'hidden' }}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  )
}

function CustomPlaylistsSection({
  customPlaylists,
  schedules,
  scheduleProps,
  openTracklist,
  setOpenTracklist,
  lbUser,
  onImportedRefresh,
  onSync,
  onDelete,
  showImportModal,
  setShowImportModal,
  refreshTick = 0,
}) {
  return (
    <div className="mt-6">
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div className="text-[16px] font-bold tracking-tight text-white">Custom Playlists</div>
        <button
          onClick={() => setShowImportModal(true)}
          className="bg-transparent border border-ui-border text-muted rounded-full px-3 py-1 text-[12px] cursor-pointer hover:text-white hover:border-[#444] transition-colors"
        >
          + Import
        </button>
      </div>

      {customPlaylists.length === 0 ? (
        <p className="text-[12px] text-muted mt-3">
          No custom playlists yet. Import one from ListenBrainz, Apple Music or Spotify.
        </p>
      ) : (
        <div className="grid grid-cols-1 min-[420px]:grid-cols-2 min-[720px]:grid-cols-4 gap-3 mt-3">
          {customPlaylists.map((cp, i) => {
            if (!schedules[cp.id]) return null
            return (
              <PlaylistCard
                key={cp.id}
                playlist={{ value: `custom-${cp.color_index ?? i}`, name: cp.name }}
                trackId={cp.id}
                artworkUrl={cp.artwork_url || undefined}
                {...scheduleProps(cp.id)}
                index={i}
                nextRunText={schedules[cp.id]?.enabled
                  ? SCHEDULE_DAYS.find(d => d.value === schedules[cp.id].day)?.summary ?? 'Every day'
                  : 'Disabled'}
                tracklistOpen={openTracklist === cp.id}
                onTracklistToggle={() => setOpenTracklist(v => v === cp.id ? null : cp.id)}
                sourceUrl={cp.source_url || undefined}
                onDelete={(opts) => onDelete(cp.id, opts)}
                refreshTick={refreshTick}
              />
            )
          })}
        </div>
      )}

      <TracklistSlide show={openTracklist && openTracklist.startsWith('custom-')} slideKey={openTracklist}>
        <TracklistDropdown
          playlist={openTracklist}
          lbUser={null}
          onRun={() => onSync(openTracklist)}
          refreshTick={refreshTick}
        />
      </TracklistSlide>

      <AnimatePresence>
        {showImportModal && (
          <ImportModal
            onClose={() => setShowImportModal(false)}
            onImported={onImportedRefresh}
            onSync={onSync}
          />
        )}
      </AnimatePresence>
    </div>
  )
}

const WEEK = [
  { d: 1, label: 'Mon' }, { d: 2, label: 'Tue' }, { d: 3, label: 'Wed' }, { d: 4, label: 'Thu' },
  { d: 5, label: 'Fri' }, { d: 6, label: 'Sat' }, { d: 0, label: 'Sun' },
]
const pad2 = n => String(n).padStart(2, '0')

function ScheduleOverview({ schedules, customPlaylists }) {
  const items = [
    ...PLAYLISTS.map(p => ({ id: p.value, name: p.name })),
    ...customPlaylists.map(cp => ({ id: cp.id, name: cp.name })),
  ].map(it => ({ ...it, s: schedules[it.id] })).filter(it => it.s?.enabled)

  const scheduled = items.filter(it => it.s.day !== -2)
  const manualOnly = items.filter(it => it.s.day === -2)
  const monthly = scheduled.filter(it => it.s.day === 100)
  const forDay = d => scheduled
    .filter(it => it.s.day === d || it.s.day === -1)
    .sort((a, b) => (a.s.hour * 60 + a.s.minute) - (b.s.hour * 60 + b.s.minute))

  const Chip = ({ it }) => (
    <div className="rounded-[6px] bg-well border border-ui-border px-2 py-1 mb-1.5">
      <div className="text-[11px] text-white leading-tight break-words" title={it.name}>{it.name}</div>
      <div className="text-[11px] text-accent tabular-nums">{pad2(it.s.hour)}:{pad2(it.s.minute)}</div>
    </div>
  )

  const steps = [
    ['1', 'On schedule', 'Explo starts by itself at the set time.'],
    ['2', 'Fetch & download', 'It gets your recommendations from ListenBrainz and downloads songs you don\'t have yet.'],
    ['3', 'Update playlist', 'The playlist in your music app is rebuilt. Old songs stay.'],
  ]

  return (
    <div className="mb-5">
      <div className="flex gap-2.5 flex-wrap mb-4">
        {steps.map(([n, title, text]) => (
          <div key={n} className="flex-1 min-w-[170px] flex gap-2.5 items-start">
            <span className="shrink-0 w-5 h-5 rounded-full bg-accent text-black text-[11px] font-bold flex items-center justify-center mt-0.5">{n}</span>
            <div>
              <div className="text-[12px] text-white">{title}</div>
              <div className="text-[11px] text-muted">{text}</div>
            </div>
          </div>
        ))}
      </div>

      <div className="rounded-[10px] border border-ui-border p-3">
        <div className="grid grid-cols-7 gap-2">
          {WEEK.map(({ d, label }) => (
            <div key={d} className="min-w-0">
              <div className="text-[11px] uppercase tracking-wide text-muted mb-1.5">{label}</div>
              {forDay(d).map(it => <Chip key={it.id} it={it} />)}
              {forDay(d).length === 0 && <div className="text-[11px] text-[#444]">—</div>}
            </div>
          ))}
        </div>
        {monthly.length > 0 && (
          <div className="mt-2 pt-2 border-t border-ui-border flex gap-2 flex-wrap items-center">
            <span className="text-[11px] uppercase tracking-wide text-muted">1st of month</span>
            {monthly.map(it => <span key={it.id} className="text-[11px] text-white">{it.name} <span className="text-accent">{pad2(it.s.hour)}:{pad2(it.s.minute)}</span></span>)}
          </div>
        )}
        {scheduled.length === 0 && (
          <div className="text-[12px] text-muted mt-2">Nothing scheduled yet. Open a playlist’s ⋮ menu → Edit Schedule, or run one manually below.</div>
        )}
        {manualOnly.length > 0 && (
          <div className="mt-2 pt-2 border-t border-ui-border text-[11px] text-muted">
            Manual only: {manualOnly.map(it => it.name).join(', ')}
          </div>
        )}
      </div>
      <p className="text-[11px] text-muted mt-2">
        Times use the container’s timezone (<code>TZ</code>). Schedule changes need a restart, you will see a button for it.
      </p>
    </div>
  )
}

function PlaylistHandling() {
  const [items, setItems] = useState([])   // [{id, name, replace}]
  const [loaded, setLoaded] = useState(false)
  const [keepN, setKeepN] = useState('0')
  const [keepErr, setKeepErr] = useState('')

  useEffect(() => {
    Promise.all([fetchConfig(), fetchCustomPlaylists().catch(() => [])]).then(([{ values }, customs]) => {
      setKeepN(String(parseInt(values.KEEP_VERSIONS, 10) || 0))
      const list = []
      for (const p of PLAYLISTS) {
        if (!values[p.scheduleKey] && !values[p.flagsKey]) continue
        list.push({ id: p.value, name: p.name, replace: !(values[p.flagsKey] || '').includes('--replace-playlist=false') })
      }
      for (const cp of customs) {
        if (!cp.flags && !cp.schedule) continue
        list.push({ id: cp.id, name: cp.name, replace: !(cp.flags || '').includes('--replace-playlist=false') })
      }
      setItems(list)
      setLoaded(true)
    })
  }, [])

  const mode = items.length === 0 ? null : items.every(i => i.replace) ? 'replace' : items.every(i => !i.replace) ? 'keep' : 'mixed'

  const setAll = async (next) => {
    setItems(prev => prev.map(i => ({ ...i, replace: next })))
    // one after the other: parallel saves of the same config file used to overwrite each other
    for (const i of items) {
      try { await saveReplacePlaylist(i.id, i.name, next) } catch {
        setItems(prev => prev.map(x => x.id === i.id ? { ...x, replace: !next } : x))
      }
    }
  }

  const changeKeep = (v) => {
    const prev = keepN
    setKeepN(v); setKeepErr('')
    saveConfigKeys({ KEEP_VERSIONS: v }).catch(e => { setKeepN(prev); setKeepErr(e.message) })
  }

  const Option = ({ value, title, text }) => (
    <button
      type="button"
      onClick={() => setAll(value === 'replace')}
      className={`text-left flex-1 min-w-[220px] rounded-[8px] border px-3.5 py-3 cursor-pointer bg-transparent transition-colors ${mode === value ? 'border-accent' : 'border-ui-border hover:border-[#444]'}`}
    >
      <div className="flex items-center gap-2 text-[13px] text-white">
        <span className={`inline-block w-3 h-3 rounded-full border ${mode === value ? 'bg-accent border-accent' : 'border-[#555]'}`} />
        {title}
      </div>
      <div className="text-[11px] text-muted mt-1.5">{text}</div>
    </button>
  )

  return (
    <div className="mt-6">
      <SectionLabel>Playlists</SectionLabel>
      <div className="text-[13px] text-white mb-1">What happens to a playlist when it is regenerated?</div>
      <p className="text-[12px] text-muted mb-3">
        Songs you already downloaded always stay in your library, unless you turn on “Delete old song files” further down.
      </p>
      <div className="flex gap-3 flex-wrap">
        <Option value="replace" title="Replace the old playlist (recommended)"
          text="Always exactly one playlist per name. The previous version is removed, its songs stay in your library." />
        <Option value="keep" title="Keep every version"
          text="Each run adds a new dated playlist next to the old ones, e.g. Weekly-Jams-2026-11-16. Applies to custom playlists too." />
      </div>
      {mode !== 'replace' && (
        <div className="mt-3 flex items-center gap-2 text-[12px] text-muted">
          <span>Keep versions:</span>
          <select value={keepN} onChange={e => changeKeep(e.target.value)}
            className="bg-transparent border border-ui-border rounded-[6px] text-white text-[12px] px-2 py-1">
            <option value="0">all of them</option>
            {[...new Set([1, 2, 3, 5, 7, 14, 30, parseInt(keepN, 10) || 0])].filter(n => n > 0).sort((x, y) => x - y).map(n => <option key={n} value={n}>newest {n}</option>)}
          </select>
          <span>older dated copies are deleted automatically (Navidrome/Subsonic only).</span>
        </div>
      )}
      {keepErr && <p className="text-[11px] text-red-400 mt-1">{keepErr}</p>}
      {loaded && items.length === 0 && <p className="text-[11px] text-muted mt-2">No playlist is scheduled yet — set one up on the Home tab first.</p>}
      {mode === 'mixed' && <p className="text-[11px] text-muted mt-2">Currently set differently per playlist. Pick an option to apply it to all of them.</p>}
    </div>
  )
}

function HomeSection() {
  const [schedules, setSchedules] = useState(null)
  const [replacePlaylists, setReplacePlaylists] = useState({})
  const [scheduleSaveStatus, setScheduleSaveStatus] = useState({})
  const [lbUser, setLbUser] = useState('')
  const [openTracklist, setOpenTracklist] = useState(null)
  const [customPlaylists, setCustomPlaylists] = useState([])
  const [showImportModal, setShowImportModal] = useState(false)

  const [playlist, setPlaylist] = useState('weekly-exploration')
  const [dlmode, setDlmode] = useState('normal')

  const [running, setRunning] = useState(false)
  const [status, setStatus] = useState('')
  const [logEntries, setLogEntries] = useState([])
  const [rawLog, setRawLog] = useState(false)
  const logRef = useRef(null)

  const [refreshTick, setRefreshTick] = useState(0)

  useEffect(() => {
    Promise.all([
      fetchConfig(),
      fetchCustomPlaylists().catch(() => [])
    ]).then(([{ values }, customList]) => {
      setLbUser(values.LISTENBRAINZ_USER || '')
      setCustomPlaylists(customList)

      const rp = {}
      for (const p of PLAYLISTS) {
        rp[p.value] = !(values[p.flagsKey] || '').includes('--replace-playlist=false')
      }
      for (const cp of customList) {
        rp[cp.id] = !(cp.flags || '').includes('--replace-playlist=false')
      }
      setReplacePlaylists(rp)

      const s = {}
      for (const p of PLAYLISTS) {
        const cron = values[p.scheduleKey]
        s[p.value] = cron
          ? { enabled: true, editing: false, ...cronToFields(cron) }
          : { enabled: false, day: p.defaultDay, hour: p.defaultHour, minute: p.defaultMinute, editing: false }
      }
      for (const cp of customList) {
        s[cp.id] = cp.schedule
          ? { name: cp.name, enabled: true, editing: false, ...cronToFields(cp.schedule) }
          : cp.flags
            ? { name: cp.name, enabled: true, editing: false, day: -2, hour: 4, minute: 0 }
            : { name: cp.name, enabled: false, day: -1, hour: 4, minute: 0, editing: false }
      }
      setSchedules(s)
    })
  }, [])

  const onLine = useCallback(data => {
    setLogEntries(prev => [...prev, { raw: data, ...parseSlogLine(data) }])
    requestAnimationFrame(() => {
      if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
    })
  }, [])

  const onDone = useCallback(code => {
    setStatus(code === 0 ? 'done ✓' : code === null ? 'error' : `failed (exit ${code})`)
    setRunning(false)
    if (code !== null) {
      clearPlaylistCache()
      setRefreshTick(t => t + 1)
    }
  }, [])

  const { connect, disconnect } = useSSE({ onLine, onDone })

  useEffect(() => {
    fetchRunStatus().then(s => {
      if (s.running) {
        setRunning(true)
        setStatus('running…')
        setLogEntries([])
        connect()
      }
    })
    return () => disconnect()
  }, [connect, disconnect])

  const nextRunText = id => {
    const s = schedules[id]
    if (!s?.enabled) return 'Disabled'
    return SCHEDULE_DAYS.find(d => d.value === s.day)?.summary || 'Every day'
  }

  const flashStatus = (id, msg) => {
    setScheduleSaveStatus(prev => ({ ...prev, [id]: msg }))
    if (msg === 'Saved.') setTimeout(() => setScheduleSaveStatus(prev => ({ ...prev, [id]: '' })), 2000)
  }

  const scheduleProps = id => {
    const s = schedules[id]
    return {
      schedule: s,
      scheduleSaveStatus: scheduleSaveStatus[id] || '',
      onToggle: v => {
        setSchedules(prev => ({ ...prev, [id]: { ...prev[id], enabled: v } }))
        saveSchedule(id, s.name, v, s.day, s.hour, s.minute)
          .then(() => flashStatus(id, 'Saved.'))
          .catch(() => flashStatus(id, 'Error saving.'))
      },
      onToggleEdit: () => setSchedules(prev => ({
        ...prev, [id]: { ...prev[id], editing: !prev[id].editing }
      })),
      onSave: () => {
        saveSchedule(id, s.name, s.enabled, s.day, s.hour, s.minute)
          .then(() => flashStatus(id, 'Saved.'))
          .catch(() => flashStatus(id, 'Error saving.'))
        setSchedules(prev => ({ ...prev, [id]: { ...prev[id], editing: false } }))
      },
      onCancelEdit: () => setSchedules(prev => ({
        ...prev, [id]: { ...prev[id], editing: false }
      })),
      replacePlaylist: replacePlaylists[id] ?? true,
      onReplaceToggle: () => {
        const next = !(replacePlaylists[id] ?? true)
        setReplacePlaylists(prev => ({ ...prev, [id]: next }))
        saveReplacePlaylist(id, s.name, next).catch(() =>
          setReplacePlaylists(prev => ({ ...prev, [id]: !next }))
        )
      },
      onDayChange: day => setSchedules(prev => ({
        ...prev, [id]: { ...prev[id], day }
      })),
      onTimeChange: val => {
        const [h = '00', m = '00'] = val.split(':')
        setSchedules(prev => ({
          ...prev, [id]: { ...prev[id], hour: parseInt(h) || 0, minute: parseInt(m) || 0 }
        }))
      },
    }
  }

  const handleRun = async () => {
    setRunning(true)
    setLogEntries([])
    setStatus('running…')
    try {
      await startRun(playlist, dlmode, replacePlaylists[playlist] ?? true)
      connect()
    } catch (e) {
      if (e.conflict) { setStatus('already running'); setRunning(false); return }
      setStatus('error')
      setRunning(false)
    }
  }

  const handleStop = async () => {
    setStatus('stopping…')
    try { await stopRun() }
    catch { setStatus('error stopping run') }
  }

  if (!schedules) return null

  return (
    <div>
      {/* Scheduled Playlists */}
      <div className="mt-6">
        <div className="text-[16px] font-bold tracking-tight text-white mb-3.5">Scheduled Playlists</div>
        <ScheduleOverview schedules={schedules} customPlaylists={customPlaylists} />
        <div className="grid grid-cols-1 min-[420px]:grid-cols-2 min-[720px]:grid-cols-4 gap-3 mt-3">
          {PLAYLISTS.map((p, i) => (
            <PlaylistCard
              key={p.value}
              playlist={p}
              {...scheduleProps(p.value)}
              fixedSchedule={!!p.fixedSchedule}
              index={i}
              nextRunText={nextRunText(p.value)}
              tracklistOpen={openTracklist === p.value}
              onTracklistToggle={() => setOpenTracklist(v => v === p.value ? null : p.value)}
              refreshTick={refreshTick}
            />
          ))}
        </div>
        <TracklistSlide show={openTracklist && PLAYLISTS.some(p => p.value === openTracklist)} slideKey={openTracklist}>
          <TracklistDropdown
            lbUser={lbUser}
            playlist={openTracklist}
            refreshTick={refreshTick}
            onRun={async () => {
              await startRun(openTracklist, 'normal', replacePlaylists[openTracklist] ?? true)
              setRunning(true)
              setStatus('running…')
              setLogEntries([])
              connect()
            }}
          />
        </TracklistSlide>
      </div>

      {/* Custom Playlists */}
      <CustomPlaylistsSection
        customPlaylists={customPlaylists}
        refreshTick={refreshTick}
        schedules={schedules}
        scheduleProps={scheduleProps}
        openTracklist={openTracklist}
        setOpenTracklist={setOpenTracklist}
        lbUser={lbUser}
        showImportModal={showImportModal}
        setShowImportModal={setShowImportModal}
        onImportedRefresh={() => {
          fetchCustomPlaylists().then(list => {
            setCustomPlaylists(list)
            setSchedules(prev => {
              const next = { ...prev }
              for (const cp of list) {
                if (next[cp.id]) continue
                next[cp.id] = cp.schedule
                  ? { enabled: true, editing: false, ...cronToFields(cp.schedule) }
                  : cp.flags
                    ? { enabled: true, editing: false, day: -2, hour: 4, minute: 0 }
                    : { enabled: false, day: -1, hour: 4, minute: 0, editing: false }
              }
              return next
            })
          }).catch(() => {})
          setShowImportModal(false)
        }}
        onSync={async (id) => {
          await startRun(id, 'normal', replacePlaylists[id] ?? true)
          setRunning(true)
          setStatus('running…')
          setLogEntries([])
          connect()
        }}
        onDelete={async (id, opts) => {
          try {
            await deleteCustomPlaylist(id, opts)
            setCustomPlaylists(prev => prev.filter(p => p.id !== id))
            setSchedules(prev => { const next = { ...prev }; delete next[id]; return next })
            if (openTracklist === id) setOpenTracklist(null)
          } catch {}
        }}
      />

      {/* Manual Run */}
      <div className="mt-6">
        <div className="text-[16px] font-bold tracking-tight text-white mb-3.5">Manual Run</div>
        <div className="flex flex-col gap-1.5 mb-3">
          <label className="text-[12px] text-muted">Download mode</label>
          <div className="flex gap-1.5">
            {[
              { value: 'normal', name: 'Normal', desc: "Download only if the track isn't found locally" },
              { value: 'skip',   name: 'Skip',   desc: 'No downloads — builds a playlist from tracks already in your library. Good for testing.' },
              { value: 'force',  name: 'Force',  desc: 'Always download, ignoring local tracks' },
            ].map(m => (
              <button
                key={m.value}
                onClick={() => setDlmode(m.value)}
                className={`px-3 py-1.5 text-[12px] rounded-[6px] border bg-well cursor-pointer transition-colors
                  ${dlmode === m.value ? 'border-accent text-accent' : 'border-ui-border text-muted hover:border-[#404040] hover:text-white'}`}
              >
                {m.name}
              </button>
            ))}
          </div>
          <p className="text-[11px] text-muted">
            {({ normal: "Download only if the track isn't found locally", skip: 'No downloads — builds a playlist from tracks already in your library. Good for testing.', force: 'Always download, ignoring local tracks' })[dlmode]}
          </p>
        </div>
        <div className="flex gap-2.5 items-center flex-wrap mb-2.5">
          <label className="text-[12px] text-muted">Playlist</label>
          <select className={selectCls} value={playlist} onChange={e => setPlaylist(e.target.value)}>
            {PLAYLISTS.map(p => <option key={p.value} value={p.value}>{p.name}</option>)}
            {customPlaylists.length > 0 && <option disabled>---</option>}
            {customPlaylists.map(cp => <option key={cp.id} value={cp.id}>{cp.name}</option>)}
          </select>
        </div>
        <div className="flex gap-2.5 items-center">
          <Button onClick={handleRun} disabled={running}>▶ Run</Button>
          {running && (
            <button
              onClick={handleStop}
              className="bg-transparent border border-danger text-danger rounded-[6px] px-[18px] py-[7px] text-[13px] cursor-pointer hover:bg-danger hover:text-white transition-colors"
            >
              ■ Stop
            </button>
          )}
          <span className="text-[12px] text-muted">{status}</span>
        </div>
      </div>

      {/* Output */}
      <div className="mt-6">
        <div className="flex items-center justify-between mb-3.5">
          <SectionLabel className="">Output</SectionLabel>
          <label className="flex items-center gap-1.5 text-[12px] text-muted cursor-pointer">
            <input type="checkbox" checked={rawLog} onChange={e => setRawLog(e.target.checked)} /> Raw
          </label>
        </div>
        <Panel ref={logRef} className="w-full h-[300px]">
          {logEntries.map((e, i) => (
            rawLog
              ? <div key={i} className="font-mono text-[11px] text-accent whitespace-pre-wrap break-all py-px">{e.raw}</div>
              : <LogRow key={i} entry={e} />
          ))}
        </Panel>
      </div>
    </div>
  )
}

// ── Download Path Tab ─────────────────────────────────────────────────────────
// Profile-picker for the PATH_TEMPLATE env key. Users select a preset folder
// structure (or author their own via the modal) and apply it. Pending changes
// are previewed inline before being written to .env via the confirm bar.


function DownloadPathSection() {
  const [profiles, setProfiles] = useState(() => SEED_PRESETS.map(p => ({ ...p, seed: true })))
  const [appliedIdx, setAppliedIdx] = useState(null)
  const [selectedIdx, setSelectedIdx] = useState(null)
  const [loaded, setLoaded] = useState(false)
  const [saveStatus, setSaveStatus] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [openMenuIdx, setOpenMenuIdx] = useState(null)
  const [enrichEnabled, setEnrichEnabled] = useState(false)
  const [cleanDownloads, setCleanDownloads] = useState(false)
  const [templateEnabled, setTemplateEnabled] = useState(false)
  const [suffixRemoval, setSuffixRemoval] = useState(false)

  useEffect(() => {
    Promise.all([
      fetchConfig(),
      fetchPathTemplatePresets().catch(() => []),
    ]).then(([{ values }, jsonPresets]) => {
      const allProfiles = [
        ...SEED_PRESETS.map(p => ({ ...p, seed: true })),
        ...jsonPresets,
      ]
      setSuffixRemoval(values.SUFFIX_REMOVAL === 'true')
      setEnrichEnabled(values.ENRICH_TRACK_METADATA === 'true')
      const anyFlags = values.WEEKLY_EXPLORATION_FLAGS || values.WEEKLY_JAMS_FLAGS || values.DAILY_JAMS_FLAGS || values.ON_REPEAT_FLAGS || ''
      setCleanDownloads(anyFlags.includes('--clean-downloads'))
      const t = values.PATH_TEMPLATE || ''
      if (t) {
        setTemplateEnabled(true)
        const idx = allProfiles.findIndex(p => p.template === t)
        if (idx >= 0) {
          setProfiles(allProfiles)
          setAppliedIdx(idx)
          setSelectedIdx(idx)
        } else {
          const customIdx = allProfiles.length
          setProfiles([...allProfiles, { name: 'Custom', template: t }])
          setAppliedIdx(customIdx)
          setSelectedIdx(customIdx)
        }
      } else {
        setProfiles(allProfiles)
      }
      setLoaded(true)
    })
  }, [])

  const handleEnrichToggle = async () => {
    const next = !enrichEnabled
    setEnrichEnabled(next)
    try { await saveEnrichMetadata(next) } catch { setEnrichEnabled(!next) }
  }

  const handleCleanToggle = async () => {
    const next = !cleanDownloads
    setCleanDownloads(next)
    try { await saveCleanDownloads(next) } catch { setCleanDownloads(!next) }
  }

  const handleTemplateToggle = async () => {
    if (templateEnabled) {
      setTemplateEnabled(false)
      setAppliedIdx(null)
      setSelectedIdx(null)
      try { await savePathTemplate('') } catch { setTemplateEnabled(true) }
    } else {
      setTemplateEnabled(true)
      if (selectedIdx === null) setSelectedIdx(0)
    }
  }

  const handleSuffixToggle = async () => {
    const next = !suffixRemoval
    setSuffixRemoval(next)
    try { await saveSuffixRemoval(next) } catch { setSuffixRemoval(!next) }
  }

  useEffect(() => {
    if (!showModal) return
    const handle = e => { e.preventDefault(); e.returnValue = '' }
    window.addEventListener('beforeunload', handle)
    return () => window.removeEventListener('beforeunload', handle)
  }, [showModal])

  useEffect(() => {
    if (openMenuIdx === null) return
    const handle = () => setOpenMenuIdx(null)
    document.addEventListener('click', handle)
    return () => document.removeEventListener('click', handle)
  }, [openMenuIdx])

  const dirty = selectedIdx !== appliedIdx

  if (!loaded) return null

  const handleDeleteProfile = async (i) => {
    const profile = profiles[i]
    if (!profile.seed) {
      try { await deletePathTemplatePreset(profile.name) } catch {}
    }
    const newApplied = appliedIdx === i ? null : appliedIdx !== null && appliedIdx > i ? appliedIdx - 1 : appliedIdx
    const newSelected = selectedIdx === i ? newApplied : selectedIdx !== null && selectedIdx > i ? selectedIdx - 1 : selectedIdx
    setProfiles(prev => prev.filter((_, j) => j !== i))
    setAppliedIdx(newApplied)
    setSelectedIdx(newSelected)
    setOpenMenuIdx(null)
  }
  const previewTemplate = selectedIdx !== null
    ? (profiles[selectedIdx]?.template ?? SEED_PRESETS[0].template)
    : SEED_PRESETS[0].template

  const handleSave = async () => {
    const t = selectedIdx !== null ? profiles[selectedIdx].template : ''
    try {
      await savePathTemplate(t)
      setAppliedIdx(selectedIdx)
      setSaveStatus('Saved.')
      setTimeout(() => setSaveStatus(''), 2500)
    } catch {
      setSaveStatus('Error saving.')
    }
  }

  const handleSavePreset = async ({ name, template }) => {
    try { await addPathTemplatePreset(name, template) } catch {}
    const newIdx = profiles.length
    setProfiles(prev => [...prev, { name, template }])
    setSelectedIdx(newIdx)
    setShowModal(false)
  }

  return (
    <div className="mt-6">
      <SectionLabel>Folder Structure</SectionLabel>
      {/* ENRICH_TRACK_METADATA toggle */}
      <div className="flex items-start justify-between mt-3 mb-1 gap-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-[13px] text-white">Auto-tag songs</span>
          <span className="text-[11px] text-muted">Looks up track numbers, year, genre & more from MusicBrainz and writes them to downloaded files. Applies to scheduled playlists only — not custom imports.</span>
        </div>
        <button
          role="switch"
          aria-checked={enrichEnabled}
          onClick={handleEnrichToggle}
          className={`relative inline-flex h-[22px] w-10 shrink-0 cursor-pointer rounded-full transition-colors duration-200 ${enrichEnabled ? 'bg-accent' : 'bg-[#383838]'}`}
        >
          <span className={`inline-block h-[18px] w-[18px] my-[2px] rounded-full bg-white shadow transition-transform duration-200 ${enrichEnabled ? 'translate-x-[20px]' : 'translate-x-[2px]'}`} />
        </button>
      </div>

      {/* Clean old downloads toggle */}
      <div className="flex items-start justify-between mt-3 mb-1 gap-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-[13px] text-white">Delete old song files before each run</span>
          <span className="text-[11px] text-muted">Off (recommended): songs from earlier runs stay in your library. On: files in Explo's download folder are deleted before each run, so they disappear from your music app too.</span>
        </div>
        <button
          role="switch"
          aria-checked={cleanDownloads}
          onClick={handleCleanToggle}
          className={`relative inline-flex h-[22px] w-10 shrink-0 cursor-pointer rounded-full transition-colors duration-200 ${cleanDownloads ? 'bg-accent' : 'bg-[#383838]'}`}
        >
          <span className={`inline-block h-[18px] w-[18px] my-[2px] rounded-full bg-white shadow transition-transform duration-200 ${cleanDownloads ? 'translate-x-[20px]' : 'translate-x-[2px]'}`} />
        </button>
      </div>

      {/* Organize into folders toggle */}
      <div className="flex items-start justify-between mt-3 mb-1 gap-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-[13px] text-white">Organize into folders</span>
          <span className="text-[11px] text-muted">Sort downloads into subfolders by artist, album, etc.</span>
        </div>
        <button
          role="switch"
          aria-checked={templateEnabled}
          onClick={handleTemplateToggle}
          className={`relative inline-flex h-[22px] w-10 shrink-0 cursor-pointer rounded-full transition-colors duration-200 ${templateEnabled ? 'bg-accent' : 'bg-[#383838]'}`}
        >
          <span className={`inline-block h-[18px] w-[18px] my-[2px] rounded-full bg-white shadow transition-transform duration-200 ${templateEnabled ? 'translate-x-[20px]' : 'translate-x-[2px]'}`} />
        </button>
      </div>

      {/* Remove suffixes from album names for Apple Music*/}
      <div className="flex items-start justify-between mt-3 mb-1 gap-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-[13px] text-white">Remove suffixes from album names from Apple Music</span>
          <span className="text-[13px] text-muted">Removes the additional suffixes (e.g " - EP", " - Single") Apple Music generates when importing playlists.</span>
        </div>
        <button
          role="switch"
          aria-checked={suffixRemoval}
          onClick={handleSuffixToggle}
          className={`relative inline-flex h-[22px] w-10 shrink-0 cursor-pointer rounded-full transition-colors duration-200 ${suffixRemoval ? 'bg-accent' : 'bg-[#383838]'}`}
          >
            <span className={`inline-block h-[18px] w-[18px] my-[2px] rounded-full bg-white shadow transition-transform duration-200 ${suffixRemoval ? 'translate-x-[20px]' : 'translate-x-[2px]'}`} />
          </button>
      </div>

      {templateEnabled && (<>
      {/* Current / pending path readout */}
      <div className="flex items-baseline gap-2.5 overflow-x-auto py-1 mt-6">
        <span className={`text-[11px] shrink-0 transition-colors ${dirty ? 'text-accent' : 'text-muted'}`}>
          {dirty ? 'Preview:' : 'Active:'}
        </span>
        <div className="text-[13px] font-medium whitespace-nowrap">
          <PathLine template={previewTemplate} />
        </div>
      </div>

      
      

      {/* Profile card grid */}
      <div className="grid grid-cols-1 min-[520px]:grid-cols-2 min-[720px]:grid-cols-4 gap-3 mt-2">
        {profiles.map((profile, i) => {
          const isSelected = i === selectedIdx
          return (
            <div
              key={i}
              onClick={() => setSelectedIdx(i)}
              className={`group relative flex flex-col gap-2.5 p-4 bg-transparent rounded-[8px] cursor-pointer select-none transition-all duration-[120ms] border
                ${isSelected
                  ? 'border-accent'
                  : 'border-ui-border hover:border-[#404040] hover:-translate-y-px'}`}
              style={{ minHeight: 112, ...(isSelected ? { boxShadow: '0 0 0 3px rgba(30,215,96,0.14)' } : {}) }}
            >
              <div className="flex items-start justify-between gap-1.5">
                <span className="text-[14px] font-medium text-white leading-snug">{profile.name}</span>
                <div className="flex items-center gap-1.5 shrink-0">
                  {!profile.seed && (
                    <div className="relative" onClick={e => e.stopPropagation()}>
                      <button
                        onClick={e => { e.stopPropagation(); setOpenMenuIdx(openMenuIdx === i ? null : i) }}
                        className="opacity-0 group-hover:opacity-100 transition-opacity bg-transparent border-none text-muted hover:text-white text-[15px] leading-none cursor-pointer px-1 py-0"
                        title="Options"
                      >
                        ···
                      </button>
                      {openMenuIdx === i && (
                        <div className="absolute right-0 top-full mt-1 bg-well border border-ui-border rounded-[6px] z-10 py-1 min-w-[80px]"
                          style={{ boxShadow: '0 8px 24px #00000066' }}
                        >
                          <button
                            onClick={() => handleDeleteProfile(i)}
                            className="w-full text-left px-3 py-1.5 text-[12px] text-danger hover:bg-well transition-colors cursor-pointer bg-transparent border-none whitespace-nowrap"
                          >
                            Delete
                          </button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </div>
              <div className="mt-auto pt-2.5 border-t border-dashed border-ui-border">
                <div className="text-[11px] leading-relaxed">
                  <PathLine template={profile.template} />
                </div>
              </div>
            </div>
          )
        })}

        {/* New template card */}
        <div
          onClick={() => setShowModal(true)}
          className="flex flex-col items-center justify-center gap-2 p-4 bg-transparent rounded-[8px] border border-dashed border-ui-border cursor-pointer transition-colors text-muted hover:text-accent hover:border-accent"
          style={{ minHeight: 112 }}
        >
          <span className="text-[26px] leading-none">+</span>
          <span className="text-[13px]">New template</span>
        </div>
      </div>

      {/* Confirm bar */}
      <AnimatePresence>
        {dirty && (
          <motion.div
            key="confirmbar"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.2 }}
            className="flex items-center gap-4 mt-5"
          >
            <span className="mr-auto text-[12px] text-muted">
              {saveStatus || 'Preview only — not applied yet.'}
            </span>
            <button
              onClick={() => setSelectedIdx(appliedIdx)}
              className="bg-transparent border-none text-muted text-[13px] cursor-pointer p-0 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <Button onClick={handleSave}>
              Save folder structure
            </Button>
          </motion.div>
        )}
        {!dirty && saveStatus && (
          <motion.p
            key="savestatus"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="text-[12px] text-accent mt-4"
          >
            {saveStatus}
          </motion.p>
        )}
      </AnimatePresence>

      </>)}

      <AnimatePresence>
        {showModal && (
          <PathTemplateModal
            onClose={() => setShowModal(false)}
            onSave={handleSavePreset}
            enrichEnabled={enrichEnabled}
          />
        )}
      </AnimatePresence>
    </div>
  )
}

// ── Config Tab ────────────────────────────────────────────────────────────────
// Raw .env file viewer/editor, plus wizard re-run and full reset actions.
// Fetches its own raw config text from the API.

const MUSIC_SYSTEMS = [
  { value: 'subsonic', label: 'Navidrome / Subsonic', fields: ['SYSTEM_URL', 'SYSTEM_USERNAME', 'SYSTEM_PASSWORD'], admin: ['ADMIN_SYSTEM_USERNAME', 'ADMIN_SYSTEM_PASSWORD'], hint: 'Use the login of the Navidrome user that should get the playlists.' },
  { value: 'jellyfin', label: 'Jellyfin', fields: ['SYSTEM_URL', 'SYSTEM_USERNAME', 'API_KEY', 'LIBRARY_NAME'], admin: ['ADMIN_API_KEY'], hint: 'Create an API key in Jellyfin → Dashboard → API Keys.' },
  { value: 'emby', label: 'Emby', fields: ['SYSTEM_URL', 'SYSTEM_USERNAME', 'API_KEY', 'LIBRARY_NAME'], hint: 'Create an API key in Emby → Advanced → API Keys.' },
  { value: 'plex', label: 'Plex', fields: ['SYSTEM_URL', 'SYSTEM_USERNAME', 'SYSTEM_PASSWORD', 'API_KEY', 'LIBRARY_NAME'], admin: ['ADMIN_SYSTEM_USERNAME', 'ADMIN_SYSTEM_PASSWORD', 'ADMIN_API_KEY'], hint: 'Password or token (API key), plus the name of your music library.' },
  { value: 'mpd', label: 'MPD', fields: ['SYSTEM_URL'], hint: 'Address of your MPD server.' },
]

const FIELD_META = {
  ADMIN_SYSTEM_USERNAME: { label: 'Admin username' },
  ADMIN_SYSTEM_PASSWORD: { label: 'Admin password', secret: true },
  ADMIN_API_KEY: { label: 'Admin API key', secret: true },
  SYSTEM_URL: { label: 'Server address', placeholder: 'http://192.168.1.10:4533' },
  SYSTEM_USERNAME: { label: 'Username' },
  SYSTEM_PASSWORD: { label: 'Password', secret: true },
  API_KEY: { label: 'API key', secret: true },
  LIBRARY_NAME: { label: 'Music library name', placeholder: 'Music' },
  LISTENBRAINZ_USER: { label: 'ListenBrainz username' },
  LISTENBRAINZ_USER_TOKEN: { label: 'ListenBrainz user token', secret: true },
  YOUTUBE_API_KEY: { label: 'YouTube API key (optional)', secret: true },
  SLSKD_URL: { label: 'slskd address', placeholder: 'http://192.168.1.10:5030' },
  SLSKD_API_KEY: { label: 'slskd API key', secret: true },
  LIDARR_URL: { label: 'Lidarr address', placeholder: 'http://192.168.1.10:8686' },
  LIDARR_API_KEY: { label: 'Lidarr API key', secret: true },
}

const DOWNLOAD_SOURCES = [
  { value: 'youtube', label: 'YouTube', text: 'Works out of the box, no setup.', fields: ['YOUTUBE_API_KEY'] },
  { value: 'slskd', label: 'Soulseek (slskd)', text: 'Better quality, needs a running slskd.', fields: ['SLSKD_URL', 'SLSKD_API_KEY'] },
  { value: 'lidarr', label: 'Lidarr', text: 'Downloads whole albums through Lidarr.', fields: ['LIDARR_URL', 'LIDARR_API_KEY'] },
]

function YoutubeCookies() {
  const [present, setPresent] = useState(false)
  const [msg, setMsg] = useState('')
  const inputRef = useRef(null)
  useEffect(() => { cookiesPresent().then(setPresent).catch(() => {}) }, [])

  const onPick = async (e) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    try { await uploadCookies(file); setPresent(true); setMsg('Cookies saved ✓') } catch (err) { setMsg(err.message) }
  }
  const onRemove = async () => {
    try { await deleteCookies(); setPresent(false); setMsg('Cookies removed') } catch (err) { setMsg(err.message) }
  }

  return (
    <div className="mt-3 rounded-[8px] border border-ui-border p-3.5">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="text-[13px] text-white">YouTube cookies (optional) {present && <span className="text-accent text-[12px]">· active</span>}</div>
        <div className="flex items-center gap-2">
          <input ref={inputRef} type="file" accept=".txt,text/plain" className="hidden" onChange={onPick} />
          <Button onClick={() => inputRef.current?.click()}>{present ? 'Replace cookies.txt…' : 'Upload cookies.txt…'}</Button>
          {present && <button onClick={onRemove} className="bg-transparent border-none text-[12px] text-muted hover:text-white cursor-pointer">Remove</button>}
        </div>
      </div>
      <p className="text-[11px] text-muted mt-2">
        Helps when YouTube blocks downloads (“Sign in to confirm you’re not a bot”) and for age-restricted songs.
        With a YouTube Premium account it can also give you higher audio quality.
      </p>
      <p className="text-[11px] text-muted mt-1">
        How: log in to YouTube in your browser, then export the cookies in <b>Netscape format</b> (e.g. with the browser extension
        “Get cookies.txt LOCALLY”) and upload the file. Tip: use a separate Google account, the cookies give access to it.
      </p>
      {msg && <p className="text-[12px] text-muted mt-1.5">{msg}</p>}
    </div>
  )
}

// Spotify login cookie (sp_dc) of this profile: needed for personal playlists (Blend, Daily Mix, Discover Weekly of your account).
function SpotifyCookie() {
  const [present, setPresent] = useState(false)
  const [value, setValue] = useState('')
  const [msg, setMsg] = useState('')
  const inputRef = useRef(null)
  useEffect(() => { spotifyCookiePresent().then(setPresent).catch(() => {}) }, [])

  const save = async (args) => {
    try { await saveSpotifyCookie(args); setPresent(true); setValue(''); setMsg('Spotify cookie saved ✓') } catch (err) { setMsg(err.message) }
  }
  const onPick = (e) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (file) save({ file })
  }
  const onRemove = async () => {
    try { await deleteSpotifyCookie(); setPresent(false); setMsg('Spotify cookie removed') } catch (err) { setMsg(err.message) }
  }

  return (
    <div className="mt-6">
      <SectionLabel>Spotify login (optional)</SectionLabel>
      <div className="rounded-[8px] border border-ui-border p-3.5">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="text-[13px] text-white">Spotify cookie {present && <span className="text-accent text-[12px]">· active</span>}</div>
          <div className="flex items-center gap-2">
            <input ref={inputRef} type="file" accept=".txt,text/plain" className="hidden" onChange={onPick} />
            <Button onClick={() => inputRef.current?.click()}>{present ? 'Replace cookies.txt…' : 'Upload cookies.txt…'}</Button>
            {present && <button onClick={onRemove} className="bg-transparent border-none text-[12px] text-muted hover:text-white cursor-pointer">Remove</button>}
          </div>
        </div>
        <p className="text-[11px] text-muted mt-2">
          Without it, Explo only sees public playlists. With it, it can also read personal ones: <b>Blend</b>, your Daily Mixes, Discover Weekly
          and private playlists of this Spotify account. Each profile has its own cookie.
        </p>
        <p className="text-[11px] text-muted mt-1">
          How: log in at open.spotify.com in your browser, export the cookies in <b>Netscape format</b> (e.g. “Get cookies.txt LOCALLY”) and upload
          the file, or paste the value of the cookie <code>sp_dc</code> below. It stays valid for months; if Spotify stops accepting it, upload a new one.
          Tip: the cookie gives access to the account, only use it on your own server.
        </p>
        <div className="flex gap-2 mt-2.5 items-center">
          <input type="password" value={value} onChange={e => setValue(e.target.value)} placeholder="or paste the sp_dc value" autoComplete="off" spellCheck={false}
            className="bg-well border border-ui-border text-white rounded-[6px] px-2.5 py-1.5 text-[13px] outline-none focus:border-accent flex-1" />
          <Button onClick={() => value.trim() && save({ value: value.trim() })} disabled={!value.trim()}>Save</Button>
        </div>
        {msg && <p className="text-[12px] text-muted mt-1.5">{msg}</p>}
      </div>
    </div>
  )
}

function SettingsForm() {
  const [vals, setVals] = useState(null)
  const [status, setStatus] = useState('')
  const [dirty, setDirty] = useState(false)
  const [showSecret, setShowSecret] = useState({})
  const [isAdmin, setIsAdmin] = useState(null) // null = not loaded yet; true/false = answer to "is this user a server admin?"
  const [shared, setShared] = useState({})      // admin account stored once for all profiles
  const [useShared, setUseShared] = useState(false)
  const [shareNow, setShareNow] = useState(false) // save the admin typed below as the shared one

  useEffect(() => {
    Promise.all([fetchConfig(), fetchShared().catch(() => ({}))]).then(([{ values }, sh]) => {
      setVals(values)
      setShared(sh)
      const own = !!(values.ADMIN_SYSTEM_USERNAME || values.ADMIN_SYSTEM_PASSWORD || values.ADMIN_API_KEY)
      const hasShared = !!(sh.ADMIN_SYSTEM_USERNAME || sh.ADMIN_SYSTEM_PASSWORD || sh.ADMIN_API_KEY)
      setIsAdmin(own || hasShared ? false : true)
      setUseShared(!own && hasShared)
    }).catch(() => setVals({}))
  }, [])
  if (!vals) return null

  const set = (k, v) => { setVals(prev => ({ ...prev, [k]: v })); setDirty(true); setStatus('') }
  const system = MUSIC_SYSTEMS.find(m => m.value === vals.EXPLO_SYSTEM)
  const services = (vals.DOWNLOAD_SERVICES || 'youtube').split(',').map(x => x.trim()).filter(Boolean)
  const toggleService = (v) => {
    const next = services.includes(v) ? services.filter(x => x !== v) : [...services, v]
    set('DOWNLOAD_SERVICES', (next.length ? next : ['youtube']).join(','))
  }

  const hasShared = !!(shared.ADMIN_SYSTEM_USERNAME || shared.ADMIN_SYSTEM_PASSWORD || shared.ADMIN_API_KEY)
  const adminMissing = (() => {
    if (!system?.admin || isAdmin !== false || (useShared && hasShared)) return []
    // Plex accepts either user+password or an API key; all other systems need every admin field
    if (system.value === 'plex') {
      return (vals.ADMIN_API_KEY || (vals.ADMIN_SYSTEM_USERNAME && vals.ADMIN_SYSTEM_PASSWORD)) ? [] : ['ADMIN_SYSTEM_USERNAME', 'ADMIN_SYSTEM_PASSWORD']
    }
    return system.admin.filter(k => !vals[k])
  })()
  const canSave = dirty && adminMissing.length === 0

  const save = async () => {
    const keys = new Set(['EXPLO_SYSTEM', 'DOWNLOAD_SERVICES', 'TRACK_EXTENSION', 'EXTENSIONS', 'MIN_BITRATE', 'PUBLIC_PLAYLIST', 'LISTENBRAINZ_USER', 'LISTENBRAINZ_USER_TOKEN',
      ...MUSIC_SYSTEMS.flatMap(m => [...m.fields, ...(m.admin || [])]), ...DOWNLOAD_SOURCES.flatMap(d => d.fields)])
    const out = {}
    for (const k of keys) out[k] = vals[k] ?? ''
    const adminKeys = ['ADMIN_SYSTEM_USERNAME', 'ADMIN_SYSTEM_PASSWORD', 'ADMIN_API_KEY']
    const makeShared = system?.admin && isAdmin === false && !useShared && shareNow
    if (system?.admin && (isAdmin || (useShared && hasShared))) for (const k of system.admin) out[k] = '' // admin user or shared admin: no own admin account
    try {
      if (makeShared) {
        const sh = {}
        for (const k of adminKeys) sh[k] = vals[k] || ''
        await saveShared(sh)
        setShared(sh); setUseShared(true); setShareNow(false)
        for (const k of adminKeys) out[k] = '' // the profile uses the shared one from now on
      }
      await saveConfigKeys(out)
      setDirty(false); setStatus('Saved ✓')
      setTimeout(() => setStatus(''), 2500)
    } catch (e) { setStatus(e.message) }
  }

  const inputCls = 'bg-well border border-ui-border text-white rounded-[6px] px-2.5 py-1.5 text-[13px] outline-none focus:border-accent w-full'
  // plain render function (NOT a component): a component defined here would remount on every keystroke and steal focus
  const field = (k, required = false) => {
    const m = FIELD_META[k] || { label: k }
    const missing = required && adminMissing.includes(k)
    return (
      <label className="flex flex-col gap-1 min-w-[220px] flex-1">
        <span className="text-[12px] text-muted">{m.label}{required && <span className="text-danger"> *</span>}</span>
        <div className="flex gap-1.5">
          <input
            type={m.secret && !showSecret[k] ? 'password' : 'text'}
            value={vals[k] ?? ''}
            placeholder={m.placeholder}
            onChange={e => set(k, e.target.value)}
            autoComplete="off" spellCheck={false}
            className={inputCls + (missing ? ' !border-danger' : '')}
          />
          {m.secret && (
            <button type="button" onClick={() => setShowSecret(p => ({ ...p, [k]: !p[k] }))}
              className="bg-transparent border border-ui-border text-muted rounded-[6px] px-2 text-[11px] cursor-pointer hover:text-white">
              {showSecret[k] ? 'Hide' : 'Show'}
            </button>
          )}
        </div>
      </label>
    )
  }

  return (
    <div>
      <div className="mt-6">
        <SectionLabel>Music server</SectionLabel>
        <p className="text-[12px] text-muted mb-3">Where the generated playlists should appear.</p>
        <div className="flex gap-2 flex-wrap mb-3">
          {MUSIC_SYSTEMS.map(m => (
            <button key={m.value} type="button" onClick={() => set('EXPLO_SYSTEM', m.value)}
              className={`rounded-full px-3.5 py-1.5 text-[13px] border cursor-pointer bg-transparent transition-colors ${vals.EXPLO_SYSTEM === m.value ? 'border-accent text-white' : 'border-ui-border text-muted hover:text-white'}`}>
              {m.label}
            </button>
          ))}
        </div>
        {system ? (
          <>
            <div className="flex gap-3 flex-wrap">{system.fields.map(k => field(k))}</div>
            <p className="text-[11px] text-muted mt-2">{system.hint}</p>
            {system.admin && (
              <div className="mt-4 rounded-[8px] border border-ui-border p-3.5">
                <div className="text-[13px] text-white mb-1">Is this user an admin on the server?</div>
                <p className="text-[11px] text-muted mb-2.5">
                  Explo has to start a library scan after downloading, and only admins may do that.
                </p>
                <div className="flex gap-2">
                  {[[true, 'Yes'], [false, 'No']].map(([v, label]) => (
                    <button key={label} type="button"
                      onClick={() => { setIsAdmin(v); setDirty(true); setStatus('') }}
                      className={`rounded-full px-4 py-1 text-[13px] border cursor-pointer bg-transparent transition-colors ${isAdmin === v ? 'border-accent text-white' : 'border-ui-border text-muted hover:text-white'}`}>
                      {label}
                    </button>
                  ))}
                </div>
                {isAdmin === false && hasShared && useShared && (
                  <div className="mt-3 text-[12px] text-muted">
                    Using the shared admin account{shared.ADMIN_SYSTEM_USERNAME ? <> “<span className="text-white">{shared.ADMIN_SYSTEM_USERNAME}</span>”</> : ''} that is set up for all profiles.{' '}
                    <button type="button" onClick={() => { setUseShared(false); setDirty(true) }}
                      className="text-accent bg-transparent border-none cursor-pointer text-[12px] p-0">Use a different admin for this user</button>
                  </div>
                )}
                {isAdmin === false && !(hasShared && useShared) && (
                  <div className="mt-3">
                    <p className="text-[11px] text-muted mb-2">
                      Enter an admin account of your server. The playlists are still created for the user above; the admin is only used for the scan.
                    </p>
                    <div className="flex gap-3 flex-wrap">{system.admin.map(k => field(k, true))}</div>
                    {system.value === 'plex' && <p className="text-[11px] text-muted mt-1.5">Plex: admin username + password, or an admin API key.</p>}
                    <label className="flex items-center gap-2 mt-3 text-[12px] text-white cursor-pointer">
                      <input type="checkbox" checked={shareNow} onChange={e => { setShareNow(e.target.checked); setDirty(true) }} />
                      Use this admin account for all profiles (enter it only once)
                    </label>
                    {hasShared && (
                      <button type="button" onClick={() => { setUseShared(true); setDirty(true) }}
                        className="mt-2 text-accent bg-transparent border-none cursor-pointer text-[12px] p-0">Use the shared admin instead</button>
                    )}
                  </div>
                )}
              </div>
            )}
            {(system.value === 'subsonic' || system.value === 'jellyfin') && (
              <label className="flex items-center gap-2 mt-3 text-[13px] text-white cursor-pointer">
                <input type="checkbox" checked={vals.PUBLIC_PLAYLIST === 'true'} onChange={e => set('PUBLIC_PLAYLIST', e.target.checked ? 'true' : '')} />
                Make playlists public
              </label>
            )}
          </>
        ) : <p className="text-[12px] text-muted">Pick your music server above.</p>}
      </div>

      <div className="mt-6">
        <SectionLabel>ListenBrainz</SectionLabel>
        <p className="text-[12px] text-muted mb-3">Your listening history is where the recommendations come from.</p>
        <div className="flex gap-3 flex-wrap">{field("LISTENBRAINZ_USER")}{field("LISTENBRAINZ_USER_TOKEN")}</div>
        <p className="text-[11px] text-muted mt-2">The token is on your ListenBrainz settings page.</p>
      </div>

      <div className="mt-6">
        <SectionLabel>Where songs come from</SectionLabel>
        <p className="text-[12px] text-muted mb-3">Explo tries the checked sources in this order until it finds the song.</p>
        <div className="flex flex-col gap-3">
          {DOWNLOAD_SOURCES.map(d => (
            <div key={d.value}>
              <label className="flex items-start gap-2 cursor-pointer">
                <input type="checkbox" className="mt-1" checked={services.includes(d.value)} onChange={() => toggleService(d.value)} />
                <span>
                  <span className="text-[13px] text-white">{d.label}</span>
                  <span className="block text-[11px] text-muted">{d.text}</span>
                </span>
              </label>
              {services.includes(d.value) && d.fields.length > 0 && (
                <div className="flex gap-3 flex-wrap mt-2 ml-6">{d.fields.map(k => field(k))}</div>
              )}
              {services.includes(d.value) && d.value === 'youtube' && (
                <div className="ml-6">
                  <p className="text-[11px] text-muted mt-1.5">
                    The API key only changes how songs are <i>searched</i> (official YouTube search instead of the built-in YouTube Music search).
                    It does not change audio quality — you can leave it empty. Free key: Google Cloud Console → YouTube Data API v3.
                  </p>
                  <YoutubeCookies />
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="mt-6">
        <SectionLabel>Audio format</SectionLabel>
        <p className="text-[12px] text-muted mb-3">Which file type you want. The original quality of the source is the limit: converting never makes a song better.</p>

        {services.includes('youtube') && (
          <div className="mb-4">
            <div className="text-[13px] text-white mb-1.5">YouTube downloads are saved as</div>
            <div className="flex gap-2 flex-wrap">
              {[['mp3', 'MP3', 'Works on every device (recommended)'], ['m4a', 'M4A (AAC)', 'Slightly better quality than MP3 at the same size'], ['opus', 'Opus', 'Best quality for YouTube, a few old players cannot play it']].map(([v, label, text]) => (
                <button key={v} type="button" onClick={() => set('TRACK_EXTENSION', v)}
                  className={`text-left rounded-[8px] border px-3.5 py-2.5 cursor-pointer bg-transparent transition-colors min-w-[170px] ${(vals.TRACK_EXTENSION || 'mp3') === v ? 'border-accent' : 'border-ui-border hover:border-[#444]'}`}>
                  <div className="text-[13px] text-white">{label}</div>
                  <div className="text-[11px] text-muted mt-0.5">{text}</div>
                </button>
              ))}
            </div>
            <p className="text-[11px] text-muted mt-2">YouTube only delivers lossy audio (about 128–160 kbps, more with Premium cookies), so there is no FLAC option here.</p>
          </div>
        )}

        {services.includes('slskd') && (
          <div>
            <div className="text-[13px] text-white mb-1.5">Soulseek: accepted file types</div>
            <div className="flex gap-4 flex-wrap mb-3">
              {[['flac', 'FLAC (lossless)'], ['mp3', 'MP3'], ['m4a', 'M4A'], ['ogg', 'OGG']].map(([v, label]) => {
                const exts = (vals.EXTENSIONS || 'flac,mp3').split(',').map(x => x.trim()).filter(Boolean)
                return (
                  <label key={v} className="flex items-center gap-2 text-[13px] text-white cursor-pointer">
                    <input type="checkbox" checked={exts.includes(v)} onChange={() => {
                      const next = exts.includes(v) ? exts.filter(x => x !== v) : [...exts, v]
                      set('EXTENSIONS', (next.length ? next : ['mp3']).join(','))
                    }} />
                    {label}
                  </label>
                )
              })}
            </div>
            <label className="flex flex-col gap-1 w-[260px]">
              <span className="text-[12px] text-muted">Minimum quality for lossy files</span>
              <select value={vals.MIN_BITRATE || '256'} onChange={e => set('MIN_BITRATE', e.target.value)} className={inputCls}>
                <option value="128">128 kbps (small, lower quality)</option>
                <option value="192">192 kbps</option>
                <option value="256">256 kbps (default)</option>
                <option value="320">320 kbps (best MP3)</option>
              </select>
            </label>
            <p className="text-[11px] text-muted mt-2">Songs below this quality are skipped. FLAC is not affected.</p>
          </div>
        )}

        {!services.includes('youtube') && !services.includes('slskd') && (
          <p className="text-[12px] text-muted">The format is decided by Lidarr for your selected source.</p>
        )}
      </div>

      <div className="sticky bottom-0 z-20 flex items-center gap-3 mt-5 -mx-6 px-6 py-3 border-t border-ui-border bg-[#121212]/95 backdrop-blur">
        <Button onClick={save} disabled={!canSave}>Save settings</Button>
        <span className="text-[12px] text-muted">{status || (adminMissing.length ? 'Please fill in the admin account above.' : dirty ? 'Unsaved changes. Saved settings apply from the next run on.' : '')}</span>
      </div>
    </div>
  )
}

function ConfigSection({ onWizard }) {
  const [rawConfig, setRawConfig] = useState('')
  const [editing, setEditing] = useState(false)
  const [saveStatus, setSaveStatus] = useState('')

  useEffect(() => {
    fetchConfigRaw().then(text => setRawConfig(text))
  }, [])

  const handleSave = async () => {
    try {
      await saveConfig(rawConfig)
      setEditing(false)
      setSaveStatus('Saved.')
      setTimeout(() => setSaveStatus(''), 2500)
    } catch (e) {
      setSaveStatus(e.message && e.message.length < 120 ? e.message : 'Error saving.')
    }
  }

  const handleReset = async () => {
    if (!confirm('Reset all settings? This will restart the container and take you back to setup.')) return
    try {
      await resetConfig()
      const poll = async () => {
        try { await fetch('/api/config'); location.reload() }
        catch { setTimeout(poll, 1500) }
      }
      setTimeout(poll, 3000)
    } catch (e) {
      alert('Reset failed: ' + e.message)
    }
  }

  return (
    <div>
      <SettingsForm />

      <SpotifyCookie />

      <PlaylistHandling />

      <DownloadPathSection />

      <details className="mt-6 group">
        <summary className="cursor-pointer text-[13px] text-muted hover:text-white select-none">Advanced: edit the config file directly</summary>
      <div className="mt-6">
        <div className="flex items-center justify-between mb-3.5">
          <SectionLabel className="">Config file (.env)</SectionLabel>
          {!editing ? (
            <Button onClick={() => setEditing(true)}>Edit</Button>
          ) : (
            <div className="flex items-center gap-2.5">
              <span className="text-[12px] text-muted">{saveStatus}</span>
              <Button onClick={handleSave}>Save</Button>
              <Button onClick={() => { fetchConfigRaw().then(setRawConfig); setEditing(false) }}>Cancel</Button>
            </div>
          )}
        </div>

        {!editing ? (
          <pre
            className="bg-well border border-ui-border rounded-[6px] w-full h-[420px] overflow-y-auto p-3.5 font-mono text-[12px] leading-relaxed whitespace-pre break-normal"
            dangerouslySetInnerHTML={{ __html: highlightEnv(rawConfig, true) }}
          />
        ) : (
          <textarea
            className="bg-well border border-ui-border text-white rounded-[6px] w-full h-[420px] p-3.5 font-mono text-[12px] leading-relaxed resize-y outline-none focus:border-accent"
            value={rawConfig}
            onChange={e => setRawConfig(e.target.value)}
            spellCheck={false}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
          />
        )}
      </div>

      </details>

      <AccountSection />

      <div className="mt-6">
        <SectionLabel>Setup</SectionLabel>
        <div className="flex flex-col items-start gap-2.5">
          <button
            onClick={onWizard}
            className="bg-transparent border-none text-muted text-[13px] cursor-pointer p-0 hover:text-white transition-colors"
          >
            Re-run setup wizard →
          </button>
          <button
            onClick={handleReset}
            className="bg-transparent border-none text-[#c0392b] text-[13px] cursor-pointer p-0 hover:text-[#d65546] transition-colors"
          >
            Reset all settings
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Logs Tab ──────────────────────────────────────────────────────────────────
// Displays the full server log file. Fetches its own log data from the API.

function LogsSection() {
  const [logFileEntries, setLogFileEntries] = useState([])
  const panelRef = useRef(null)

  const loadLog = () => {
    fetchLogs().then(text => {
      setLogFileEntries(text.split('\n').filter(l => l.trim()).map(l => ({ raw: l, ...parseSlogLine(l) })))
    })
  }

  useEffect(() => { loadLog() }, [])

  useEffect(() => {
    if (panelRef.current) panelRef.current.scrollTop = panelRef.current.scrollHeight
  }, [logFileEntries])

  return (
    <div className="mt-6">
      <div className="flex items-center justify-between mb-3.5">
        <SectionLabel className="">Log</SectionLabel>
        <button
          onClick={loadLog}
          className="bg-transparent border-none text-muted text-[11px] cursor-pointer p-0 hover:text-white transition-colors"
        >
          Refresh
        </button>
      </div>

      {logFileEntries.length === 0 ? (
        <p className="text-[12px] text-muted py-1">No log output yet.</p>
      ) : (
        <Panel ref={panelRef} className="h-[400px]">
          {logFileEntries.map((e, i) => <LogRow key={i} entry={e} />)}
        </Panel>
      )}
    </div>
  )
}

// ── Settings ──────────────────────────────────────────────────────────────────
// Tab shell. Routes between Home, Settings, and Logs sections.

// Module-level cache so the picked cover survives component remounts.
let _bgCoverCache = null

// Login of the Explo web interface itself (not the music server): change name and password any time.
function AccountSection() {
  const [acc, setAcc] = useState(null)
  const [username, setUsername] = useState('')
  const [current, setCurrent] = useState('')
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [msg, setMsg] = useState({ text: '', ok: false })

  useEffect(() => {
    fetchAccount().then(a => { setAcc(a); setUsername(a.username || '') }).catch(() => {})
  }, [])
  if (!acc) return null

  const off = acc.auth_disabled
  const inputCls = 'bg-well border border-ui-border text-white rounded-[6px] px-2.5 py-1.5 text-[13px] outline-none focus:border-accent w-full'
  const submit = async () => {
    setMsg({ text: '', ok: false })
    if (pw && pw !== pw2) { setMsg({ text: 'The two new passwords are different.', ok: false }); return }
    if (off && !pw) { setMsg({ text: 'Choose a password (at least 8 characters) to turn the login on.', ok: false }); return }
    try {
      await changeAccount({ current, username: username.trim(), password: pw })
      setCurrent(''); setPw(''); setPw2('')
      setAcc(a => ({ ...a, username: username.trim() || a.username, auth_disabled: false }))
      setMsg({ text: off ? 'Login turned on ✓ — you will be asked for it from the next page load.' : 'Saved ✓', ok: true })
    } catch (e) { setMsg({ text: e.message, ok: false }) }
  }

  return (
    <div className="mt-6">
      <SectionLabel>Explo login</SectionLabel>
      <p className="text-[12px] text-muted mb-3">
        {off
          ? 'The web interface is open to everyone who can reach it. Set a name and password to protect it.'
          : 'The name and password you use to sign in to this web interface. You can change them at any time. A login set here replaces UI_USERNAME / UI_PASSWORD from the compose file.'}
      </p>
      <div className="flex gap-3 flex-wrap">
        <label className="flex flex-col gap-1 min-w-[200px] flex-1">
          <span className="text-[12px] text-muted">Username</span>
          <input value={username} onChange={e => setUsername(e.target.value)} autoComplete="off" spellCheck={false} className={inputCls} />
        </label>
        {!off && (
          <label className="flex flex-col gap-1 min-w-[200px] flex-1">
            <span className="text-[12px] text-muted">Current password</span>
            <input type="password" value={current} onChange={e => setCurrent(e.target.value)} autoComplete="current-password" className={inputCls} />
          </label>
        )}
        <label className="flex flex-col gap-1 min-w-[200px] flex-1">
          <span className="text-[12px] text-muted">New password{off ? '' : ' (leave empty to keep it)'}</span>
          <input type="password" value={pw} onChange={e => setPw(e.target.value)} autoComplete="new-password" className={inputCls} />
        </label>
        <label className="flex flex-col gap-1 min-w-[200px] flex-1">
          <span className="text-[12px] text-muted">Repeat new password</span>
          <input type="password" value={pw2} onChange={e => setPw2(e.target.value)} autoComplete="new-password" className={inputCls} />
        </label>
      </div>
      <div className="flex items-center gap-3 mt-3">
        <button type="button" onClick={submit}
          className="bg-transparent border border-accent text-white rounded-full px-4 py-1 text-[13px] cursor-pointer hover:bg-accent/10">
          {off ? 'Turn login on' : 'Change login'}
        </button>
        {msg.text && <span className={`text-[12px] ${msg.ok ? 'text-muted' : 'text-danger'}`}>{msg.text}</span>}
      </div>
      <p className="text-[11px] text-muted mt-2">Password: 8–72 characters. If you forget it, delete the file ui-auth.json in the config folder and restart.</p>
    </div>
  )
}

function ProfileSwitcher() {
  const [profiles, setProfiles] = useState([getProfile()])
  const [adding, setAdding] = useState(false)
  const [newName, setNewName] = useState('')
  const [err, setErr] = useState('')
  const [mode, setMode] = useState(null) // null | 'rename' | 'delete'
  const [labels, setLabels] = useState({})
  const current = getProfile()
  const labelOf = (n) => labels[n] || n

  useEffect(() => {
    fetchProfileLabels().then(setLabels)
    fetchProfiles().then(list => {
      setProfiles(list)
      // the selected profile was deleted/renamed elsewhere: fall back to the default one
      if (!list.includes(getProfile())) { setProfile('default'); window.location.reload() }
    }).catch(() => {})
  }, [])

  const switchTo = (name) => {
    setProfile(name)
    window.location.reload()
  }

  const addProfile = async () => {
    const name = newName.trim().toLowerCase()
    if (!name) return
    try {
      await createProfile(name)
      switchTo(name)
    } catch (e) {
      setErr(e.message)
    }
  }

  const doRename = async () => {
    if (current === 'default') {
      // the default profile keeps its folder, only the display name changes
      try { await setProfileLabel('default', newName.trim()); window.location.reload() } catch (e) { setErr(e.message) }
      return
    }
    const name = newName.trim().toLowerCase()
    if (!name || name === current) { setMode(null); return }
    try { await renameProfile(current, name); switchTo(name) } catch (e) { setErr(e.message) }
  }
  const doDelete = async () => {
    try { await deleteProfile(current); switchTo('default') } catch (e) { setErr(e.message) }
  }

  if (mode === 'rename') {
    return (
      <div className="pb-2 flex items-center gap-1.5">
        <input
          autoFocus
          value={newName}
          onChange={e => { setNewName(e.target.value); setErr('') }}
          onKeyDown={e => { if (e.key === 'Enter') doRename(); if (e.key === 'Escape') setMode(null) }}
          className="text-[12px] text-white bg-transparent border border-ui-border rounded px-1.5 py-0.5 w-[120px] outline-none focus:border-accent"
        />
        <button onClick={doRename} className="text-[12px] text-accent bg-transparent border-none cursor-pointer">Rename</button>
        <button onClick={() => { setMode(null); setErr('') }} className="text-[12px] text-muted bg-transparent border-none cursor-pointer">✕</button>
        {err && <span className="text-[11px] text-danger">{err}</span>}
      </div>
    )
  }

  if (mode === 'delete') {
    return (
      <div className="pb-2 flex items-center gap-2 flex-wrap max-w-[420px] justify-end">
        <span className="text-[12px] text-muted">Delete profile “{labelOf(current)}”? Its folder is moved to profiles/.trash, nothing is erased.</span>
        <button onClick={doDelete} className="text-[12px] text-danger bg-transparent border-none cursor-pointer">Delete</button>
        <button onClick={() => { setMode(null); setErr('') }} className="text-[12px] text-muted bg-transparent border-none cursor-pointer">Cancel</button>
        {err && <span className="text-[11px] text-danger">{err}</span>}
      </div>
    )
  }

  if (adding) {
    return (
      <div className="pb-2 flex items-center gap-1.5">
        <input
          autoFocus
          value={newName}
          onChange={e => { setNewName(e.target.value); setErr('') }}
          onKeyDown={e => { if (e.key === 'Enter') addProfile(); if (e.key === 'Escape') setAdding(false) }}
          placeholder="name, e.g. anna"
          className="text-[12px] text-white bg-transparent border border-ui-border rounded px-1.5 py-0.5 w-[120px] outline-none focus:border-accent"
        />
        <button onClick={addProfile} className="text-[12px] text-accent bg-transparent border-none cursor-pointer">Add</button>
        <button onClick={() => { setAdding(false); setErr('') }} className="text-[12px] text-muted bg-transparent border-none cursor-pointer">✕</button>
        {err && <span className="text-[11px] text-danger">{err}</span>}
      </div>
    )
  }

  return (
    <div className="pb-2 flex items-center gap-1">
      <select
        value={current}
        onChange={(e) => (e.target.value === '__add__' ? setAdding(true) : switchTo(e.target.value))}
        title="User profile"
        className="text-[12px] text-muted bg-transparent border border-ui-border rounded px-1 py-0.5 cursor-pointer"
      >
        {profiles.map((n) => (
          <option key={n} value={n} className="bg-black">{labelOf(n)}</option>
        ))}
        <option value="__add__" className="bg-black">+ add profile…</option>
      </select>
      <button onClick={() => { setNewName(current === 'default' ? (labels.default || '') : current); setErr(''); setMode('rename') }} title="Rename this profile"
        className="text-[12px] text-muted hover:text-white bg-transparent border-none cursor-pointer">Rename</button>
      {current !== 'default' && (
        <>
          <button onClick={() => { setErr(''); setMode('delete') }} title="Delete this profile"
            className="text-[12px] text-muted hover:text-danger bg-transparent border-none cursor-pointer">Delete</button>
        </>
      )}
    </div>
  )
}

function RestartBanner() {
  const pollTimer = useRef(null)
  useEffect(() => () => clearTimeout(pollTimer.current), [])
  const [needed, setNeeded] = useState(false)
  const [phase, setPhase] = useState('idle') // idle | confirm | restarting
  const [err, setErr] = useState('')

  useEffect(() => {
    let alive = true
    const check = () => restartNeeded().then(v => { if (alive) setNeeded(v) })
    check()
    window.addEventListener('explo-changed', check)
    const t = setInterval(check, 10000)
    return () => { alive = false; window.removeEventListener('explo-changed', check); clearInterval(t) }
  }, [])

  const go = async () => {
    setErr('')
    try {
      await restartNow()
      setPhase('restarting')
      // wait for the container to go down and come back, then reload. Any HTTP answer counts as
      // "back" (a 401 is fine: sessions do not survive a restart), a connection error means "down".
      let wentDown = false
      const started = Date.now()
      const poll = async () => {
        if (Date.now() - started > 180000) {
          setPhase('confirm')
          setErr('The restart takes longer than expected. Check the container (docker ps / docker logs).')
          return
        }
        try {
          await fetch('/api/ui/auth/status', { cache: 'no-store' })
          if (wentDown || Date.now() - started > 15000) { window.location.reload(); return }
        } catch { wentDown = true }
        pollTimer.current = setTimeout(poll, 1500)
      }
      pollTimer.current = setTimeout(poll, 2000)
    } catch (e) { setErr(e.message); setPhase('confirm') }
  }

  if (!needed && phase !== 'restarting') return null
  return (
    <div className="mb-4 rounded-[8px] border border-accent/60 bg-[#16221a] px-4 py-3 flex items-center gap-3 flex-wrap">
      {phase === 'restarting' ? (
        <span className="text-[13px] text-white">Restarting Explo… this page reloads automatically.</span>
      ) : (
        <>
          <div className="flex-1 min-w-[220px]">
            <div className="text-[13px] text-white">Restart needed</div>
            <div className="text-[11px] text-muted">Changed schedules and profiles only take effect after Explo restarts. The container must have a restart policy (restart: unless-stopped).</div>
            {err && <div className="text-[11px] text-danger mt-1">{err}</div>}
          </div>
          {phase === 'idle' ? (
            <Button onClick={() => setPhase('confirm')}>Restart now</Button>
          ) : (
            <div className="flex items-center gap-2">
              <span className="text-[12px] text-muted">A running download would be interrupted.</span>
              <Button onClick={go}>Yes, restart</Button>
              <button onClick={() => { setPhase('idle'); setErr('') }} className="bg-transparent border-none text-[12px] text-muted hover:text-white cursor-pointer">Cancel</button>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default function Settings({ authDisabled, onWizard, onLogout }) {
  const [activeTab, setActiveTab] = useState('run')
  const [bgCover, setBgCover] = useState(_bgCoverCache)

  useEffect(() => {
    if (_bgCoverCache) return
    Promise.all(['weekly-exploration', 'weekly-jams', 'daily-jams', 'on-repeat', 'fresh-releases'].map(
      t => fetchPlaylistTracks(t).catch(() => ({ tracks: [] }))
    )).then(results => {
      const covers = results.flatMap(r => (r.tracks ?? []).map(t => t.coverUrl).filter(Boolean))
      if (covers.length) {
        _bgCoverCache = covers[Math.floor(Math.random() * covers.length)]
        setBgCover(_bgCoverCache)
      }
    })
  }, [])

  return (
    <div style={{ position: 'relative', minHeight: '100vh' }}>
      <UpdateNotification />
      {/* Page background art */}
      <div style={{ position: 'fixed', inset: 0, zIndex: 0, background: '#121212', overflow: 'hidden', willChange: 'transform' }}>
        <AnimatePresence>
          {bgCover && (
            <motion.img
              key={bgCover}
              src={bgCover}
              alt=""
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 2, ease: 'easeInOut' }}
              style={{
                position: 'absolute', inset: 0,
                width: '100%', height: '100%',
                objectFit: 'cover',
                filter: 'blur(90px) saturate(1.8) brightness(0.14)',
                transform: 'scale(1.15) translateZ(0)',
                willChange: 'opacity',
              }}
            />
          )}
        </AnimatePresence>
      </div>

      {/* Content */}
      <div style={{ position: 'relative', zIndex: 1 }} className="min-h-screen">
        <div className="max-w-[980px] mx-auto px-6 pb-12">
          <header className="flex items-baseline gap-4 pt-5 pb-0 border-b border-ui-border mb-6">
            <span className="text-[16px] leading-none font-bold tracking-tight text-accent">Explo</span>
            <nav className="flex gap-0.5 items-baseline flex-1">
              <button className={tabBtnCls(activeTab === 'run')} onClick={() => setActiveTab('run')}>Home</button>
              <button className={tabBtnCls(activeTab === 'config')} onClick={() => setActiveTab('config')}>Settings</button>
              <button className={tabBtnCls(activeTab === 'logs')} onClick={() => setActiveTab('logs')}>Logs</button>
            </nav>
            <ProfileSwitcher />
            {authDisabled ? (
              <span
                title="UI_USERNAME and UI_PASSWORD are not set, so the web UI is open to anyone who can reach it."
                className="pb-2 text-[12px] text-muted/70 cursor-default select-none"
              >
                Auth disabled
              </span>
            ) : (
              <button
                onClick={onLogout}
                className="pb-2 text-[12px] text-muted hover:text-white transition-colors cursor-pointer bg-transparent border-none"
              >
                Sign out
              </button>
            )}
          </header>

          <RestartBanner />
          {activeTab === 'run' && <HomeSection />}
          {activeTab === 'config' && <ConfigSection onWizard={onWizard} />}
          {activeTab === 'logs' && <LogsSection />}
        </div>
      </div>
    </div>
  )
}
