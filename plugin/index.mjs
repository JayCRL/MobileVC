/**
 * mobilevc — MobileVC UI served by the DSH host.
 *
 * The page is static, but every read and write goes through the live
 * `ctx.sessionController` service in this process: no second Agent, no second
 * session-log writer, no copy of the state machine.
 */
import { randomUUID } from 'node:crypto'
import { chmod, mkdir, readFile, readdir, realpath, stat, writeFile } from 'node:fs/promises'
import { dirname, extname, join, resolve, sep } from 'node:path'
import { homedir } from 'node:os'
import { fileURLToPath } from 'node:url'
import webpush from 'web-push'

const BASE = '/mobilevc'
const DSH_STATE = join(homedir(), '.dsh')
const VAPID_FILE = join(DSH_STATE, 'mobilevc-vapid.json')
const PUSH_FILE = join(DSH_STATE, 'mobilevc-push.json')
const PREVIEW_ROOT = fileURLToPath(new URL('../preview', import.meta.url))
const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.webmanifest': 'application/manifest+json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
}

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c])
}

function textOfContent(content) {
  if (!Array.isArray(content)) return ''
  const parts = []
  for (const part of content) {
    if (part && part.type === 'text' && typeof part.text === 'string' && part.text) parts.push(part.text)
  }
  return parts.join('\n\n').trim()
}

function messageText(message) {
  return message && typeof message === 'object' ? textOfContent(message.content) : ''
}

function mdToHtml(input, limit = 14000) {
  let text = String(input ?? '').trim()
  if (text.length > limit) text = `${text.slice(0, limit)}\n\n…（已截断）`
  let out = esc(text)
  out = out.replace(/```[a-zA-Z0-9_+-]*\n([\s\S]*?)```/g, (_m, code) => `<pre><code>${code.replace(/\s+$/, '')}</code></pre>`)
  out = out.replace(/`([^`\n]+)`/g, '<code>$1</code>')
  out = out.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
  out = out.replace(/^#{1,6}\s+(.+)$/gm, '<div class="md-h">$1</div>')
  out = out.replace(/^(\s*)[-*]\s+/gm, '$1• ')
  out = out.replace(/```/g, '')
  return out
    .split(/\n{2,}/)
    .map((block) => {
      const b = block.trim()
      if (!b) return ''
      if (b.startsWith('<pre>') || b.startsWith('<div class="md-h">')) return b
      return `<p>${b.replace(/\n/g, '<br>')}</p>`
    })
    .join('') || '<p></p>'
}

function prettyArgs(raw) {
  let value = raw
  if (typeof raw === 'string') {
    try {
      value = JSON.parse(raw)
    } catch {
      return raw.slice(0, 4000)
    }
  }
  if (value && typeof value === 'object') return JSON.stringify(value, null, 2).slice(0, 4000)
  return String(value ?? '').slice(0, 4000)
}

function argSummary(name, raw) {
  let value = raw
  if (typeof raw === 'string') {
    try {
      value = JSON.parse(raw)
    } catch {
      return raw.trim().split('\n')[0].slice(0, 160)
    }
  }
  if (!value || typeof value !== 'object') return String(value ?? '').slice(0, 160)
  for (const key of ['command', 'file_path', 'path', 'pattern', 'query', 'url', 'description']) {
    if (value[key]) return String(value[key]).trim().split('\n')[0].slice(0, 160)
  }
  const key = Object.keys(value)[0]
  return key ? `${key}=${String(value[key]).slice(0, 120)}` : ''
}

function relativeTime(ts) {
  if (!ts) return ''
  const diff = Math.max(0, Date.now() - Number(ts))
  if (diff < 90_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)} 天前`
  return new Date(Number(ts)).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
}

/** Convert one durable event log into the JSON the MobileVC page renders. */
const USER_SOURCE_LABELS = {
  'steering': '插话',
  'runtime-context': '运行时上下文',
  'tool-jobs': '后台任务通知',
  'agent-instructions': 'Agent 指令',
  'compact-checkpoint': '压缩检查点',
  'user-approval': '审批结果',
  'model-selection': '模型切换',
  'subagent-settled': '子任务完成',
  'goal': '目标提醒',
  'schedule': '定时任务',
}

function sourceLabel(source) {
  return USER_SOURCE_LABELS[String(source || '')] || '系统消息'
}

function isHumanSource(source) {
  const kind = String(source || 'user')
  return kind === 'user' || kind === 'steering'
}

function inspectToBlocks(inspection) {
  const events = Array.isArray(inspection?.events) ? inspection.events : []
  const meta = inspection?.meta ?? {}
  const blocks = []
  const calls = new Map()
  const pending = new Map()
  let title = ''
  let engine = ''
  let model = ''
  let running = false
  let lastTurnError = false
  let userCount = 0
  let assistantCount = 0
  let lastText = ''
  let lastTime = 0

  for (const event of events) {
    const type = event?.type
    const data = event?.data ?? {}
    lastTime = Number(event?.time ?? lastTime)
    if (type === 'session/title') {
      title = String(data.title ?? '').trim()
      continue
    }
    if (type === 'request/header') {
      const cfg = data.header?.config ?? {}
      engine = String(cfg.provider ?? engine)
      model = String(cfg.model ?? model)
      continue
    }
    if (type === 'user/message') {
      const content = Array.isArray(data.content) ? data.content : []
      const text = textOfContent(content)
      const images = contentImages(content, data.meta)
      const files = contentFiles(content)
      const source = String(data.source?.kind || 'user')
      if (isHumanSource(source)) {
        if (text || images.length || files.length) {
          userCount += 1
          if (text) lastText = text
          blocks.push({ type: 'user', text: text.slice(0, 12000), images, files, source })
        }
      } else if (text) {
        blocks.push({
          type: 'system',
          title: sourceLabel(source),
          body: text.slice(0, 1600),
          icon: 'info',
          tone: 'blue',
          source,
        })
      }
      continue
    }
    if (type === 'assistant/message') {
      const content = Array.isArray(data.message?.content) ? data.message.content : []
      const reasoning = content
        .filter((part) => part && part.type === 'reasoning' && typeof part.text === 'string' && part.text)
        .map((part) => part.text)
        .join('\n\n')
        .trim()
      const text = content
        .filter((part) => part && part.type === 'text' && typeof part.text === 'string' && part.text)
        .map((part) => part.text)
        .join('\n\n')
        .trim()
      if (reasoning) {
        blocks.push({ type: 'thinking', html: mdToHtml(reasoning, 20000), chars: reasoning.length })
      }
      if (text) {
        assistantCount += 1
        lastText = text
        blocks.push({ type: 'assistant', html: mdToHtml(text) })
      }
      continue
    }
    if (type === 'tool/call') {
      const callId = String(data.callId ?? '')
      const name = String(data.name ?? 'tool')
      const pretty = prettyArgs(data.arguments)
      const block = {
        type: 'tool',
        name,
        arg: argSummary(name, data.arguments),
        status: '运行中',
        body: pretty,
        images: [],
        _callId: callId,
        startedAt: Number(event?.time ?? 0),
      }
      blocks.push(block)
      calls.set(callId, block)
      continue
    }
    if (type === 'tool/result') {
      const callId = String(data.toolCallId ?? data.message?.toolCallId ?? '')
      const body = messageText(data.message)
      const images = toolImages(data.message, data.meta)
      const failed = Boolean(data.message?.isError)
      const status = failed ? '失败' : '完成'
      const block = calls.get(callId)
      if (block) {
        block.status = status
        block.body = (body || block.body || '').slice(0, 6000)
        block.images = images
        block.finishedAt = Number(event?.time ?? 0)
        if (block.startedAt && block.finishedAt) block.durationMs = Math.max(0, block.finishedAt - block.startedAt)
      } else {
        blocks.push({
          type: 'tool',
          name: 'result',
          arg: callId.slice(0, 40),
          status,
          body: body.slice(0, 6000),
          images,
        })
      }
      continue
    }
    if (type === 'approval/asked') {
      const id = String(data.id ?? '')
      pending.set(id, {
        id,
        tool: String(data.toolName ?? ''),
        reason: String(data.reason ?? '').slice(0, 600),
        callId: String(data.callId ?? ''),
      })
      continue
    }
    if (type === 'approval/decided') {
      pending.delete(String(data.id ?? ''))
      continue
    }
    if (type === 'compaction/summary') {
      const summary = textOfContent(data.summary)
      blocks.push({
        type: 'compaction',
        text: `上下文已压缩${summary ? ` · ${summary.slice(0, 120)}` : ''}`,
      })
      continue
    }
    if (type === 'turn/start') {
      running = true
      lastTurnError = false
      continue
    }
    if (type === 'turn/end') {
      running = false
      const reason = data.reason ?? {}
      if (reason.kind === 'error' || reason.kind === 'max-tokens' || reason.kind === 'blocked') {
        lastTurnError = true
        blocks.push(failureBlock(reason))
      } else {
        lastTurnError = false
      }
      continue
    }
    if (type === 'session/end-seed') {
      running = false
    }
  }

  const publicBlocks = blocks.slice(-120)
  if (!title) {
    const first = blocks.find((b) => b.type === 'user' && b.text)
    title = first ? first.text.trim().split('\n')[0].slice(0, 40) : String(meta.id ?? 'DSH')
  }
  const cwd = String(meta.cwd ?? '')
  const subagent = Boolean(meta.parentSession) || Number(meta.delegationDepth ?? 0) > 0
  const pendingList = [...pending.values()]
  const state = pendingList.length ? 'waiting' : running ? 'running' : lastTurnError ? 'error' : 'idle'
  return {
    id: String(meta.id ?? ''),
    title,
    cwd,
    engine: engine || 'DSH',
    model: model || String(meta.agentPreset ?? 'default'),
    state,
    stateLabel: state === 'waiting' ? '待授权' : state === 'running' ? '运行中' : state === 'error' ? '失败' : '空闲',
    last: lastText.replace(/\s+/g, ' ').slice(0, 120),
    time: relativeTime(meta.updatedAt ?? lastTime ?? meta.createdAt),
    updatedAt: Number(meta.updatedAt ?? lastTime ?? meta.createdAt ?? 0),
    pending: pendingList.length,
    blockCount: publicBlocks.length,
    userCount,
    assistantCount,
    subagent,
    blocks: publicBlocks,
    approvals: pendingList,
  }
}

function summaryFromInspection(summary, inspection) {
  const detail = inspectToBlocks(inspection)
  return {
    id: String(summary.sessionId ?? detail.id),
    title: detail.title,
    cwd: detail.cwd || String(summary.cwd ?? ''),
    engine: detail.engine,
    model: detail.model,
    state: summary.running ? 'running' : detail.pending ? 'waiting' : detail.state === 'error' ? 'error' : 'idle',
    stateLabel: summary.running ? '运行中' : detail.pending ? '待授权' : detail.state === 'error' ? '失败' : '空闲',
    last: detail.last,
    time: relativeTime(summary.updatedAt ?? detail.updatedAt),
    updatedAt: Number(summary.updatedAt ?? detail.updatedAt ?? 0),
    pending: detail.pending,
    blockCount: detail.blockCount,
    userCount: detail.userCount,
    assistantCount: detail.assistantCount,
    subagent: Boolean(summary.origin === 'subagent' || detail.subagent),
  }
}


const HOME = homedir()
const LOG_DIR = join(HOME, '.dsh', 'logs')
const ATTACHMENTS = join(HOME, '.dsh', 'attachments', 'v1', 'objects')
const FILE_OBJECTS = join(HOME, '.dsh', 'attachments', 'v1', 'file-objects')

function humanSize(n) {
  const units = ['B', 'KB', 'MB', 'GB']
  let value = Number(n) || 0
  for (const unit of units) {
    if (value < 1024) return unit === 'B' ? `${Math.round(value)} B` : `${value.toFixed(1)} ${unit}`
    value /= 1024
  }
  return `${value.toFixed(1)} TB`
}

function parseFrontmatter(text) {
  const meta = {}
  let body = text
  if (text.startsWith('---')) {
    const end = text.indexOf('\n---', 3)
    if (end !== -1) {
      const head = text.slice(3, end)
      body = text.slice(end + 4)
      for (const line of head.split('\n')) {
        const at = line.indexOf(':')
        if (at > 0) meta[line.slice(0, at).trim()] = line.slice(at + 1).trim().replace(/^['"]|['"]$/g, '')
      }
    }
  }
  return { meta, body }
}

async function listDirs(root) {
  try {
    return await readdir(root, { withFileTypes: true })
  } catch {
    return []
  }
}

async function listSkills(cwd) {
  const roots = []
  if (cwd) {
    roots.push([join(cwd, '.dsh', 'skills'), '项目'], [join(cwd, '.agents', 'skills'), '项目'])
  }
  roots.push(
    [join(HOME, '.dsh', 'skills'), 'DSH'],
    [join(HOME, '.agents', 'skills'), 'Agents'],
    [join(HOME, '.claude', 'skills'), 'Claude'],
    [join(HOME, '.codex', 'skills'), 'Codex'],
  )
  const seen = new Set()
  const out = []
  for (const [root, src] of roots) {
    for (const entry of await listDirs(root)) {
      if (!entry.isDirectory()) continue
      const file = join(root, entry.name, 'SKILL.md')
      try {
        const text = await readFile(file, 'utf8')
        const { meta, body } = parseFrontmatter(text)
        const name = String(meta.name || entry.name).trim()
        if (!name || seen.has(name)) continue
        seen.add(name)
        out.push({
          name,
          desc: String(meta.description || body.trim().split('\n')[0] || '').slice(0, 160),
          src,
          path: file,
        })
      } catch {
        /* not a skill */
      }
    }
  }
  return out
}

async function memoryItems(cwd) {
  const paths = []
  if (cwd) paths.push(join(cwd, 'AGENTS.md'), join(cwd, 'CLAUDE.md'))
  paths.push(join(HOME, '.dsh', 'AGENTS.md'), join(HOME, 'AGENTS.md'), join(HOME, 'CLAUDE.md'))
  const seen = new Set()
  const out = []
  for (const path of paths) {
    if (seen.has(path)) continue
    seen.add(path)
    try {
      const info = await stat(path)
      if (!info.isFile()) continue
      const body = (await readFile(path, 'utf8')).trim()
      if (!body) continue
      out.push({
        id: path,
        title: path.split('/').pop() + (path.startsWith(join(HOME, '.dsh')) ? ' · 全局' : ''),
        body: body.slice(0, 12000),
        src: path,
        updated: relativeTime(info.mtimeMs),
        path,
      })
    } catch {
      /* missing */
    }
  }
  return out
}

async function readLogs(limit = 200) {
  const entries = []
  for (const entry of await listDirs(LOG_DIR)) {
    if (!entry.isFile()) continue
    const path = join(LOG_DIR, entry.name)
    try {
      entries.push({ path, mtime: (await stat(path)).mtimeMs })
    } catch {
      /* skip */
    }
  }
  entries.sort((a, b) => b.mtime - a.mtime)
  if (!entries.length) return { file: '', lines: [] }
  const file = entries[0].path
  let text = ''
  try {
    text = await readFile(file, 'utf8')
  } catch {
    return { file, lines: [] }
  }
  const lines = text.split(/\r?\n/).slice(-limit).map((line) => {
    const m = line.match(/^\s*(\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(?:\[?(\w+)\]?\s+)?(.*)$/)
    return m
      ? { t: m[1], level: (m[2] || 'info').toLowerCase(), m: m[3].slice(0, 500) }
      : { t: '', level: 'info', m: line.slice(0, 500) }
  })
  return { file, lines }
}

async function listFiles(dir, relBase = '') {
  if (!dir) return []
  let entries = []
  try {
    entries = await readdir(dir, { withFileTypes: true })
  } catch {
    return []
  }
  entries.sort((a, b) => Number(!a.isDirectory()) - Number(!b.isDirectory()) || a.name.localeCompare(b.name))
  const out = []
  for (const entry of entries.slice(0, 120)) {
    if (entry.name.startsWith('.') && entry.name !== '.dsh' && entry.name !== '.agents') continue
    const path = join(dir, entry.name)
    try {
      const info = await stat(path)
      const image = /\.(png|jpe?g|webp|gif|svg)$/i.test(entry.name)
      out.push({
        name: entry.name,
        type: entry.isDirectory() ? 'dir' : image ? 'img' : 'file',
        meta: `${entry.isDirectory() ? '目录' : humanSize(info.size)} · ${relativeTime(info.mtimeMs)}`,
        path: relBase ? `${relBase}/${entry.name}` : entry.name,
      })
    } catch {
      /* skip */
    }
  }
  return out
}


/** Detect a browser-safe image MIME from magic bytes. */
function imageMime(data) {
  if (data.length >= 8 && data[0] === 0x89 && data[1] === 0x50 && data[2] === 0x4e && data[3] === 0x47) return 'image/png'
  if (data.length >= 3 && data[0] === 0xff && data[1] === 0xd8 && data[2] === 0xff) return 'image/jpeg'
  if (data.length >= 6 && data.toString('ascii', 0, 3) === 'GIF') return 'image/gif'
  if (data.length >= 12 && data.toString('ascii', 0, 4) === 'RIFF' && data.toString('ascii', 8, 12) === 'WEBP') return 'image/webp'
  return ''
}

/** Images carried by a content array. */
function contentImages(content, meta) {
  const parts = Array.isArray(content) ? content : []
  return parts
    .filter((part) => part && part.type === 'image' && part.attachment)
    .map((part) => ({ ...part.attachment, path: meta?.path || '' }))
}

/** Files carried by a content array. */
function contentFiles(content) {
  const parts = Array.isArray(content) ? content : []
  return parts
    .filter((part) => part && part.type === 'file' && part.attachment)
    .map((part) => ({ ...part.attachment }))
}

/** Images carried by a tool result message. */
function toolImages(message, meta) {
  return contentImages(message?.content, meta)
}

/** Turn a failed turn-end reason into a visible error block. */
function failureBlock(reason) {
  const err = reason?.error ?? {}
  const code = String(err.code ?? (reason?.kind === 'max-tokens' ? 'MAX_TOKENS' : 'UNKNOWN'))
  const status = err.status ? Number(err.status) : undefined
  const message = String(err.message ?? '')
  let title = '模型调用失败'
  if (code === 'QUOTA' || code === 'ACCOUNT_QUOTA' || status === 402 || /insufficient|balance|quota|额度|余额/i.test(message)) title = '额度不足 / 配额耗尽'
  else if (code === 'MISSING_CREDENTIAL') title = '缺少 API Key'
  else if (code === 'MAX_TOKENS' || reason?.kind === 'max-tokens') title = '输出达到 token 上限'
  else if (code === 'REQUEST_EXTENSION') title = '请求准备失败'
  else if (reason?.kind === 'blocked') title = '请求被阻止'
  return {
    type: 'error',
    title,
    code,
    status,
    message: message || (reason?.kind === 'max-tokens' ? '本步输出达到上限，可以继续让它接着写。' : ''),
  }
}

/** Map one durable event to a compact patch the live trace can apply. */
function eventPatch(event) {
  const type = event?.type
  const data = event?.data ?? {}
  if (type === 'user/message') {
    const content = Array.isArray(data.content) ? data.content : []
    const text = textOfContent(content)
    const images = contentImages(content, data.meta)
    const files = contentFiles(content)
    const source = String(data.source?.kind || 'user')
    if (!isHumanSource(source)) {
      return text
        ? { blocks: [{ type: 'system', title: sourceLabel(source), body: text.slice(0, 1600), icon: 'info', tone: 'blue', source }] }
        : null
    }
    return text || images.length || files.length
      ? {
          blocks: [{ type: 'user', text: text.slice(0, 12000), images, files, source }],
          userRpcId: String(data.source?.rpcId ?? ''),
        }
      : null
  }
  if (type === 'assistant/message') {
    const content = Array.isArray(data.message?.content) ? data.message.content : []
    const reasoning = content
      .filter((part) => part && part.type === 'reasoning' && typeof part.text === 'string' && part.text)
      .map((part) => part.text)
      .join('\n\n')
      .trim()
    const text = content
      .filter((part) => part && part.type === 'text' && typeof part.text === 'string' && part.text)
      .map((part) => part.text)
      .join('\n\n')
      .trim()
    const blocks = []
    if (reasoning) blocks.push({ type: 'thinking', html: mdToHtml(reasoning, 20000), chars: reasoning.length })
    if (text) blocks.push({ type: 'assistant', html: mdToHtml(text) })
    return { blocks, clearLive: true }
  }
  if (type === 'tool/call') {
    const callId = String(data.callId ?? '')
    const name = String(data.name ?? 'tool')
    return {
      blocks: [{
        type: 'tool',
        name,
        arg: argSummary(name, data.arguments),
        status: '运行中',
        body: prettyArgs(data.arguments),
        _callId: callId,
      }],
    }
  }
  if (type === 'tool/result') {
    const callId = String(data.toolCallId ?? data.message?.toolCallId ?? '')
    return {
      toolResult: {
        callId,
        body: messageText(data.message).slice(0, 6000),
        status: data.message?.isError ? '失败' : '完成',
        images: toolImages(data.message, data.meta),
      },
    }
  }
  if (type === 'approval/asked') {
    return {
      approval: {
        id: String(data.id ?? ''),
        tool: String(data.toolName ?? ''),
        reason: String(data.reason ?? '').slice(0, 600),
        callId: String(data.callId ?? ''),
      },
      state: 'waiting',
      stateLabel: '待授权',
    }
  }
  if (type === 'approval/decided') {
    return { approvalDecided: String(data.id ?? '') }
  }
  if (type === 'compaction/summary') {
    const summary = textOfContent(data.summary)
    return { blocks: [{ type: 'compaction', text: `上下文已压缩${summary ? ` · ${summary.slice(0, 120)}` : ''}` }] }
  }
  if (type === 'request/header') {
    const cfg = data.header?.config ?? {}
    return { model: String(cfg.model ?? ''), engine: String(cfg.provider ?? '') }
  }
  if (type === 'session/title') {
    return { title: String(data.title ?? '').trim() }
  }
  if (type === 'turn/start') {
    return { state: 'running', stateLabel: '运行中' }
  }
  if (type === 'turn/end') {
    const reason = data.reason ?? {}
    const failed = reason.kind === 'error' || reason.kind === 'max-tokens' || reason.kind === 'blocked'
    const patch = { state: failed ? 'error' : 'idle', stateLabel: failed ? '失败' : '空闲', clearLive: true }
    if (failed) patch.blocks = [failureBlock(reason)]
    return patch
  }
  if (type === 'session/end-seed') {
    return { state: 'idle', stateLabel: '空闲' }
  }
  return null
}

export const inject = ['webServer', 'sessionController']

/**
 * Register the UI and its session bridge on the host web server.
 *
 * A functional plugin (rather than a Service) keeps this bridge from owning a
 * public service name: it only reads and writes through `sessionController`.
 *
 * @param {import('@deepseek-ai/cordis').Context} ctx - host context.
 */
export function apply(ctx) {
  const cache = new Map()
  let listCache = { at: 0, value: null }
  const activeFollows = new Map()
  const pendingApprovals = new Map()
  const mobilePresence = new Map()
  const PRESENCE_TTL = 45_000
  let vapidPublic = ''
  let pushReady = false

  const ensurePush = async () => {
    if (pushReady) return true
    try {
      let keys
      try {
        keys = JSON.parse(await readFile(VAPID_FILE, 'utf8'))
      } catch {
        keys = webpush.generateVAPIDKeys()
        await mkdir(DSH_STATE, { recursive: true })
        await writeFile(VAPID_FILE, JSON.stringify(keys), { mode: 0o600 })
        await chmod(VAPID_FILE, 0o600).catch(() => {})
      }
      if (!keys?.publicKey || !keys?.privateKey) throw new Error('invalid VAPID key file')
      webpush.setVapidDetails('mailto:mobilevc@example.com', keys.publicKey, keys.privateKey)
      vapidPublic = keys.publicKey
      pushReady = true
      return true
    } catch (error) {
      ctx.logger?.warn?.(`mobilevc: push unavailable: ${error?.message || error}`)
      return false
    }
  }

  const readPushSubs = async () => {
    try {
      const value = JSON.parse(await readFile(PUSH_FILE, 'utf8'))
      return Array.isArray(value) ? value.filter((item) => item?.endpoint && item?.keys?.p256dh && item?.keys?.auth) : []
    } catch {
      return []
    }
  }

  const writePushSubs = async (subs) => {
    await mkdir(DSH_STATE, { recursive: true })
    const unique = [...new Map(subs.map((item) => [item.endpoint, item])).values()]
    await writeFile(PUSH_FILE, JSON.stringify(unique, null, 2), { mode: 0o600 })
    await chmod(PUSH_FILE, 0o600).catch(() => {})
    return unique.length
  }

  const sendPush = async (payload) => {
    if (!(await ensurePush())) return 0
    const subs = await readPushSubs()
    let sent = 0
    let failed = 0
    const errors = []
    const keep = []
    for (const sub of subs) {
      try {
        await webpush.sendNotification(sub, JSON.stringify(payload), { TTL: 3600 })
        sent += 1
        keep.push(sub)
      } catch (error) {
        failed += 1
        const code = Number(error?.statusCode || 0)
        errors.push(`${code || 'error'}: ${String(error?.message || error).slice(0, 120)}`)
        if (code !== 404 && code !== 410) keep.push(sub)
      }
    }
    if (keep.length !== subs.length) await writePushSubs(keep).catch(() => {})
    return { sent, failed, subscriptions: subs.length, errors: errors.slice(0, 3) }
  }

  const json = (res, status, body) => {
    const text = JSON.stringify(body)
    res.writeHead(status, {
      'Content-Type': 'application/json; charset=utf-8',
      'Content-Length': Buffer.byteLength(text),
      'Cache-Control': 'no-store',
    })
    res.end(text)
  }

  const readJson = async (req) => {
    const chunks = []
    let size = 0
    for await (const chunk of req) {
      size += chunk.length
      if (size > 30_000_000) throw new Error('body too large')
      chunks.push(chunk)
    }
    return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')
  }

  const inspect = async (id) => {
    const hit = cache.get(id)
    if (hit && Date.now() - hit.at < 1200) return hit.value
    const value = await ctx.sessionController.inspect(id, AbortSignal.timeout(20_000))
    cache.set(id, { at: Date.now(), value })
    return value
  }

  const sessions = async (limit) => {
    if (listCache.value && Date.now() - listCache.at < 1500) return listCache.value.slice(0, limit)
    const result = await ctx.sessionController.list({}, AbortSignal.timeout(20_000))
    const items = [...(result.items ?? [])].sort((a, b) => Number(b.updatedAt ?? 0) - Number(a.updatedAt ?? 0))
    const rows = await Promise.all(
      items.slice(0, Math.max(1, Math.min(limit, 30))).map(async (item) => {
        try {
          const inspection = await inspect(String(item.sessionId))
          return summaryFromInspection(item, inspection)
        } catch {
          return {
            id: String(item.sessionId),
            title: String(item.sessionId),
            cwd: String(item.cwd ?? ''),
            engine: 'DSH',
            model: '',
            state: item.running ? 'running' : 'idle',
            stateLabel: item.running ? '运行中' : '空闲',
            last: '',
            time: relativeTime(item.updatedAt),
            updatedAt: Number(item.updatedAt ?? 0),
            pending: 0,
            blockCount: 0,
            userCount: 0,
            assistantCount: 0,
            subagent: item.origin === 'subagent',
          }
        }
      }),
    )
    listCache = { at: Date.now(), value: rows }
    return rows.slice(0, limit)
  }

  const sessionCwd = async (id) => String((await inspect(String(id)))?.meta?.cwd ?? '')

  const contained = (root, rel) => {
    const base = resolve(root)
    const target = resolve(base, String(rel || ''))
    if (target !== base && !target.startsWith(base + sep)) throw new Error('forbidden path')
    return target
  }

  const serveStatic = async (res, pathname) => {
    let rel = pathname.slice(BASE.length).replace(/^\/+/, '')
    if (!rel || rel === 'mobilevc') rel = 'index.html'
    const target = resolve(PREVIEW_ROOT, rel)
    if (!target.startsWith(`${PREVIEW_ROOT}/`) && target !== PREVIEW_ROOT) {
      json(res, 403, { error: 'forbidden' })
      return
    }
    try {
      const data = await readFile(target)
      res.writeHead(200, {
        'Content-Type': MIME[extname(target).toLowerCase()] ?? 'application/octet-stream',
        'Content-Length': data.length,
        'Cache-Control': 'no-store',
      })
      res.end(data)
    } catch {
      json(res, 404, { error: 'not found' })
    }
  }

  const handle = async (req, res) => {
    const url = new URL(req.url ?? '/', 'http://mobilevc.local')
    const path = url.pathname
    try {
      if (req.method === 'GET' && (path === BASE || path === `${BASE}/`)) {
        await serveStatic(res, `${BASE}/`)
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/health`) {
        json(res, 200, { ok: true, plugin: 'mobilevc', interactive: true })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/sessions`) {
        const raw = Number(url.searchParams.get('limit') ?? 12)
        json(res, 200, { sessions: await sessions(Number.isFinite(raw) ? raw : 12) })
        return
      }
      if (req.method === 'GET' && path.startsWith(`${BASE}/api/sessions/`)) {
        const id = decodeURIComponent(path.slice(`${BASE}/api/sessions/`.length))
        json(res, 200, inspectToBlocks(await inspect(id)))
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/queue`) {
        const sid = String(url.searchParams.get('session') ?? '')
        if (!sid) { json(res, 400, { error: 'session is required' }); return }
        try {
          const agent = await ctx.sessionController.resolveAgent(sid)
          const items = [...(agent.inbox?.nextTurn ?? []), ...(agent.inbox?.nextStep ?? [])].map((message) => ({
            id: String(message.id ?? ''),
            text: textOfContent(message.content).slice(0, 4000),
            rpcId: String(message.source?.rpcId ?? ''),
          })).filter((item) => item.id)
          json(res, 200, { items })
        } catch (error) {
          json(res, 200, { items: [], error: error instanceof Error ? error.message : String(error) })
        }
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/queue`) {
        if (req.headers['x-mobilevc'] !== '1') { json(res, 403, { error: 'missing mobilevc marker' }); return }
        const body = await readJson(req)
        const sid = String(body.sessionId ?? '')
        const itemId = String(body.itemId ?? '')
        const actionName = String(body.action ?? '')
        if (!sid || !itemId || !['edit', 'remove', 'steer'].includes(actionName)) {
          json(res, 400, { error: 'sessionId, itemId and action are required' })
          return
        }
        let action
        if (actionName === 'edit') {
          const text = String(body.text ?? '').trim()
          if (!text) { json(res, 400, { error: 'text is required' }); return }
          action = { kind: 'edit', content: [{ type: 'text', text: text.slice(0, 20_000) }] }
        } else {
          action = { kind: actionName }
        }
        await ctx.sessionController.updateQueue({ sessionId: sid, itemId, action })
        listCache = { at: 0, value: null }
        cache.delete(sid)
        json(res, 200, { ok: true })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/account`) {
        const account = ctx.get?.('accountController') || ctx.accountController
        const credentials = ctx.get?.('credentials') || ctx.credentials
        const fallback = {
          available: Boolean(account || credentials),
          status: 'unknown',
          links: { topUpUrl: 'https://platform.deepseek.com/top_up', usageUrl: 'https://platform.deepseek.com/usage' },
          balance: null,
        }
        if (!account && !credentials) { json(res, 200, { ...fallback, available: false }); return }
        let state = null
        try { if (account) state = await account.getState() } catch (_) {}
        const links = state?.links || fallback.links
        let balance = null
        let status = String(state?.status || 'unknown')
        const client = {
          version: 'mobilevc/0.1.0',
          locale: 'zh-CN',
          timezoneOffsetSeconds: -new Date().getTimezoneOffset() * 60,
        }

        // Prefer the signed-in Platform account. When DSH is using a raw
        // DEEPSEEK_API_KEY instead, fall back to the official user/balance API.
        if (account && status === 'credential-stored') {
          try {
            const raw = await Promise.race([
              account.getBalance(client),
              new Promise((_, reject) => setTimeout(() => reject(new Error('balance timeout')), 8000)),
            ])
            if (raw?.status === 'ready') {
              balance = {
                ready: true,
                source: 'account',
                wallets: Array.isArray(raw.value) ? raw.value : [],
                bonusWallets: Array.isArray(raw.bonusWallets) ? raw.bonusWallets : [],
              }
            } else if (raw?.status === 'failed') {
              balance = { ready: false }
            }
          } catch (error) {
            balance = { ready: false, error: String(error?.message || error).slice(0, 160) }
          }
        }

        if (!balance?.ready && credentials) {
          try {
            const resolved = await credentials.resolve('DEEPSEEK_API_KEY')
            if (resolved?.value) {
              const ac = new AbortController()
              const timer = setTimeout(() => ac.abort(), 8000)
              let raw = null
              try {
                const response = await fetch('https://api.deepseek.com/user/balance', {
                  headers: { Authorization: `Bearer ${resolved.value}`, Accept: 'application/json' },
                  signal: ac.signal,
                })
                if (!response.ok) throw new Error(`balance HTTP ${response.status}`)
                raw = await response.json()
              } finally { clearTimeout(timer) }
              const infos = Array.isArray(raw?.balance_infos) ? raw.balance_infos : []
              balance = {
                ready: true,
                source: 'api-key',
                wallets: infos.map((item) => ({ currency: String(item.currency || 'CNY'), balance: String(item.total_balance ?? '0') })),
                bonusWallets: infos
                  .filter((item) => Number(item.granted_balance) > 0)
                  .map((item) => ({ currency: String(item.currency || 'CNY'), balance: String(item.granted_balance) })),
              }
              status = 'api-key'
            }
          } catch (error) {
            if (!balance) balance = { ready: false, error: String(error?.message || error).slice(0, 160) }
          }
        }

        json(res, 200, {
          available: true,
          status,
          links: { topUpUrl: String(links.topUpUrl || fallback.links.topUpUrl), usageUrl: String(links.usageUrl || fallback.links.usageUrl) },
          balance,
        })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/push/key`) {
        if (!(await ensurePush())) { json(res, 503, { error: 'push unavailable' }); return }
        json(res, 200, { publicKey: vapidPublic })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/push/status`) {
        json(res, 200, { subscriptions: (await readPushSubs()).length, ready: await ensurePush() })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/push/subscribe`) {
        if (req.headers['x-mobilevc'] !== '1') { json(res, 403, { error: 'missing mobilevc marker' }); return }
        const body = await readJson(req)
        const sub = body?.subscription ?? body
        if (!sub?.endpoint || !sub?.keys?.p256dh || !sub?.keys?.auth) { json(res, 400, { error: 'invalid subscription' }); return }
        const subs = await readPushSubs()
        subs.push({ endpoint: String(sub.endpoint), keys: { p256dh: String(sub.keys.p256dh), auth: String(sub.keys.auth) } })
        const count = await writePushSubs(subs)
        json(res, 200, { ok: true, count })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/push/unsubscribe`) {
        if (req.headers['x-mobilevc'] !== '1') { json(res, 403, { error: 'missing mobilevc marker' }); return }
        const body = await readJson(req)
        const endpoint = String(body?.endpoint ?? '')
        const subs = (await readPushSubs()).filter((item) => item.endpoint !== endpoint)
        await writePushSubs(subs)
        json(res, 200, { ok: true, count: subs.length })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/push/test`) {
        if (req.headers['x-mobilevc'] !== '1') { json(res, 403, { error: 'missing mobilevc marker' }); return }
        const result = await sendPush({ title: 'MobileVC', body: '通知已接通 · 点击查看', url: '/mobilevc/', tag: 'mobilevc-test' })
        json(res, 200, { ok: true, ...result })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/jobs`) {
        const sid = String(url.searchParams.get('session') ?? '')
        const jobs = ctx.get?.('jobs') || ctx.jobs
        if (!jobs || !sid) { json(res, 200, { jobs: [] }); return }
        try {
          json(res, 200, { jobs: jobs.list(sid).map((job) => ({
            id: String(job.id), label: String(job.label || job.kind || '任务'), status: String(job.status || ''),
            progress: String(job.progress || ''), detail: String(job.detail || ''),
            startedAt: Number(job.startedAt || 0), finishedAt: Number(job.finishedAt || 0),
          })) })
        } catch { json(res, 200, { jobs: [] }) }
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/jobs/output`) {
        const sid = String(url.searchParams.get('session') ?? '')
        const id = String(url.searchParams.get('id') ?? '')
        const jobs = ctx.get?.('jobs') || ctx.jobs
        if (!jobs || !sid || !id) { json(res, 400, { error: 'session and id are required' }); return }
        try {
          const read = jobs.readAt(id, 0, sid)
          const text = (read.chunks || []).map((chunk) => chunk.text || '').join('').slice(-12000)
          json(res, 200, { text, lossy: Boolean(read.lossy) })
        } catch (error) { json(res, 404, { error: error?.message || 'job not found' }) }
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/jobs/kill`) {
        if (req.headers['x-mobilevc'] !== '1') { json(res, 403, { error: 'missing mobilevc marker' }); return }
        const body = await readJson(req)
        const sid = String(body.sessionId ?? '')
        const id = String(body.id ?? '')
        const jobs = ctx.get?.('jobs') || ctx.jobs
        if (!jobs || !sid || !id) { json(res, 400, { error: 'sessionId and id are required' }); return }
        try { json(res, 200, { ok: true, result: jobs.kill(id, sid, 'stopped from MobileVC') }) }
        catch (error) { json(res, 500, { error: error?.message || 'kill failed' }) }
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/presence`) {
        if (req.headers['x-mobilevc'] !== '1') {
          json(res, 403, { error: 'missing mobilevc marker' })
          return
        }
        const body = await readJson(req)
        const sid = String(body.sessionId ?? '')
        if (!sid) { json(res, 400, { error: 'sessionId is required' }); return }
        mobilePresence.set(sid, Date.now() + PRESENCE_TTL)
        json(res, 200, { ok: true })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/prompt`) {
        if (req.headers['x-mobilevc'] !== '1') {
          json(res, 403, { error: 'missing mobilevc marker' })
          return
        }
        const body = await readJson(req)
        const text = String(body.text ?? '').trim()
        const sessionId = String(body.sessionId ?? '')
        if (!sessionId) {
          json(res, 400, { error: 'sessionId is required' })
          return
        }
        const content = []
        if (text) content.push({ type: 'text', text: text.slice(0, 20_000) })
        const mode = body.mode === 'steer' ? 'steer' : 'queue'
        const attachments = Array.isArray(body.attachments) ? body.attachments.slice(0, 8) : []
        for (const att of attachments) {
          const kind = String(att?.kind ?? '')
          const data = String(att?.data ?? '')
          const name = String(att?.name ?? '')
          if (kind === 'image') {
            const mediaType = String(att?.mediaType ?? '')
            if (!/^image\/(png|jpeg|webp|gif)$/i.test(mediaType) || !data) throw new Error('unsupported image attachment')
            content.push({ type: 'image', mediaType, data, name })
          } else if (kind === 'file') {
            const uploads = ctx.get?.('fileUploads')
            if (!uploads) throw new Error('file upload service unavailable')
            const bytes = Buffer.from(data, 'base64')
            if (!bytes.length) throw new Error('empty file attachment')
            async function* chunks() { yield bytes }
            const uploaded = await uploads.uploadStream({ sessionId, data: chunks(), name })
            content.push({ type: 'file', receiptId: uploaded.receiptId })
          }
        }
        if (!content.length) {
          json(res, 400, { error: 'text or an attachment is required' })
          return
        }
        await ctx.sessionController.prompt(
          {
            requestId: String(body.requestId ?? `mvc-${randomUUID()}`),
            sessionId,
            mode,
            content,
            clientTimeZone: String(body.clientTimeZone ?? 'UTC'),
          },
          AbortSignal.timeout(30_000),
        )
        listCache = { at: 0, value: null }
        cache.delete(sessionId)
        json(res, 200, { ok: true })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/cancel`) {
        if (req.headers['x-mobilevc'] !== '1') {
          json(res, 403, { error: 'missing mobilevc marker' })
          return
        }
        const body = await readJson(req)
        const sessionId = String(body.sessionId ?? '')
        if (!sessionId) {
          json(res, 400, { error: 'sessionId is required' })
          return
        }
        const value = ctx.sessionController.cancel({ sessionId })
        listCache = { at: 0, value: null }
        cache.delete(sessionId)
        json(res, 200, { ok: true, ...value })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/plugins`) {
        const pm = ctx.get?.('pluginManager')
        if (!pm) { json(res, 503, { error: 'plugin manager unavailable' }); return }
        const [plugins, bundles] = await Promise.all([pm.listPlugins(), pm.listBundles()])
        const label = (meta, fallback) => {
          const value = meta && (meta.title?.zh || meta.title || meta.summary?.zh || meta.summary || meta.description?.zh || meta.description)
          return typeof value === 'string' ? value : fallback
        }
        json(res, 200, {
          plugins: plugins.map((row) => ({
            id: String(row.entryId),
            moduleName: String(row.moduleName),
            enabled: Boolean(row.enabled),
            fiberPhase: String(row.fiberPhase ?? ''),
            readOnlyReason: row.readOnlyReason || '',
            label: label(row.meta, String(row.moduleName).split('/').pop() || String(row.entryId)),
          })),
          bundles: bundles.map((row) => ({
            name: String(row.name),
            version: String(row.version ?? ''),
            enabled: Boolean(row.enabled),
            installed: Boolean(row.installed),
            optional: Boolean(row.optional),
            removable: Boolean(row.removable),
            readOnlyReason: row.readOnlyReason || '',
            label: label(row.meta, String(row.name)),
            description: String(row.description ?? ''),
          })),
        })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/plugins/toggle`) {
        if (req.headers['x-mobilevc'] !== '1') { json(res, 403, { error: 'missing mobilevc marker' }); return }
        const pm = ctx.get?.('pluginManager')
        if (!pm) { json(res, 503, { error: 'plugin manager unavailable' }); return }
        const body = await readJson(req)
        const kind = String(body.kind ?? '')
        const id = String(body.id ?? '')
        const enabled = Boolean(body.enabled)
        if (!id || (kind !== 'plugin' && kind !== 'bundle')) { json(res, 400, { error: 'kind and id are required' }); return }
        const result = kind === 'bundle' ? await pm.setBundleEnabled(id, enabled) : await pm.setPluginEnabled(id, enabled)
        json(res, 200, result ?? { changed: true })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/approval`) {
        if (req.headers['x-mobilevc'] !== '1') {
          json(res, 403, { error: 'missing mobilevc marker' })
          return
        }
        const body = await readJson(req)
        const id = String(body.id ?? '')
        const outcome = String(body.outcome ?? '')
        if (!id || (outcome !== 'allowed-once' && outcome !== 'rejected')) {
          json(res, 400, { error: 'id and outcome are required' })
          return
        }
        const entry = pendingApprovals.get(id)
        if (!entry) {
          json(res, 200, { ok: false, stale: true })
          return
        }
        pendingApprovals.delete(id)
        entry.resolve(outcome)
        json(res, 200, { ok: true })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/attachment`) {
        const id = String(url.searchParams.get('id') ?? '')
        const match = /^sha256:([a-f0-9]{64})$/i.exec(id)
        if (!match) { json(res, 400, { error: 'valid sha256 attachment id required' }); return }
        const hex = match[1].toLowerCase()
        try {
          const data = await readFile(join(ATTACHMENTS, hex.slice(0, 2), hex))
          const mime = imageMime(data)
          if (!mime) { json(res, 415, { error: 'not a browser-safe image' }); return }
          res.writeHead(200, {
            'Content-Type': mime,
            'Content-Length': data.length,
            'Cache-Control': 'private, max-age=300',
            'X-Content-Type-Options': 'nosniff',
          })
          res.end(data)
        } catch {
          json(res, 404, { error: 'attachment not found' })
        }
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/file-attachment`) {
        const id = String(url.searchParams.get('id') ?? '')
        const name = String(url.searchParams.get('name') ?? 'file')
        const match = /^sha256:([a-f0-9]{64})$/i.exec(id)
        if (!match) { json(res, 400, { error: 'valid sha256 attachment id required' }); return }
        const hex = match[1].toLowerCase()
        try {
          const data = await readFile(join(FILE_OBJECTS, hex.slice(0, 2), hex))
          const safeName = name.replace(/[\r\n"]/g, '_') || 'file'
          res.writeHead(200, {
            'Content-Type': 'application/octet-stream',
            'Content-Length': data.length,
            'Content-Disposition': `attachment; filename*=UTF-8''${encodeURIComponent(safeName)}`,
            'Cache-Control': 'private, max-age=300',
            'X-Content-Type-Options': 'nosniff',
          })
          res.end(data)
        } catch {
          json(res, 404, { error: 'file attachment not found' })
        }
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/follow`) {
        const sid = String(url.searchParams.get('session') ?? '')
        if (!sid) { json(res, 400, { error: 'session is required' }); return }
        res.writeHead(200, {
          'Content-Type': 'text/event-stream; charset=utf-8',
          'Cache-Control': 'no-cache, no-transform',
          'Connection': 'keep-alive',
          'X-Accel-Buffering': 'no',
        })
        res.flushHeaders?.()
        const controller = new AbortController()
        const ping = setInterval(() => {
          try { res.write(': ping\n\n') } catch { /* closed */ }
        }, 15000)
        activeFollows.set(sid, (activeFollows.get(sid) || 0) + 1)
        let closed = false
        const close = () => {
          if (closed) return
          closed = true
          clearInterval(ping)
          controller.abort()
          const remaining = Math.max(0, (activeFollows.get(sid) || 1) - 1)
          if (remaining === 0) {
            activeFollows.delete(sid)
            // The page may be reconnecting. Keep approvals while presence is
            // fresh; reap only after the heartbeat expires.
            if ([...pendingApprovals.values()].some((entry) => entry.sessionId === sid)) reapPending(sid)
          } else {
            activeFollows.set(sid, remaining)
          }
        }
        req.on('close', close)
        res.on('close', close)
        try {
          const stream = ctx.sessionController.follow(
            { address: { kind: 'session', sessionId: sid }, assistantStream: true },
            controller.signal,
          )
          for await (const frame of stream) {
            if (controller.signal.aborted) break
            if (frame.type === 'snapshot') {
              const session = inspectToBlocks({
                meta: frame.header,
                events: (frame.records ?? []).map((record) => record.event),
                inheritedEventCount: 0,
              })
              res.write(`data: ${JSON.stringify({ kind: 'snapshot', session })}\n\n`)
            } else if (frame.type === 'event') {
              const patch = eventPatch(frame.event)
              if (patch) res.write(`data: ${JSON.stringify({ kind: 'durable', patch })}\n\n`)
            } else if (frame.type === 'assistant-stream') {
              res.write(`data: ${JSON.stringify({ kind: 'assistant-stream', frame: frame.frame })}\n\n`)
            }
          }
        } catch (error) {
          if (!controller.signal.aborted) {
            try {
              res.write(`data: ${JSON.stringify({ kind: 'error', error: error instanceof Error ? error.message : String(error) })}\n\n`)
            } catch { /* closed */ }
          }
        } finally {
          clearInterval(ping)
          if (!res.writableEnded) res.end()
        }
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/files`) {
        const sid = String(url.searchParams.get('session') ?? '')
        const rel = String(url.searchParams.get('path') ?? '').split('/').filter(Boolean).join('/')
        const cwd = await sessionCwd(sid)
        if (!cwd) { json(res, 404, { error: 'session cwd not found' }); return }
        try {
          const realRoot = await realpath(cwd)
          const target = await realpath(contained(cwd, rel))
          if (target !== realRoot && !target.startsWith(realRoot + sep)) {
            json(res, 403, { error: 'forbidden path' })
            return
          }
          const info = await stat(target)
          if (!info.isDirectory()) { json(res, 400, { error: 'not a directory' }); return }
          json(res, 200, {
            cwd,
            path: rel,
            parent: rel.includes('/') ? rel.slice(0, rel.lastIndexOf('/')) : '',
            items: await listFiles(target, rel),
          })
        } catch {
          json(res, 404, { error: 'directory not found' })
        }
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/file`) {
        const sid = String(url.searchParams.get('session') ?? '')
        const rel = String(url.searchParams.get('path') ?? '')
        const cwd = await sessionCwd(sid)
        if (!cwd) { json(res, 404, { error: 'session cwd not found' }); return }
        let target
        try {
          const realRoot = await realpath(cwd)
          target = await realpath(contained(cwd, rel))
          if (target !== realRoot && !target.startsWith(realRoot + sep)) throw new Error('forbidden')
        } catch { json(res, 403, { error: 'forbidden path' }); return }
        try {
          const info = await stat(target)
          if (!info.isFile()) { json(res, 404, { error: 'not a file' }); return }
          if (/\.(png|jpe?g|webp|gif)$/i.test(target)) {
            const data = await readFile(target)
            res.writeHead(200, {
              'Content-Type': MIME[extname(target).toLowerCase()] ?? 'application/octet-stream',
              'Content-Length': data.length,
              'Cache-Control': 'no-store',
            })
            res.end(data)
            return
          }
          const text = (await readFile(target, 'utf8')).slice(0, 200000)
          json(res, 200, { path: rel, text })
        } catch {
          json(res, 404, { error: 'not found' })
        }
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/skills`) {
        const cwd = String(url.searchParams.get('cwd') ?? '')
        json(res, 200, { skills: await listSkills(cwd) })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/memory`) {
        const cwd = String(url.searchParams.get('cwd') ?? '')
        json(res, 200, { items: await memoryItems(cwd), defaultPath: join(HOME, 'AGENTS.md') })
        return
      }
      if (req.method === 'POST' && path === `${BASE}/api/memory`) {
        if (req.headers['x-mobilevc'] !== '1') {
          json(res, 403, { error: 'missing mobilevc marker' })
          return
        }
        const body = await readJson(req)
        const target = resolve(String(body.path ?? ''))
        const base = target.split('/').pop()
        const allowedBase = base === 'AGENTS.md' || base === 'CLAUDE.md'
        const allowedPath = target.startsWith(HOME + sep) || target === join(HOME, '.dsh', 'AGENTS.md')
        if (!allowedBase || !allowedPath) {
          json(res, 403, { error: 'path not allowed' })
          return
        }
        await mkdir(dirname(target), { recursive: true })
        await writeFile(target, String(body.content ?? ''), 'utf8')
        json(res, 200, { ok: true })
        return
      }
      if (req.method === 'GET' && path === `${BASE}/api/logs`) {
        json(res, 200, await readLogs())
        return
      }
      if (req.method === 'GET' || req.method === 'HEAD') {
        await serveStatic(res, path)
        return
      }
      json(res, 405, { error: 'method not allowed' })
    } catch (error) {
      json(res, 500, { error: error instanceof Error ? error.message : String(error) })
    }
  }

  const presenceAlive = (sid) => (activeFollows.get(sid) || 0) > 0 || (mobilePresence.get(sid) || 0) > Date.now()

  const delegatePending = (sid) => {
    for (const [approvalId, entry] of pendingApprovals) {
      if (entry.sessionId !== sid) continue
      pendingApprovals.delete(approvalId)
      Promise.resolve().then(() => entry.next()).then(
        (outcome) => entry.resolve(outcome),
        () => entry.resolve('unavailable'),
      )
    }
  }

  const reapPending = (sid) => {
    const has = [...pendingApprovals.values()].some((entry) => entry.sessionId === sid)
    if (!has) return
    if (presenceAlive(sid)) {
      setTimeout(() => reapPending(sid), 5000)
      return
    }
    delegatePending(sid)
  }

  // Mobile answers first when this session has an open follow stream OR a
  // recent presence heartbeat; otherwise the desktop/Remote answerer gets it.
  ctx.effect(() => ctx.on('approval/request', async (req, next) => {
    const sid = String(req?.agent?.session?.id ?? '')
    if (!sid || !presenceAlive(sid)) return next()
    let asked
    try {
      const events = req.agent.session.snapshotEvents()
      for (let i = events.length - 1; i >= 0; i -= 1) {
        const event = events[i]
        if (event?.type !== 'approval/asked') continue
        if (event.data?.toolName !== req.toolName) continue
        if (req.callId && event.data?.callId !== req.callId) continue
        asked = event.data
        break
      }
    } catch {
      return next()
    }
    if (!asked?.id || pendingApprovals.has(String(asked.id))) return next()
    return await new Promise((resolve) => {
      const id = String(asked.id)
      const entry = { resolve, next, sessionId: sid }
      pendingApprovals.set(id, entry)
      const done = () => {
        if (pendingApprovals.get(id) === entry) pendingApprovals.delete(id)
      }
      req.signal?.addEventListener('abort', () => {
        done()
        resolve('cancelled')
      }, { once: true })
    })
  }, { prepend: true }))
  const pushNotify = (sid, title, body, tag) => {
    if (!sid || presenceAlive(sid)) return
    void sendPush({ title, body, url: '/mobilevc/', tag })
  }

  ctx.effect(() => ctx.on('session/event', (session, event) => {
    const sid = String(session?.id ?? session?.sessionId ?? '')
    if (!sid || !event) return
    if (event.type === 'turn/end') {
      const failed = event.data?.reason?.kind === 'error' || event.data?.reason?.kind === 'blocked' || event.data?.reason?.kind === 'max-tokens'
      pushNotify(sid, failed ? 'MobileVC · 任务失败' : 'MobileVC · 任务完成', failed ? 'DSH 任务执行失败，点击查看' : 'DSH 任务已完成，点击查看', `turn-${sid}`)
      return
    }
    if (event.type === 'approval/asked') {
      pushNotify(sid, 'MobileVC · 等待审批', `${String(event.data?.toolName || '工具')} 等待你的审批`, `approval-${sid}`)
    }
  }))

  // Background jobs are visible in the in-app job panel. Do not push every
  // job settlement: long agent runs would otherwise spam "后台任务结束" and
  // bury the meaningful turn/approval notifications.

  ctx.effect(() => {
    const off = ctx.webServer.register({ kind: 'prefix', path: BASE, handler: handle })
    ctx.logger?.info?.('mobilevc: UI and session bridge registered at /mobilevc/')
    return () => off()
  })
}

apply.inject = inject

export default apply
