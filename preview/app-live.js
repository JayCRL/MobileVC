/* MobileVC live — real DSH data, read-only shell */
(() => {
  'use strict';

  const $ = (s, r = document) => r.querySelector(s);
  const esc = (v) =>
    String(v ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  const I = {
    folder: '<path d="M3.5 6.8a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8.4a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2V6.8Z"/>',
    refresh: '<path d="M20 12a8 8 0 1 1-2.3-5.7"/><path d="M20 4v5h-5"/>',
    dashboard: '<rect x="3.5" y="3.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="3.5" width="7" height="4.6" rx="1.4"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.6"/><rect x="13.5" y="10.5" width="7" height="10" rx="1.6"/>',
    moon: '<path d="M20 14.6A8.6 8.6 0 0 1 9.4 4a7.1 7.1 0 1 0 10.6 10.6Z"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2.2M12 19.3v2.2M4.6 4.6l1.6 1.6M17.8 17.8l1.6 1.6M2.5 12h2.2M19.3 12h2.2M4.6 19.4l1.6-1.6M17.8 6.2l1.6-1.6"/>',
    gear: '<circle cx="12" cy="12" r="3.1"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09A1.7 1.7 0 0 0 8.9 19.3a1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.7 15a1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.7 9a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.7a1.7 1.7 0 0 0 1-1.55V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55 1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.3 9v.09a1.7 1.7 0 0 0 1.55 1H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.51 1Z"/>',
    cut: '<circle cx="6.5" cy="7" r="2.5"/><circle cx="6.5" cy="17" r="2.5"/><path d="M8.7 8.3 20 18M8.7 15.7 20 6"/>',
    history: '<path d="M3.5 12a8.5 8.5 0 1 0 2.6-6.1"/><path d="M3.5 4.5V9H8"/><path d="M12 7.5V12l3 2"/>',
    server: '<rect x="3.5" y="4" width="17" height="6.5" rx="2"/><rect x="3.5" y="13.5" width="17" height="6.5" rx="2"/><path d="M7 7.2h.01M7 16.7h.01"/>',
    terminal: '<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><path d="M7 9l3 3-3 3M13 15h4"/>',
    puzzle: '<path d="M10 3.5a1.7 1.7 0 0 1 3.4 0V5h2.1a1.5 1.5 0 0 1 1.5 1.5v2.1h1.5a1.7 1.7 0 0 1 0 3.4H17v2.5a1.5 1.5 0 0 1-1.5 1.5h-2.1v1.5a1.7 1.7 0 0 1-3.4 0V16H7.9a1.5 1.5 0 0 1-1.5-1.5V12H5a1.7 1.7 0 0 1 0-3.4h1.4V6.5A1.5 1.5 0 0 1 7.9 5H10V3.5Z"/>',
    brain: '<path d="M9.5 4.2A3 3 0 0 0 6.2 7a3 3 0 0 0-1 5.6A3 3 0 0 0 8 17.5h1.5V4.2Z"/><path d="M14.5 4.2A3 3 0 0 1 17.8 7a3 3 0 0 1 1 5.6 3 3 0 0 1-2.8 4.9h-1.5V4.2Z"/><path d="M12 4v16"/>',
    shield: '<path d="M12 3l7 2.8v5.2c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V5.8L12 3Z"/><path d="M9 12l2 2 4-4"/>',
    cpu: '<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M10 3v3M14 3v3M10 18v3M14 18v3M3 10h3M3 14h3M18 10h3M18 14h3"/>',
    image: '<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><circle cx="9" cy="10" r="1.5"/><path d="M4.5 17l5-4.5 3.5 3 3-2.5 3.5 4"/>',
    up: '<path d="M12 19V5"/><path d="M6 11l6-6 6 6"/>',
    x: '<path d="M6 6l12 12M18 6L6 18"/>',
    check: '<path d="M4.5 12.5l5 5 10-11"/>',
    file: '<path d="M6 3h7l5 5v13H6V3Z"/><path d="M13 3v5h5"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    chat: '<path d="M21 11.5a8.4 8.4 0 0 1-9 8.5 9.4 9.4 0 0 1-3.2-.6L3 21l1.6-4.5A8.4 8.4 0 0 1 12 3a8.4 8.4 0 0 1 9 8.5Z"/>',
    lock: '<rect x="4.5" y="10.5" width="15" height="10" rx="2.5"/><path d="M8 10.5V7a4 4 0 0 1 8 0v3.5"/>',
    play: '<path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" stroke="none"/>',
    stop: '<rect x="7" y="7" width="10" height="10" rx="2" fill="currentColor" stroke="none"/>',
    chevron: '<path d="M8 10l4 4 4-4"/>',
  };
  const icon = (n, cls = '') =>
    `<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${I[n] || ''}</svg>`;

  const API = 'api';
  const INTERACTIVE = /^\/mobilevc(?:\/|$)/.test(location.pathname) && !location.pathname.startsWith('/mobilevc-preview');
  const state = {
    theme: document.documentElement.dataset.theme || 'light',
    sessions: [],
    session: null,
    blocks: [],
    approvals: [],
    skills: [],
    memories: [],
    logs: null,
    files: [],
    cwd: '',
    filePath: '',
    fileParent: '',
    fileLoading: false,
    fileSeq: 0,
    attachments: [],
    optimisticUser: null,
    queue: [],
    queueSeq: 0,
    memoryDefaultPath: '',
    engine: 'DSH',
    model: '',
    loading: true,
    detailLoading: false,
    error: '',
    toastTimer: null,
    draft: '',
    sending: false,
    pollTimer: null,
    pollIdle: 0,
    contentSig: '',
    follow: null,
    followRetry: null,
    lastFollowAt: 0,
    syncBusy: false,
    resumeInFlight: false,
    live: null,
    liveRaf: null,
    liveOpen: true,
  };

  async function api(path, opts = {}) {
    const r = await fetch(`${API}/${path}`, {
      headers: { Accept: 'application/json', ...(opts.headers || {}) },
      cache: 'no-store',
      ...opts,
    });
    if (!r.ok) {
      let message = `${r.status} ${r.statusText}`;
      try {
        const body = await r.json();
        if (body?.error) message = body.error;
      } catch (_) {}
      throw new Error(message);
    }
    return r.json();
  }

  const status = () => {
    if (state.loading) return { label: 'DSH · 读取中', dot: 'busy' };
    if (!state.session) return { label: 'Shell · 未连接', dot: '' };
    if (state.sending) return { label: 'DSH · 发送中', dot: 'busy' };
    const s = state.session;
    if (s.state === 'running') return { label: `DSH · 运行中`, dot: 'busy' };
    if (s.state === 'waiting') return { label: `DSH · 待授权`, dot: 'busy' };
    if (s.state === 'error') return { label: `DSH · 失败`, dot: 'err' };
    return { label: `DSH · 空闲`, dot: 'on' };
  };

  /* ---------- data ---------- */
  async function loadSessions() {
    state.loading = true;
    state.error = '';
    render();
    try {
      const data = await api('sessions?limit=12');
      state.sessions = data.sessions || [];
      const hashId = (location.hash.match(/^#\/chat\/(.+)$/) || [])[1] || '';
      const preferred = state.sessions.find((s) => !s.subagent && s.state !== 'running' && s.assistantCount > 0);
      const target = state.sessions.find((s) => s.id === hashId) || preferred || state.sessions[0];
      if (target) {
        await selectSession(target.id, { keepLoading: true });
      } else {
        state.loading = false;
        state.session = null;
        render();
      }
    } catch (e) {
      state.loading = false;
      state.error = `读取 DSH 会话失败：${e.message}`;
      render();
    }
  }

  async function selectSession(id, opts = {}) {
    if (!id) return;
    stopPolling();
    stopFollow();
    state.draft = '';
    state.sending = false;
    state.contentSig = '';
    state.live = null;
    clearAttachments();
    state.session = null;
    state.blocks = [];
    state.approvals = [];
    state.optimisticUser = null;
    state.detailLoading = true;
    state.error = '';
    if (!opts.keepLoading) state.loading = false;
    render();
    try {
      const data = await api(`sessions/${encodeURIComponent(id)}`);
      state.session = data;
      state.blocks = data.blocks || [];
      state.approvals = data.approvals || [];
      state.engine = data.engine || 'DSH';
      state.model = data.model || '';
      state.cwd = data.cwd || '';
      state.loading = false;
      state.detailLoading = false;
      state.contentSig = contentSignature(data);
      history.replaceState(null, '', `#/chat/${id}`);
      render();
      requestAnimationFrame(scrollContentBottom);
      if (INTERACTIVE) {
        pingPresence();
        refreshQueue();
        startFollow();
      } else if (data.state === 'running') startPolling();
    } catch (e) {
      stopPolling();
      stopFollow();
      state.loading = false;
      state.detailLoading = false;
      state.error = `读取会话失败：${e.message}`;
      render();
    }
  }

  function applyDetail(data) {
    state.session = data;
    state.blocks = data.blocks || [];
    if (state.optimisticUser && !state.blocks.some((b) => b.type === 'user' && b.text === state.optimisticUser.text)) {
      state.blocks.push(state.optimisticUser.block);
    }
    state.approvals = data.approvals || [];
    state.engine = data.engine || state.engine;
    state.model = data.model || state.model;
    state.cwd = data.cwd || state.cwd;
  }

  function contentSignature(data) {
    const blocks = data.blocks || [];
    const shape = blocks.map((b) => `${b.type || ''}:${b.status || ''}:${(b.body || b.html || b.text || '').length}`).join(',');
    return `${data.state || ''}|${blocks.length}|${shape}|${data.pending || 0}`;
  }

  function isNearBottom(el, threshold = 90) {
    return !el || el.scrollHeight - el.scrollTop - el.clientHeight <= threshold;
  }

  function scrollContentBottom() {
    const el = $('#content');
    if (el) el.scrollTop = el.scrollHeight;
  }

  function renderContentPreservingOpen() {
    const el = $('#content');
    const atBottom = isNearBottom(el);
    const open = [...document.querySelectorAll('.thinking')].map((node) => node.open);
    const scrollTop = el.scrollTop;
    renderContent();
    [...document.querySelectorAll('.thinking')].forEach((node, index) => {
      if (open[index]) node.open = true;
    });
    if (atBottom) scrollContentBottom();
    else el.scrollTop = scrollTop;
  }

  function stopPolling() {
    if (state.pollTimer) clearInterval(state.pollTimer);
    state.pollTimer = null;
    state.pollIdle = 0;
  }

  function updateDockStatus() {
    const el = $('.dock-status');
    if (!el) return;
    const st = status();
    const parts = st.label.split(' · ');
    el.innerHTML = `<span class="dot ${st.dot}"></span><b>${parts[0]}</b> · ${parts[1] || ''}`;
  }

  function updateDockAction() {
    const s = state.session;
    const run = $('.run-btn');
    if (run) {
      const active = Boolean(s && s.state === 'running');
      run.hidden = !active;
      run.classList.toggle('active', active);
    }
    const send = $('.send-btn');
    if (send) send.classList.toggle('busy', Boolean(state.sending));
  }

  function startPolling() {
    if (!INTERACTIVE || !state.session?.id || state.follow) return;
    stopPolling();
    state.pollIdle = 0;
    state.pollTimer = setInterval(async () => {
      if (!state.session?.id) { stopPolling(); return; }
      try {
        const data = await api(`sessions/${encodeURIComponent(state.session.id)}`);
        applyDetail(data);
        const active = data.state === 'running' || data.state === 'waiting';
        state.pollIdle = active ? 0 : state.pollIdle + 1;
        renderAppbar();
        const sig = contentSignature(data);
        if (sig !== state.contentSig) {
          state.contentSig = sig;
          renderContentPreservingOpen();
        }
        updateDockStatus();
        updateDockAction();
        if (!active && state.pollIdle >= 2 && !state.sending) stopPolling();
      } catch (_) {
        /* keep the current view on a transient poll failure */
      }
    }, 1600);
  }


  function stopFollow() {
    if (state.followRetry) {
      clearTimeout(state.followRetry);
      state.followRetry = null;
    }
    if (state.follow) {
      try { state.follow.close(); } catch (_) {}
      state.follow = null;
    }
    if (state.liveRaf) cancelAnimationFrame(state.liveRaf);
    state.liveRaf = null;
  }

  function startFollow(opts = {}) {
    if (!INTERACTIVE || !state.session?.id) return;
    if (typeof EventSource === 'undefined') return;
    const sid = state.session.id;
    if (!opts.force && state.follow && state.follow.__sid === sid) return;
    stopFollow();
    stopPolling();
    const es = new EventSource(`${API}/follow?session=${encodeURIComponent(sid)}`);
    es.__sid = sid;
    state.follow = es;
    let opened = false;
    let warned = false;
    es.onopen = () => { opened = true; state.lastFollowAt = Date.now(); };
    es.onmessage = (event) => {
      state.lastFollowAt = Date.now();
      try { handleFollowMessage(JSON.parse(event.data)); } catch (_) {}
    };
    es.onerror = () => {
      if (state.follow !== es) return;
      try { es.close(); } catch (_) {}
      state.follow = null;
      if (!opened && !warned) {
        warned = true;
        toast('实时流未建立，正在重连…');
      }
      // 页面在后台时不要重连；回到前台会统一 refresh + 重开
      if (document.hidden) return;
      // 断开期间先用轮询兜底，避免完全空白
      startPolling();
      if (state.followRetry) clearTimeout(state.followRetry);
      const delay = 1200 + Math.floor(Math.random() * 800);
      state.followRetry = setTimeout(() => {
        state.followRetry = null;
        if (state.session?.id === sid && !document.hidden) startFollow({ force: true });
      }, delay);
    };
  }

  function handleFollowMessage(msg) {
    if (!msg || !state.session) return;
    if (msg.kind === 'snapshot') {
      const detail = msg.session;
      if (!detail || (detail.id && detail.id !== state.session.id)) return;
      applyDetail(detail);
      state.contentSig = contentSignature(detail);
      state.live = null;
      renderContentPreservingOpen();
      updateDockStatus();
      updateDockAction();
      return;
    }
    if (msg.kind === 'durable') {
      applyPatch(msg.patch);
      return;
    }
    if (msg.kind === 'assistant-stream') {
      handleAssistantFrame(msg.frame);
      return;
    }
    if (msg.kind === 'error') {
      toast(`实时流：${msg.error || 'error'}`);
    }
  }

  function appendBlocksToTimeline(blocks) {
    const timeline = $('#timeline');
    const slot = $('#live-slot');
    if (!timeline || !slot) {
      renderContentPreservingOpen();
      return;
    }
    for (const block of blocks) {
      const html = chatBlock(block);
      if (!html) continue;
      const tmp = document.createElement('div');
      tmp.innerHTML = html;
      while (tmp.firstChild) timeline.insertBefore(tmp.firstChild, slot);
    }
  }

  function updateApprovalSlot() {
    const slot = $('#approval-slot');
    if (slot) slot.innerHTML = state.approvals.map(approvalCard).join('');
  }

  function updateToolResult(result) {
    const timeline = $('#timeline');
    if (!timeline || !result) return;
    const card = [...timeline.querySelectorAll('.tool-card')].find((node) => node.dataset.callId === result.callId);
    if (!card) return;
    const status = card.querySelector('.tool-status');
    if (status) {
      status.textContent = result.status || '完成';
      status.classList.toggle('running', result.status === '运行中');
      status.classList.toggle('error', result.status === '失败');
    }
    const body = card.querySelector('.tool-body');
    if (body && result.body) body.textContent = result.body;
    if (Array.isArray(result.images)) {
      const content = card.querySelector('.tool-content');
      const existing = card.querySelector('.tool-images');
      if (existing) existing.remove();
      if (content && result.images.length) content.insertAdjacentHTML('afterbegin', toolImagesHtml(result.images));
    }
  }

  function removeOptimisticUser() {
    const opt = state.optimisticUser;
    if (!opt) return;
    state.optimisticUser = null;
    state.blocks = state.blocks.filter((block) => block !== opt.block);
    try {
      const selector = `[data-optimistic="${CSS.escape(opt.rpcId)}"]`;
      document.querySelector(selector)?.remove();
    } catch (_) {}
    updateQueueChip();
  }

  function applyPatch(patch) {
    if (!patch || !state.session) return;
    const el = $('#content');
    const stick = isNearBottom(el);
    if (patch.userRpcId && state.optimisticUser?.rpcId === patch.userRpcId) removeOptimisticUser();
    else if (state.optimisticUser && patch.blocks?.some((b) => b.type === 'user' && b.text === state.optimisticUser.text)) removeOptimisticUser();
    if (patch.title) state.session.title = patch.title;
    if (patch.model) state.model = patch.model;
    if (patch.engine) state.engine = patch.engine;
    if (patch.blocks?.length) {
      for (const block of patch.blocks) state.blocks.push(block);
      if (state.blocks.length > 200) state.blocks = state.blocks.slice(-200);
      appendBlocksToTimeline(patch.blocks);
    }
    if (patch.toolResult) {
      const block = state.blocks.find((b) => b.type === 'tool' && b._callId === patch.toolResult.callId);
      if (block) {
        block.status = patch.toolResult.status || '完成';
        block.body = patch.toolResult.body || block.body;
      }
      updateToolResult(patch.toolResult);
    }
    if (patch.approval) {
      if (!state.approvals.some((a) => a.id === patch.approval.id)) state.approvals.push(patch.approval);
      updateApprovalSlot();
    }
    if (patch.approvalDecided) {
      state.approvals = state.approvals.filter((a) => a.id !== patch.approvalDecided);
      updateApprovalSlot();
    }
    if (patch.state) {
      const waiting = patch.state === 'waiting' || state.approvals.length > 0;
      state.session.state = waiting ? 'waiting' : patch.state;
      state.session.stateLabel = waiting ? '待授权' : patch.stateLabel || (patch.state === 'running' ? '运行中' : '空闲');
    }
    if (patch.clearLive) {
      state.live = null;
      updateLiveSlot();
    }
    state.contentSig = contentSignature({
      state: state.session.state,
      blocks: state.blocks,
      pending: state.approvals.length,
    });
    if (patch.title) renderAppbar();
    updateDockStatus();
    updateDockAction();
    if (stick) scrollContentBottom();
  }

  function updateLiveSlot() {
    const slot = $('#live-slot');
    if (!slot) return;
    const live = state.live;
    if (!live || (!live.reasoning && !live.text)) {
      slot.innerHTML = '';
      return;
    }
    let think = slot.querySelector('[data-live-think]');
    if (live.reasoning) {
      if (!think) {
        slot.insertAdjacentHTML('afterbegin', `<div class="row"><details class="thinking live" data-live-think="1"><summary><span class="think-ico">${icon('brain')}</span><span class="think-label">正在思考</span><span class="think-chars">0 字</span><span class="think-caret">${icon('chevron')}</span></summary><div class="think-body"></div></details></div>`);
        think = slot.querySelector('[data-live-think]');
        think.open = state.liveOpen;
        think.addEventListener('toggle', () => { state.liveOpen = think.open; }, { once: true });
      }
      if (state.liveOpen && !think.open) think.open = true;
      const body = think.querySelector('.think-body');
      if (body && body.textContent !== live.reasoning) body.textContent = live.reasoning;
      const chars = think.querySelector('.think-chars');
      if (chars) chars.textContent = `${live.reasoning.length} 字`;
    } else if (think) {
      think.closest('.row')?.remove();
    }
    let text = slot.querySelector('.live-text');
    if (live.text) {
      if (!text) {
        slot.insertAdjacentHTML('beforeend', '<div class="row"><div class="card-md live-text"></div></div>');
        text = slot.querySelector('.live-text');
      }
      if (text.textContent !== live.text) text.textContent = live.text;
    } else if (text) {
      text.closest('.row')?.remove();
    }
  }

  function liveHtml() {
    const live = state.live;
    if (!live || (!live.reasoning && !live.text)) return '';
    let html = '';
    if (live.reasoning) {
      html += `<div class="row"><details class="thinking live"${state.liveOpen ? ' open' : ''} data-live-think="1"><summary><span class="think-ico">${icon('brain')}</span><span class="think-label">正在思考</span><span class="think-chars">${live.reasoning.length} 字</span><span class="think-caret">${icon('chevron')}</span></summary><div class="think-body">${esc(live.reasoning).replace(/\n/g, '<br>')}</div></details></div>`;
    }
    if (live.text) {
      html += `<div class="row"><div class="card-md live-text">${esc(live.text).replace(/\n/g, '<br>')}</div></div>`;
    }
    return html;
  }

  async function decideApproval(id, outcome, button) {
    if (!id) return;
    if (button) button.disabled = true;
    try {
      const r = await fetch(`${API}/approval`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({ id, outcome }),
      });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error || `${r.status} ${r.statusText}`);
      state.approvals = state.approvals.filter((a) => a.id !== id);
      updateApprovalSlot();
      if (state.session?.state === 'waiting') {
        state.session.state = 'running';
        state.session.stateLabel = '运行中';
      }
      updateDockStatus();
      updateDockAction();
      toast(data.stale ? '已在其他端处理，这次点击已失效' : (outcome === 'allowed-once' ? '已允许一次' : '已拒绝'));
    } catch (e) {
      if (button) button.disabled = false;
      toast(`审批失败：${e.message}`);
    }
  }

  async function refreshSession(opts = {}) {
    if (!state.session?.id) return;
    const { silent = false, forceFollow = false, preserve = false } = opts;
    try {
      const data = await api(`sessions/${encodeURIComponent(state.session.id)}`);
      applyDetail(data);
      state.contentSig = contentSignature(data);
      if (preserve) {
        renderAppbar();
        renderContentPreservingOpen();
        updateDockStatus();
        updateDockAction();
      } else {
        render();
      }
      if (INTERACTIVE) {
        pingPresence();
        startFollow({ force: forceFollow });
      } else if (data.state === 'running') startPolling();
      if (!silent) toast('已刷新 DSH 数据');
    } catch (e) {
      if (!silent) toast(`刷新失败：${e.message}`);
    }
  }

  async function refreshSessionsOnly() {
    try {
      const data = await api('sessions?limit=12');
      state.sessions = data.sessions || [];
      const title = document.querySelector('#sheet-root .sheet-head h2')?.textContent || '';
      if (title.startsWith('会话')) setSheetBody(sessionsBody());
    } catch (_) {
      /* keep the current list on a transient failure */
    }
  }

  async function syncCurrentSession() {
    // Hard fallback: even if an SSE connection looks OPEN but no frames are
    // arriving, pull the durable snapshot. This is what guarantees a new
    // reply appears without a manual refresh.
    if (document.hidden || state.syncBusy || state.loading || state.detailLoading) return;
    if (!state.session?.id || state.sending) return;
    if (state.follow && Date.now() - state.lastFollowAt < 4000) return;
    if (state.live && Date.now() - state.lastFollowAt < 10000) return;
    const sid = state.session.id;
    state.syncBusy = true;
    try {
      const data = await api(`sessions/${encodeURIComponent(sid)}`);
      if (state.session?.id !== sid) return;
      const sig = contentSignature(data);
      if (sig === state.contentSig) return;
      applyDetail(data);
      state.live = null;
      state.contentSig = sig;
      renderAppbar();
      renderContentPreservingOpen();
      updateDockStatus();
      updateDockAction();
    } catch (_) {
      /* transient; the next tick retries */
    } finally {
      state.syncBusy = false;
    }
  }

  async function refreshQueue() {
    if (!INTERACTIVE || !state.session?.id) return;
    const sid = state.session.id;
    const seq = ++state.queueSeq;
    try {
      const data = await api(`queue?session=${encodeURIComponent(sid)}`);
      if (seq !== state.queueSeq || state.session?.id !== sid) return;
      state.queue = data.items || [];
      const mine = state.optimisticUser ? state.queue.find((item) => item.rpcId && item.rpcId === state.optimisticUser.rpcId) : null;
      if (mine && mine.text && mine.text !== state.optimisticUser.text) {
        state.optimisticUser.text = mine.text;
        state.optimisticUser.block.text = mine.text;
        try {
          const node = document.querySelector(`[data-optimistic="${CSS.escape(state.optimisticUser.rpcId)}"] .user-text`);
          if (node) node.textContent = mine.text;
        } catch (_) {}
      }
      updateQueueChip();
      const title = document.querySelector('#sheet-root .sheet-head h2')?.textContent || '';
      if (title.startsWith('插话')) setSheetBody(queueBody());
    } catch (_) {
      /* queue is optional; keep the current list */
    }
  }

  function updateQueueChip() {
    const el = $('#queue-banner');
    if (!el) return;
    if (state.queue.length) {
      const latest = state.queue[state.queue.length - 1];
      el.hidden = false;
      el.innerHTML = `<span class="qb-kicker">插话队列 · ${state.queue.length}</span>
        <span class="qb-text">${esc(latest.text || '（无文本）')}</span>
        <span class="qb-action">查看 / 修改</span>`;
      return;
    }
    if (state.optimisticUser && state.optimisticUser.busy) {
      el.hidden = false;
      el.innerHTML = `<span class="qb-kicker">插话已发送</span>
        <span class="qb-text">${esc(state.optimisticUser.text)}</span>
        <span class="qb-action">同步中…</span>`;
      return;
    }
    el.hidden = true;
    el.innerHTML = '';
  }

  function queueBody() {
    const rows = state.queue.map((item) => `<div class="queue-item">
      <div class="queue-text">${esc(item.text || '（无文本）')}</div>
      <div class="queue-actions">
        <button class="btn ghost" data-act="queue-edit" data-id="${esc(item.id)}">修改</button>
        <button class="btn ghost" data-act="queue-remove" data-id="${esc(item.id)}">删除</button>
        <button class="btn primary" data-act="queue-steer" data-id="${esc(item.id)}">立即插话</button>
      </div>
    </div>`).join('');
    const pending = !rows && state.optimisticUser
      ? `<div class="queue-item"><div class="queue-text">${esc(state.optimisticUser.text)}</div><div class="note">已发送，正在同步 DSH 队列…</div></div>`
      : '';
    return `<div class="queue-list">${rows || pending || '<div class="note">当前没有待处理的插话。</div>'}</div>
      <div class="note">插话会在当前回合的步骤边界进入；点「立即插话」可让 DSH 在当前步骤后优先处理。</div>`;
  }

  async function queueSheet() {
    openSheet({ title: '插话队列', sub: '执行中的待处理消息', body: queueBody() });
    await refreshQueue();
  }

  function editQueue(id) {
    const item = state.queue.find((x) => x.id === id);
    if (!item) return;
    openSheet({
      title: '修改插话',
      sub: '只修改仍在队列里的消息',
      body: `<div class="field"><label>内容</label><textarea id="queue-edit" style="min-height:160px">${esc(item.text || '')}</textarea></div>
        <div class="action-actions"><button class="btn ghost wide" data-act="queue">返回</button><button class="btn primary wide" data-act="queue-save" data-id="${esc(id)}">保存</button></div>`,
    });
  }

  async function queueAction(id, action, text = '') {
    if (!state.session?.id || !id) return;
    try {
      const r = await fetch(`${API}/queue`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({ sessionId: state.session.id, itemId: id, action, text }),
      });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error || `${r.status} ${r.statusText}`);
      closeSheet();
      await refreshQueue();
      toast(action === 'edit' ? '插话已修改' : action === 'remove' ? '插话已删除' : '已立即插话');
    } catch (e) {
      toast(`插话操作失败：${e.message}`);
    }
  }

  async function pingPresence() {
    if (!INTERACTIVE || !state.session?.id || document.hidden) return;
    try {
      await fetch(`${API}/presence`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({ sessionId: state.session.id }),
      });
    } catch (_) {
      /* presence is best-effort; SSE remains the primary path */
    }
  }

  async function refreshOnResume() {
    if (document.visibilityState === 'hidden' || state.resumeInFlight) return;
    if (state.loading || state.detailLoading) return;
    state.resumeInFlight = true;
    try {
      if (!state.session?.id) await loadSessions();
      else {
        await refreshSession({ silent: true, forceFollow: true, preserve: true });
        await refreshSessionsOnly();
      }
    } finally {
      state.resumeInFlight = false;
    }
  }

  function clearAttachments() {
    for (const item of state.attachments) {
      if (item.preview) URL.revokeObjectURL(item.preview);
    }
    state.attachments = [];
    updateAttachStrip();
  }

  function removeAttachment(id) {
    const item = state.attachments.find((x) => x.id === id);
    if (item?.preview) URL.revokeObjectURL(item.preview);
    state.attachments = state.attachments.filter((x) => x.id !== id);
    updateAttachStrip();
  }

  function updateAttachStrip() {
    const el = $('#attach-strip');
    if (!el) return;
    el.innerHTML = state.attachments.map((item) => `<div class="attach-chip">
      ${item.kind === 'image' && item.preview ? `<img src="${esc(item.preview)}" alt="">` : `<span class="attach-file-ico">${icon('file')}</span>`}
      <span class="attach-name">${esc(item.name)}</span>
      <button class="attach-remove" data-act="remove-attachment" data-id="${esc(item.id)}" aria-label="移除">${icon('x')}</button>
    </div>`).join('');
    el.hidden = state.attachments.length === 0;
  }

  function readFileBase64(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const result = String(reader.result || '');
        resolve(result.slice(result.indexOf(',') + 1));
      };
      reader.onerror = () => reject(reader.error || new Error('read failed'));
      reader.readAsDataURL(file);
    });
  }

  async function handleFiles(fileList, forcedKind) {
    const files = [...(fileList || [])].slice(0, 8);
    const maxOne = 12 * 1024 * 1024;
    let total = state.attachments.reduce((n, a) => n + (a.size || 0), 0);
    for (const file of files) {
      if (file.size > maxOne || total + file.size > 20 * 1024 * 1024) {
        toast(`附件太大：${file.name}`);
        continue;
      }
      const declared = String(file.type || '').toLowerCase().replace('image/jpg', 'image/jpeg');
      const ext = String(file.name || '').toLowerCase().split('.').pop();
      const inferred = declared || ({ png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', webp: 'image/webp', gif: 'image/gif' }[ext] || '');
      const kind = forcedKind === 'image' || declared.startsWith('image/') ? 'image' : 'file';
      if (kind === 'image' && !['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(inferred)) {
        toast(`不支持的图片格式：${file.name}`);
        continue;
      }
      try {
        const data = await readFileBase64(file);
        state.attachments.push({
          id: typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : String(Date.now() + Math.random()),
          kind,
          name: file.name || (kind === 'image' ? 'image.png' : 'file'),
          mediaType: kind === 'image' ? inferred : (declared || 'application/octet-stream'),
          data,
          preview: kind === 'image' ? URL.createObjectURL(file) : '',
          size: file.size,
        });
        total += file.size;
      } catch (error) {
        toast(`读取附件失败：${error.message}`);
      }
    }
    closeSheet();
    updateAttachStrip();
    if (state.attachments.length) toast(`已添加 ${state.attachments.length} 个附件`);
  }

  function attachmentSheet() {
    if (!state.session) { toast('先选择一个会话'); return; }
    openSheet({
      title: '添加附件',
      sub: '图片作为图片消息；文件上传到 DSH',
      body: `<div class="attach-options">
        <button class="attach-option" data-act="pick-image">${icon('image')}<div><b>图片</b><span>PNG / JPG / WebP / GIF</span></div></button>
        <button class="attach-option" data-act="pick-file">${icon('file')}<div><b>文件</b><span>上传到 DSH 文件附件</span></div></button>
      </div>
      <input id="pick-image-input" type="file" accept="image/png,image/jpeg,image/webp,image/gif" multiple hidden>
      <input id="pick-file-input" type="file" multiple hidden>
      <div class="note">单个附件最大 12MB，最多 8 个。图片需要模型支持图片输入。</div>`,
    });
  }

  async function sendMessage() {
    if (!INTERACTIVE || !state.session || state.sending) return;
    const text = (state.draft || '').trim();
    if (!text && state.attachments.length === 0) return;
    const pendingAttachments = state.attachments.map((a) => ({ kind: a.kind, name: a.name, mediaType: a.mediaType, data: a.data }));
    const busy = Boolean(state.session.state === 'running' || state.session.state === 'waiting');
    state.sending = true;
    updateDockStatus();
    try {
      const requestId = typeof crypto !== 'undefined' && crypto.randomUUID
        ? `mvc-${crypto.randomUUID()}`
        : `mvc-${Date.now()}-${Math.random().toString(16).slice(2)}`;
      if (text) {
        const block = { type: 'user', text, images: [], files: [], _optimistic: true, _rpcId: requestId, _busy: busy };
        state.blocks.push(block);
        state.optimisticUser = { rpcId: requestId, text, block, busy };
        appendBlocksToTimeline([block]);
        updateQueueChip();
        requestAnimationFrame(scrollContentBottom);
      }
      const r = await fetch(`${API}/prompt`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({
          sessionId: state.session.id,
          text,
          attachments: pendingAttachments,
          requestId,
          mode: 'queue',
          clientTimeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        }),
      });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error || `${r.status} ${r.statusText}`);
      state.draft = '';
      clearAttachments();
      const composerEl = $('#composer');
      if (composerEl) {
        composerEl.value = '';
        composerEl.style.height = 'auto';
      }
      state.session.state = 'running';
      state.session.stateLabel = '运行中';
      state.sending = false;
      if (state.follow) {
        updateDockStatus();
        updateDockAction();
        requestAnimationFrame(scrollContentBottom);
        startFollow();
      } else {
        const fresh = await api(`sessions/${encodeURIComponent(state.session.id)}`);
        applyDetail(fresh);
        state.contentSig = contentSignature(fresh);
        render();
        requestAnimationFrame(scrollContentBottom);
        startFollow();
      }
      $('#composer')?.focus();
      await refreshQueue();
      if (busy) queueSheet();
      toast(busy ? '已加入插话队列，可在这里修改或立即插话' : '已发送到 DSH');
    } catch (e) {
      state.sending = false;
      removeOptimisticUser();
      updateDockStatus();
      toast(`发送失败：${e.message}`);
    }
  }

  async function cancelRun() {
    if (!INTERACTIVE || !state.session) return;
    try {
      const r = await fetch(`${API}/cancel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({ sessionId: state.session.id }),
      });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error || `${r.status} ${r.statusText}`);
      toast('已请求停止');
      startPolling();
    } catch (e) {
      toast(`停止失败：${e.message}`);
    }
  }

  /* ---------- appbar ---------- */
  function renderAppbar() {
    const s = state.session;
    $('#appbar').innerHTML = `
      <button class="abtn" data-act="files" aria-label="文件">${icon('folder')}</button>
      <div class="abtitle" data-act="sessions">${s ? esc(s.title) : 'MobileVC'}</div>
      <span class="conn-dot ${s ? 'on' : ''}" title="${s ? 'DSH 已连接' : '未连接'}"></span>
      <button class="abtn" data-act="refresh" aria-label="刷新">${icon('refresh')}</button>
      <button class="abtn" data-act="status" aria-label="状态">${icon('dashboard')}</button>
      <button class="abtn" data-act="theme" aria-label="切换主题">${icon(state.theme === 'light' ? 'moon' : 'sun')}</button>
      <button class="abtn" data-act="settings" aria-label="设置">${icon('gear')}</button>
    `;
  }

  /* ---------- content ---------- */
  function toolImagesHtml(images) {
    if (!Array.isArray(images) || !images.length) return '';
    return `<div class="tool-images">${images.map((img) => {
      if (!img?.attachmentId) return '';
      const src = `${API}/attachment?id=${encodeURIComponent(img.attachmentId)}`;
      const label = [img.name, img.width && img.height ? `${img.width}×${img.height}` : ''].filter(Boolean).join(' · ');
      return `<button type="button" class="tool-image" data-act="view-image" data-src="${esc(src)}" data-label="${esc(label || '图片')}"><img src="${esc(src)}" alt="${esc(img.name || 'image')}" loading="lazy"><span>${esc(label)}</span></button>`;
    }).join('')}</div>`;
  }

  function userFilesHtml(files) {
    if (!Array.isArray(files) || !files.length) return '';
    return `<div class="user-files">${files.map((file) => {
      if (!file?.attachmentId) return '';
      const src = `${API}/file-attachment?id=${encodeURIComponent(file.attachmentId)}&name=${encodeURIComponent(file.name || 'file')}`;
      const size = file.bytes ? `${(file.bytes / 1024).toFixed(0)} KB` : '';
      return `<a class="user-file" href="${esc(src)}" download="${esc(file.name || 'file')}"><span class="user-file-ico">${icon('file')}</span><span class="user-file-main"><b>${esc(file.name || 'file')}</b><span>${esc(size)}</span></span></a>`;
    }).join('')}</div>`;
  }

  function userImagesHtml(images) {
    if (!Array.isArray(images) || !images.length) return '';
    return `<div class="user-images">${images.map((img) => {
      if (!img?.attachmentId) return '';
      const src = `${API}/attachment?id=${encodeURIComponent(img.attachmentId)}`;
      const label = [img.name, img.width && img.height ? `${img.width}×${img.height}` : ''].filter(Boolean).join(' · ');
      return `<button type="button" class="user-image" data-act="view-image" data-src="${esc(src)}" data-label="${esc(label || '图片')}"><img src="${esc(src)}" alt="${esc(img.name || 'image')}" loading="lazy"></button>`;
    }).join('')}</div>`;
  }

  function chatBlock(b) {
    switch (b.type) {
      case 'user':
        return `<div class="row user"${b._optimistic ? ` data-optimistic="${esc(b._rpcId || '')}"` : ''}><div class="bubble-user">${userImagesHtml(b.images)}${userFilesHtml(b.files)}${b.text ? `<div class="user-text">${esc(b.text)}</div>` : ''}${b._optimistic ? `<div class="user-state">${b._busy ? '插话 · 待处理' : '发送中'}</div>` : ''}</div></div>`;
      case 'assistant':
        return `<div class="row"><div class="card-md">${b.html || esc(b.text || '')}</div></div>`;
      case 'thinking': {
        const chars = b.chars || (b.text || '').length;
        return `<div class="row"><details class="thinking"><summary><span class="think-ico">${icon('brain')}</span><span class="think-label">深度思考</span><span class="think-chars">${chars} 字</span><span class="think-caret">${icon('chevron')}</span></summary><div class="think-body">${b.html || esc(b.text || '')}</div></details></div>`;
      }
      case 'tool': {
        const running = b.status === '运行中';
        const statusClass = running ? 'running' : b.status === '失败' ? 'error' : '';
        return `<div class="row"><details class="tool-card" data-call-id="${esc(b._callId || '')}">
          <summary class="tool-head"><span class="tool-ico">${icon(b.name === 'read' ? 'file' : 'terminal')}</span><span class="tool-name">${esc(b.name)}</span><span class="tool-arg">${esc(b.arg || '')}</span><span class="tool-status ${statusClass}">${esc(b.status || '')}</span><span class="tool-caret">${icon('chevron')}</span></summary>
          <div class="tool-content">${toolImagesHtml(b.images)}<pre class="tool-body">${esc(b.body || '')}</pre></div>
        </details></div>`;
      }
      case 'error': {
        const quota = b.code === 'QUOTA' || b.status === 402 || /额度|余额|insufficient|balance|quota/i.test(`${b.title || ''} ${b.message || ''}`);
        return `<div class="row"><div class="error-card${quota ? ' quota' : ''}">
          <div class="error-head"><span class="error-ico">!</span><div class="error-main"><div class="error-title">${esc(b.title || '模型调用失败')}</div><div class="error-meta">${esc(b.code || 'ERROR')}${b.status ? ` · HTTP ${esc(b.status)}` : ''}</div></div></div>
          ${b.message ? `<div class="error-body">${esc(b.message)}</div>` : ''}
          <div class="error-actions"><button class="btn primary wide" data-act="open-dsh">${quota ? '去 DSH 充值 / 切换模型' : '在 DSH 中处理'}</button></div>
        </div></div>`;
      }
      case 'compaction':
        return `<div class="row"><div class="compaction">${icon('cut')} ${esc(b.text)}</div></div>`;
      case 'system':
        return `<div class="row"><div class="card-compact"><span class="ico-badge ${b.tone || ''}">${icon(b.icon || 'check')}</span><div class="c-main"><div class="c-title">${esc(b.title || '')}</div><div class="c-body">${b.html || esc(b.body || '')}</div></div></div></div>`;
      default:
        return '';
    }
  }

  function approvalCard(a) {
    const id = esc(a.id || '');
    const disabled = a.id ? '' : ' disabled';
    return `<div class="row"><div class="action-card">
      <div class="action-head"><span class="action-ico">${icon('shield')}</span><div><div class="action-title">待授权 <span class="chip amber">MobileVC</span></div><div class="action-desc">${esc(a.tool || '工具')} · ${esc(a.reason || '')}</div></div></div>
      <div class="action-actions">
        <button class="btn ghost wide" data-act="approval" data-id="${id}" data-outcome="rejected"${disabled}>拒绝</button>
        <button class="btn primary wide" data-act="approval" data-id="${id}" data-outcome="allowed-once"${disabled}>允许一次</button>
      </div>
      <div class="note">直接在这里审批。如果另一端已经处理，这次点击会自动失效。</div>
    </div></div>`;
  }

  function renderContent() {
    const el = $('#content');
    if (state.loading) {
      el.className = 'content landing-mode';
      el.innerHTML = `<div class="landing">
        <img class="logo" src="assets/logo-transparent.png" alt="MobileVC">
        <div class="wordmark">MobileVC</div>
        <div class="tagline">正在读取 DSH 会话…</div>
        <div class="boot-spinner" aria-label="loading"></div>
      </div>`;
      return;
    }
    if (state.error) {
      el.className = 'content landing-mode';
      el.innerHTML = `<div class="landing"><img class="logo" src="assets/logo-transparent.png" alt="MobileVC"><div class="wordmark">MobileVC</div>
        <div class="error-box">${esc(state.error)}</div>
        <button class="btn primary" data-act="reload">重新连接 DSH</button>
        <div class="tagline" style="margin-top:10px">如果一直失败，请先打开 <a href="/dsh/" target="_blank">DSH</a> 确认登录状态。</div>
      </div>`;
      return;
    }
    if (state.detailLoading) {
      el.className = 'content';
      el.innerHTML = `<div class="timeline"><div class="row"><div class="card-md"><span class="typing"><i></i><i></i><i></i></span></div></div></div>`;
      return;
    }
    if (!state.session) {
      el.className = 'content landing-mode';
      el.innerHTML = `<div class="landing">
        <img class="logo" src="assets/logo-transparent.png" alt="MobileVC">
        <div class="wordmark">MobileVC</div>
        <div class="tagline">连接后即可开始对话、查看文件和远程调试。</div>
      </div>`;
      return;
    }
    el.className = 'content';
    const blocks = state.blocks.map(chatBlock).join('');
    const approvals = state.approvals.map(approvalCard).join('');
    const empty = `<div class="row"><div class="card-compact"><span class="ico-badge">${icon('chat')}</span><div class="c-main"><div class="c-title">${esc(state.session.title)}</div><div class="c-body">这个会话还没有可见消息。在 DSH 里继续对话后，这里会同步显示。</div></div></div></div>`;
    el.innerHTML = `<div class="timeline" id="timeline">${blocks || (state.live ? '' : empty)}<div id="live-slot"></div><div id="approval-slot">${approvals}</div></div>`;
    updateLiveSlot();
  }

  /* ---------- dock ---------- */
  function renderDock() {
    const s = state.session;
    const st = status();
    const chips = [{ ico: 'history', label: '会话', act: 'sessions' }];
    chips.push({ ico: 'terminal', label: '迹', act: 'trace' });
    chips.push({ ico: 'puzzle', label: '插件', act: 'plugins' });
    chips.push({ ico: 'brain', label: 'Memory', act: 'memory' });
    if (s) {
      if (state.approvals.length) chips.push({ ico: 'shield', label: `权限 · ${state.approvals.length}`, act: 'approvals' });
      if (state.model) chips.push({ ico: 'cpu', label: `模型 · ${state.model.split('/').pop()}`, act: 'models' });
    }
    $('#dock').innerHTML = `<div class="dock">
      <div class="dock-status"><span class="dot ${st.dot}"></span><b>${st.label.split(' · ')[0]}</b> · ${st.label.split(' · ')[1] || ''}</div>
      <div class="rail">
        <div class="chips">${chips.map((c) => `<button class="tchip" data-act="${c.act}">${icon(c.ico)}<span>${c.label}</span></button>`).join('')}</div>
        ${s && state.approvals.length ? `<button class="round-btn" data-act="approvals" aria-label="审批">${icon('shield')}<span class="round-count">${state.approvals.length}</span></button>` : ''}
      </div>
      ${s ? '<div class="queue-banner" id="queue-banner" data-act="queue" hidden></div>' : ''}
      ${s ? '<div class="attach-strip" id="attach-strip" hidden></div>' : ''}
      <div class="inputbox">
        <textarea id="composer" rows="1"${s && INTERACTIVE ? '' : ' readonly'} placeholder="${s ? (INTERACTIVE ? '输入消息，发送给 DSH…' : '在 DSH 中继续对话') : '打开一个会话'}">${s && INTERACTIVE ? esc(state.draft) : ''}</textarea>
        ${s ? `<button class="img-btn plus-btn" data-act="attach" aria-label="添加图片或文件">${icon('plus')}</button>` : ''}
        <button class="send-btn${state.sending ? ' busy' : ''}" data-act="${s && INTERACTIVE ? 'send' : 'open-dsh'}" aria-label="${s && INTERACTIVE ? '发送' : '在 DSH 打开'}">${icon('up')}</button>
        ${s ? `<button class="run-btn" data-act="cancel" aria-label="运行中，点击停止" title="运行中 · 点击停止" hidden>${icon('stop')}</button>` : ''}
      </div>
    </div>`;
    requestAnimationFrame(() => {
      measureDock();
      updateAttachStrip();
      updateDockAction();
      updateQueueChip();
    });
  }
  function measureDock() {
    const dock = $('.dock');
    if (!dock) return;
    const h = Math.ceil(dock.getBoundingClientRect().height);
    $('#screen').style.setProperty('--dock-space', `${h + 20}px`);
  }

  function render() {
    renderAppbar();
    renderContent();
    renderDock();
    document.documentElement.dataset.theme = state.theme;
    document.title = state.session ? `${state.session.title} · MobileVC` : 'MobileVC';
  }

  /* ---------- sheets ---------- */
  let sheetTimer = null;
  function openSheet({ title, sub = '', body, tall = false }) {
    const root = $('#sheet-root');
    clearTimeout(sheetTimer);
    root.innerHTML = `<div class="sheet-backdrop" data-act="close-sheet"></div>
      <div class="sheet"${tall ? ' style="max-height:92%"' : ''}>
        <div class="sheet-handle"></div>
        <div class="sheet-head"><h2>${title}${sub ? `<div class="sub">${sub}</div>` : ''}</h2><button class="sheet-close" data-act="close-sheet" aria-label="关闭">${icon('x')}</button></div>
        <div class="sheet-body">${body}</div>
      </div>`;
    root.classList.add('open');
  }
  function setSheetBody(html) {
    const body = $('#sheet-root .sheet-body');
    if (body) body.innerHTML = html;
  }
  function closeSheet() {
    const root = $('#sheet-root');
    root.classList.remove('open');
    sheetTimer = setTimeout(() => { root.innerHTML = ''; }, 260);
  }
  function toast(msg) {
    const root = $('#toast-root');
    const el = document.createElement('div');
    el.className = 'toast';
    el.textContent = msg;
    root.appendChild(el);
    setTimeout(() => { el.style.opacity = '0'; }, 1900);
    setTimeout(() => el.remove(), 2150);
  }

  function sessionsBody() {
    const rows = state.sessions.map((s) => `<div class="lrow tap sess-row ${s.id === state.session?.id ? 'selected' : ''}" data-act="select-session" data-id="${esc(s.id)}">
      <span class="ico-badge ${s.state === 'running' ? 'green' : s.state === 'waiting' ? 'red' : 'gray'}">${icon(s.state === 'running' ? 'play' : 'chat')}</span>
      <div class="l-main"><div class="l-title">${esc(s.title)} ${s.id === state.session?.id ? '<span class="chip blue">当前</span>' : ''}</div>
      <div class="l-sub">${esc(s.cwd || '')} · ${esc(s.last || '')}</div>
      <div class="l-meta">${esc(s.engine)} · ${esc(s.model)} · ${esc(s.time)}</div></div>
      <span class="chip ${s.state === 'running' ? 'green' : s.state === 'waiting' ? 'amber' : ''}">${esc(s.stateLabel)}</span>
    </div>`).join('');
    return `<div class="list">${rows || '<div class="note">没有找到 DSH 会话。</div>'}</div>
      <button class="btn primary wide" data-act="open-dsh">${icon('plus')} 在 DSH 新建会话</button>
      <div class="note">会话来自真实 DSH；可以直接在 MobileVC 发送、停止和审批。</div>`;
  }

  async function sessionsSheet() {
    openSheet({
      title: '会话',
      sub: '真实 DSH 会话 · 最近 12 条',
      body: sessionsBody(),
    });
    try {
      const data = await api('sessions?limit=12');
      state.sessions = data.sessions || [];
      const title = document.querySelector('#sheet-root .sheet-head h2')?.textContent || '';
      if (title.startsWith('会话')) setSheetBody(sessionsBody());
    } catch (_) {
      /* keep the cached list if refresh fails */
    }
  }

  async function skillsSheet() {
    openSheet({ title: 'Skills', sub: '真实目录 · 由 DSH 管理', body: `<div class="note">正在读取 ${esc(state.cwd || 'DSH')} 的 skill 目录…</div>` });
    try {
      const data = await api(`skills?cwd=${encodeURIComponent(state.cwd || '')}`);
      state.skills = data.skills || [];
      const rows = state.skills.map((s) => `<div class="lrow">
        <span class="ico-badge">${icon('puzzle')}</span>
        <div class="l-main"><div class="l-title">${esc(s.name)} <span class="chip">${esc(s.src)}</span></div><div class="l-sub">${esc(s.desc)}</div><div class="l-meta">${esc(s.path)}</div></div>
        <span class="chip">只读</span>
      </div>`).join('');
      setSheetBody(`<div class="list">${rows || '<div class="note">没有找到 SKILL.md。</div>'}</div>
        <div class="note">手机端只读展示；启用/禁用和调用由 DSH 的 skill 工具管理。</div>`);
    } catch (e) {
      setSheetBody(`<div class="note">读取 skills 失败：${esc(e.message)}</div>`);
    }
  }

  async function memorySheet() {
    openSheet({ title: 'Memory', sub: '真实 AGENTS.md / CLAUDE.md', body: `<div class="note">正在读取…</div>` });
    try {
      const data = await api(`memory?cwd=${encodeURIComponent(state.cwd || '')}`);
      state.memories = data.items || [];
      state.memoryDefaultPath = data.defaultPath || '';
      const rows = state.memories.map((m) => `<div class="mem-card">
        <div class="mem-title">${esc(m.title)} <span class="chip green">${esc(m.path)}</span></div>
        <div class="mem-body">${esc(m.body.slice(0, 400))}${m.body.length > 400 ? '…' : ''}</div>
        <div class="mem-foot"><span>${esc(m.updated)}</span><button class="mem-edit" data-act="edit-memory" data-id="${esc(m.id)}">编辑</button></div>
      </div>`).join('');
      setSheetBody(`${rows || '<div class="note">没有找到 AGENTS.md / CLAUDE.md。你可以在项目里创建一个，然后回来刷新。</div>'}
        <button class="btn primary wide" data-act="create-memory">${icon('plus')} 新建 AGENTS.md</button>
        <div class="note">保存会直接写入真实文件；DSH 下一次会话加载。</div>`);
    } catch (e) {
      setSheetBody(`<div class="note">读取 memory 失败：${esc(e.message)}</div>`);
    }
  }

  function memoryEditor(id) {
    const m = state.memories.find((x) => x.id === id) || { title: 'AGENTS.md', body: '', path: state.memoryDefaultPath || '' };
    openSheet({
      title: m.title,
      sub: '写入真实文件',
      body: `<div class="field"><label>内容</label><textarea id="mem-body" style="min-height:220px">${esc(m.body)}</textarea></div>
        <div class="note">路径：${esc(m.path)}</div>
        <div class="action-actions"><button class="btn ghost wide" data-act="memory-list">返回</button><button class="btn primary wide" data-act="save-memory" data-path="${esc(m.path)}">保存</button></div>`,
    });
  }

  async function saveMemory(path) {
    const content = $('#mem-body')?.value || '';
    try {
      const r = await fetch(`${API}/memory`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({ path, content }),
      });
      const data = await r.json();
      if (!r.ok) throw new Error(data.error || r.statusText);
      closeSheet();
      toast('已写入真实文件');
      memorySheet();
    } catch (e) {
      toast(`保存失败：${e.message}`);
    }
  }


  function plainHtml(html) {
    const div = document.createElement('div');
    div.innerHTML = html || '';
    return (div.textContent || '').replace(/\s+/g, ' ').trim();
  }

  function traceSheet() {
    const rows = state.blocks.map((block) => {
      if (block.type === 'user') {
        return `<div class="trace-row"><span class="trace-dot user"></span><div class="trace-main"><div class="trace-title">用户</div><div class="trace-sub">${esc((block.text || '').slice(0, 180))}</div></div></div>`;
      }
      if (block.type === 'thinking') {
        return `<details class="trace-row"><summary><span class="trace-dot think"></span><div class="trace-main"><div class="trace-title">思考 <span class="chip">${block.chars || 0} 字</span></div><div class="trace-sub">点开查看 reasoning</div></div></summary><div class="trace-body">${block.html || esc(block.text || '')}</div></details>`;
      }
      if (block.type === 'tool') {
        return `<div class="trace-row"><span class="trace-dot tool"></span><div class="trace-main"><div class="trace-title">${esc(block.name)} <span class="chip ${block.status === '运行中' ? 'amber' : 'green'}">${esc(block.status || '')}</span></div><div class="trace-sub">${esc(block.arg || '')}</div></div></div>`;
      }
      if (block.type === 'assistant') {
        return `<div class="trace-row"><span class="trace-dot assistant"></span><div class="trace-main"><div class="trace-title">回复</div><div class="trace-sub">${esc(plainHtml(block.html).slice(0, 180))}</div></div></div>`;
      }
      if (block.type === 'error') {
        return `<div class="trace-row"><span class="trace-dot error"></span><div class="trace-main"><div class="trace-title">${esc(block.title || '模型调用失败')} <span class="chip red">${esc(block.code || 'ERROR')}</span></div><div class="trace-sub">${esc((block.message || '').slice(0, 180))}</div></div></div>`;
      }
      if (block.type === 'compaction') {
        return `<div class="trace-row"><span class="trace-dot compaction"></span><div class="trace-main"><div class="trace-title">上下文压缩</div><div class="trace-sub">${esc(block.text || '')}</div></div></div>`;
      }
      return '';
    }).join('');
    const live = state.live && (state.live.reasoning || state.live.text)
      ? `<div class="trace-row"><span class="trace-dot live"></span><div class="trace-main"><div class="trace-title">正在执行</div><div class="trace-sub">${state.live.reasoning ? '思考中…' : ''}${state.live.text ? ' 输出中…' : ''}</div></div></div>`
      : '';
    openSheet({
      title: '执行迹',
      sub: 'DSH session trace · 实时',
      tall: true,
      body: `<div class="trace-list">${rows || '<div class="note">还没有 trace。发一条消息试试。</div>'}${live}</div>
        <div class="action-actions"><button class="btn ghost wide" data-act="logs">原始日志</button><button class="btn primary wide" data-act="refresh">刷新</button></div>`,
    });
  }

  async function pluginsSheet() {
    openSheet({ title: '插件', sub: 'DSH plugin manager · 实时生效', body: '<div class="note">正在读取插件…</div>' });
    try {
      const data = await api('plugins');
      const bundles = (data.bundles || []).map((b) => `<div class="lrow">
        <span class="ico-badge">${icon('puzzle')}</span>
        <div class="l-main"><div class="l-title">${esc(b.label || b.name)} ${b.version ? `<span class="chip">${esc(b.version)}</span>` : ''}</div>
        <div class="l-sub">${esc(b.description || b.name)}</div>
        <div class="l-meta">${b.optional ? '内置可选' : b.installed ? '已安装' : '安装提供'}${b.readOnlyReason ? ' · 只读' : ''}</div></div>
        <button class="switch ${b.enabled ? 'on' : ''}" data-act="toggle-plugin" data-kind="bundle" data-id="${esc(b.name)}" data-enabled="${b.enabled ? '0' : '1'}" ${b.readOnlyReason ? 'disabled' : ''}></button>
      </div>`).join('');
      const plugins = (data.plugins || []).map((p) => {
        const self = p.id === 'mobilevc' || /mobilevc-dsh/.test(p.moduleName || '');
        return `<div class="lrow">
          <span class="ico-badge ${p.enabled ? 'green' : 'gray'}">${icon('cpu')}</span>
          <div class="l-main"><div class="l-title">${esc(p.label || p.moduleName)} ${self ? '<span class="chip blue">当前</span>' : ''}</div>
          <div class="l-sub" style="font-family:var(--mono);font-size:10px">${esc(p.moduleName)}</div>
          <div class="l-meta">${esc(p.fiberPhase || '')}${p.readOnlyReason ? ' · ' + esc(p.readOnlyReason) : ''}</div></div>
          ${self ? '<span class="chip green">运行中</span>' : `<button class="switch ${p.enabled ? 'on' : ''}" data-act="toggle-plugin" data-kind="plugin" data-id="${esc(p.id)}" data-enabled="${p.enabled ? '0' : '1'}" ${p.readOnlyReason ? 'disabled' : ''}></button>`}
        </div>`;
      }).join('');
      setSheetBody(`<div class="list"><div class="sheet-section">插件包</div>${bundles || '<div class="note">没有插件包。</div>'}<div class="sheet-section">运行中的插件</div>${plugins}</div>
        <div class="note">开关直接走 DSH plugin manager；部分变更需要重启。当前页面自身的插件被锁定，避免把自己关掉。</div>`);
    } catch (e) {
      setSheetBody(`<div class="note">读取插件失败：${esc(e.message)}</div>`);
    }
  }

  async function togglePlugin(kind, id, enabled) {
    try {
      const r = await fetch(`${API}/plugins/toggle`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-MobileVC': '1' },
        body: JSON.stringify({ kind, id, enabled }),
      });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error || `${r.status} ${r.statusText}`);
      toast(data.application === 'restart-required' ? '已保存，需要重启 DSH' : enabled ? '已启用' : '已停用');
      pluginsSheet();
    } catch (e) {
      toast(`插件操作失败：${e.message}`);
    }
  }

  async function logsSheet() {
    openSheet({ title: '运行日志', sub: '真实 DSH 日志文件', tall: true, body: `<div class="note">正在读取…</div>` });
    try {
      const data = await api('logs');
      state.logs = data;
      const lines = (data.lines || []).map((l) => `<div class="logline ${esc(l.level)}"><span class="lt">${esc(l.t)}</span><span class="lm">${esc(l.m)}</span></div>`).join('');
      setSheetBody(`<div class="logbox"><div class="log-head"><span class="live"></span> ${esc(data.file || 'DSH log')} <span class="spacer"></span> ${(data.lines || []).length} 行</div><div class="log-list">${lines || '<div class="note">暂无日志。</div>'}</div></div>
        <button class="btn wide" data-act="logs">刷新</button>
        <div class="note">日志在电脑上产生，手机只读；不会上传。</div>`);
    } catch (e) {
      setSheetBody(`<div class="note">读取日志失败：${esc(e.message)}</div>`);
    }
  }

  function approvalsSheet() {
    const cards = state.approvals.map((a) => `<div class="action-card">
      <div class="action-head"><span class="action-ico">${icon('shield')}</span><div><div class="action-title">${esc(a.tool || '工具')} <span class="chip amber">待授权</span></div><div class="action-desc">${esc(a.reason || '')}</div></div></div>
      <div class="action-actions"><button class="btn primary wide" data-act="open-dsh">在 DSH 中处理</button></div>
    </div>`).join('');
    openSheet({
      title: '待处理授权',
      sub: '来自真实 DSH 会话',
      body: `${cards || '<div class="note">当前没有待处理授权。</div>'}<div class="note">手机端只读；批准/拒绝会改变 Agent 行为，必须在 DSH 内完成。</div>`,
    });
  }

  function modelsSheet() {
    openSheet({
      title: '模型',
      sub: '当前会话正在使用的真实配置',
      body: `<div class="list"><div class="lrow"><span class="ico-badge">${icon('cpu')}</span><div class="l-main"><div class="l-title">${esc(state.model || '未记录')}</div><div class="l-sub">provider: ${esc(state.engine)}</div></div><span class="chip green">当前</span></div></div>
        <div class="note">模型切换在 DSH 的模型选择器里完成；手机端只展示会话记录中的配置。</div>`,
    });
  }

  function statusSheet() {
    const s = state.session;
    openSheet({
      title: '状态',
      sub: '真实 DSH 会话',
      body: `<div class="list">
        <div class="lrow"><span class="ico-badge green">${icon('server')}</span><div class="l-main"><div class="l-title">DSH 已连接</div><div class="l-sub">通过 ${esc(location.host)} 的登录门读取会话文件</div></div></div>
        <div class="lrow"><span class="ico-badge">${icon('chat')}</span><div class="l-main"><div class="l-title">${esc(s?.title || '未选择会话')}</div><div class="l-sub">${esc(s?.cwd || '—')}</div></div></div>
        <div class="lrow"><span class="ico-badge">${icon('cpu')}</span><div class="l-main"><div class="l-title">${esc(state.engine)} · ${esc(state.model || '—')}</div><div class="l-sub">${esc(s?.stateLabel || '—')} · ${esc(s?.time || '')}</div></div></div>
        <div class="lrow"><span class="ico-badge gray">${icon('file')}</span><div class="l-main"><div class="l-title">Session ID</div><div class="l-sub" style="font-family:var(--mono)">${esc(s?.id || '—')}</div></div></div>
      </div>
      <button class="btn wide" data-act="refresh">刷新数据</button>
      <button class="btn primary wide" data-act="open-dsh">在 DSH 中打开</button>`,
    });
  }

  function settingsSheet() {
    openSheet({
      title: '设置',
      sub: '外观与连接',
      body: `<div class="list">
        <div class="lrow"><span class="ico-badge">${icon('chat')}</span><div class="l-main"><div class="l-title">DSH 连接</div><div class="l-sub">${esc(location.host)} · 复用登录门</div></div><span class="chip green">已连接</span></div>
        <div class="lrow"><span class="ico-badge">${icon('plus')}</span><div class="l-main"><div class="l-title">iOS 全屏</div><div class="l-sub">Safari 分享 → 添加到主屏幕；从主屏幕启动才没有地址栏</div></div></div>
        <div class="lrow"><span class="ico-badge">${icon(state.theme === 'light' ? 'moon' : 'sun')}</span><div class="l-main"><div class="l-title">深色模式</div><div class="l-sub">只影响 MobileVC 界面</div></div><button class="switch ${state.theme === 'dark' ? 'on' : ''}" data-act="theme"></button></div>
        <div class="lrow"><span class="ico-badge gray">${icon('lock')}</span><div class="l-main"><div class="l-title">${INTERACTIVE ? '已接入 DSH 运行时' : '只读预览'}</div><div class="l-sub">${INTERACTIVE ? '发送走 sessionController；审批仍在 DSH' : '发送、审批、模型切换在 DSH 内完成'}</div></div></div>
      </div>
      <button class="btn primary wide" data-act="open-dsh">打开 DSH</button>
      <div class="note">数据直接来自本机 <code>~/.dsh/sessions</code>，没有 mock。</div>`,
    });
  }

  /* ---------- files ---------- */
  function ensureFileDrawer() {
    const root = $('#drawer-root');
    if ($('#file-body', root)) return;
    root.innerHTML = `<div class="drawer-backdrop" data-act="close-drawer"></div><aside class="drawer">
      <div class="drawer-head"><h2>文件</h2><button class="sheet-close" data-act="close-drawer" aria-label="关闭">${icon('x')}</button></div>
      <div class="path-bar" id="file-path-bar"></div>
      <div class="file-body" id="file-body"></div>
    </aside>`;
  }

  function filePathMarkup() {
    const baseName = (state.cwd || '/').split('/').filter(Boolean).pop() || '/';
    let acc = '';
    let crumbs = `<button class="crumb" data-act="file-crumb" data-path="">${esc(baseName)}</button>`;
    for (const part of (state.filePath ? state.filePath.split('/') : [])) {
      acc = acc ? `${acc}/${part}` : part;
      crumbs += `<span class="crumb-sep">/</span><button class="crumb" data-act="file-crumb" data-path="${esc(acc)}">${esc(part)}</button>`;
    }
    return `${state.filePath ? `<button class="path-up" data-act="file-up" aria-label="上一级">${icon('chevron')}</button>` : ''}<div class="crumbs">${crumbs}</div>`;
  }

  function renderFileDrawer() {
    const root = $('#drawer-root');
    if (!root.classList.contains('open')) return;
    ensureFileDrawer();
    const bar = $('#file-path-bar', root);
    const body = $('#file-body', root);
    if (bar) bar.innerHTML = filePathMarkup();
    if (!body) return;
    const rows = state.files.map((f) => `<div class="frow" data-act="${f.type === 'dir' ? 'open-dir' : 'open-file'}" data-name="${esc(f.name)}" data-type="${esc(f.type)}" data-path="${esc(f.path || f.name)}">
      <span class="fico ${f.type === 'dir' ? 'dir' : f.type === 'img' ? 'img' : ''}">${icon(f.type === 'dir' ? 'folder' : f.type === 'img' ? 'image' : 'file')}</span>
      <div class="l-main"><div class="fname">${esc(f.name)}</div><div class="fmeta">${esc(f.meta)}</div></div>
      ${f.type === 'dir' ? `<span class="fchevron">${icon('chevron')}</span>` : ''}
    </div>`).join('');
    body.innerHTML = state.fileLoading
      ? '<div class="note">正在读取…</div>'
      : `<div class="list" style="border:0;background:transparent">${rows || '<div class="note">目录为空。</div>'}</div>`;
  }

  async function loadFiles(path, opts = {}) {
    if (!state.session) return;
    const seq = ++state.fileSeq;
    const initial = Boolean(opts.initial);
    if (initial) {
      state.fileLoading = true;
      renderFileDrawer();
    } else {
      $('#file-body')?.classList.add('loading');
    }
    try {
      const data = await api(`files?session=${encodeURIComponent(state.session.id)}&path=${encodeURIComponent(path || '')}`);
      if (seq !== state.fileSeq) return;
      state.files = data.items || [];
      state.cwd = data.cwd || state.cwd;
      state.filePath = data.path || '';
      state.fileParent = data.parent || '';
    } catch (e) {
      if (seq !== state.fileSeq) return;
      state.files = [];
      toast(`读取文件失败：${e.message}`);
    } finally {
      if (seq !== state.fileSeq) return;
      state.fileLoading = false;
      const body = $('#file-body');
      if (body) body.classList.remove('loading');
      renderFileDrawer();
    }
  }

  async function openDrawer() {
    if (!state.session) {
      toast('先选择一个会话');
      return;
    }
    const root = $('#drawer-root');
    state.files = [];
    state.filePath = '';
    state.fileParent = '';
    root.classList.add('open');
    ensureFileDrawer();
    state.fileLoading = true;
    renderFileDrawer();
    await loadFiles('', { initial: true });
  }

  function closeDrawer() {
    state.fileSeq += 1;
    const root = $('#drawer-root');
    root.classList.remove('open');
    setTimeout(() => { root.innerHTML = ''; }, 260);
  }

  async function openFile(name, type, path = name) {
    if (type === 'dir') { loadFiles(path); return; }
    if (type === 'img') {
      const src = `${API}/file?session=${encodeURIComponent(state.session.id)}&path=${encodeURIComponent(path)}`;
      openSheet({ title: name, sub: '真实工作区图片', body: `<img class="viewer-img-el" src="${src}" alt="${esc(name)}">` });
      return;
    }
    try {
      const data = await api(`file?session=${encodeURIComponent(state.session.id)}&path=${encodeURIComponent(path)}`);
      openSheet({ title: name, sub: '真实工作区文件 · 只读', tall: true, body: `<pre class="file-pre">${esc(data.text || '')}</pre>` });
    } catch (e) {
      toast(`读取文件失败：${e.message}`);
    }
  }

  /* ---------- actions ---------- */
  document.addEventListener('click', (e) => {
    const t = e.target.closest('[data-act]');
    if (!t) return;
    const act = t.dataset.act;
    if (act === 'sessions') { sessionsSheet(); return; }
    if (act === 'select-session') { closeSheet(); selectSession(t.dataset.id); return; }
    if (act === 'plugins') { pluginsSheet(); return; }
    if (act === 'memory') { memorySheet(); return; }
    if (act === 'memory-list') { memorySheet(); return; }
    if (act === 'edit-memory') { memoryEditor(t.dataset.id); return; }
    if (act === 'create-memory') { memoryEditor(''); return; }
    if (act === 'save-memory') { saveMemory(t.dataset.path); return; }
    if (act === 'logs') { logsSheet(); return; }
    if (act === 'trace') { traceSheet(); return; }
    if (act === 'toggle-plugin') { togglePlugin(t.dataset.kind, t.dataset.id, t.dataset.enabled === '1'); return; }
    if (act === 'approvals') { approvalsSheet(); return; }
    if (act === 'models') { modelsSheet(); return; }
    if (act === 'status') { statusSheet(); return; }
    if (act === 'settings') { settingsSheet(); return; }
    if (act === 'queue') { queueSheet(); return; }
    if (act === 'queue-edit') { editQueue(t.dataset.id || ''); return; }
    if (act === 'queue-remove') { queueAction(t.dataset.id || '', 'remove'); return; }
    if (act === 'queue-steer') { queueAction(t.dataset.id || '', 'steer'); return; }
    if (act === 'queue-save') { queueAction(t.dataset.id || '', 'edit', $('#queue-edit')?.value || ''); return; }
    if (act === 'refresh') { refreshSession({ forceFollow: true }); return; }
    if (act === 'send') { sendMessage(); return; }
    if (act === 'cancel') { cancelRun(); return; }
    if (act === 'reload') { loadSessions(); return; }
    if (act === 'files') { openDrawer(); return; }
    if (act === 'close-drawer') { closeDrawer(); return; }
    if (act === 'open-dir') { loadFiles(t.dataset.path || ''); return; }
    if (act === 'file-up') { loadFiles(state.fileParent || ''); return; }
    if (act === 'file-crumb') { loadFiles(t.dataset.path || ''); return; }
    if (act === 'open-file') { closeDrawer(); setTimeout(() => openFile(t.dataset.name, t.dataset.type, t.dataset.path), 120); return; }
    if (act === 'approval') { decideApproval(t.dataset.id, t.dataset.outcome, t); return; }
    if (act === 'view-image') { const src = t.dataset.src; if (src) openSheet({ title: t.dataset.label || '图片', sub: 'read_image · DSH 附件', body: `<img class="viewer-img-el" src="${esc(src)}" alt="">` }); return; }
    if (act === 'attach') { attachmentSheet(); return; }
    if (act === 'pick-image') { $('#pick-image-input')?.click(); return; }
    if (act === 'pick-file') { $('#pick-file-input')?.click(); return; }
    if (act === 'remove-attachment') { removeAttachment(t.dataset.id); return; }
    if (act === 'close-sheet') { closeSheet(); return; }
    if (act === 'theme') {
      state.theme = state.theme === 'light' ? 'dark' : 'light';
      try { localStorage.setItem('mobilevc.theme', state.theme); } catch (_) {}
      const meta = document.querySelector('meta[name="theme-color"]');
      if (meta) meta.setAttribute('content', state.theme === 'dark' ? '#000000' : '#F6F8FC');
      render();
      if (document.querySelector('#sheet-root .sheet-head h2')?.textContent?.startsWith('设置')) settingsSheet();
      return;
    }
    if (act === 'open-dsh') {
      closeSheet();
      window.open('/dsh/', '_blank', 'noopener');
      toast('已打开 DSH；发送和审批在那里完成');
      return;
    }
  });

  document.addEventListener('change', (e) => {
    const input = e.target;
    if (!input || input.type !== 'file') return;
    if (input.id === 'pick-image-input') {
      handleFiles(input.files, 'image');
      input.value = '';
    } else if (input.id === 'pick-file-input') {
      handleFiles(input.files, null);
      input.value = '';
    }
  });
  document.addEventListener('input', (e) => {
    if (e.target && e.target.id === 'composer') {
      state.draft = e.target.value;
      e.target.style.height = 'auto';
      e.target.style.height = `${Math.min(120, e.target.scrollHeight)}px`;
    }
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { closeSheet(); closeDrawer(); }
    if (e.target && e.target.id === 'composer' && e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });
  window.addEventListener('resize', measureDock);

  let viewportRaf = null;
  function syncAppViewport() {
    if (viewportRaf) return;
    viewportRaf = requestAnimationFrame(() => {
      viewportRaf = null;
      const vv = window.visualViewport;
      const innerHeight = window.innerHeight;
      const height = vv ? vv.height : innerHeight;
      const top = vv ? vv.offsetTop : 0;
      const inset = Math.max(0, Math.round(innerHeight - top - height));
      const el = document.activeElement;
      const focusedInput = Boolean(el && (el.id === 'composer' || el.tagName === 'TEXTAREA' || el.tagName === 'INPUT'));
      const touch = window.matchMedia?.('(pointer: coarse)')?.matches;
      document.documentElement.style.setProperty('--kb-inset', `${inset}px`);
      document.body.classList.toggle('keyboard-open', inset > 80 || Boolean(focusedInput && touch));
      if (typeof measureDock === 'function') measureDock();
    });
  }

  function installViewportSync() {
    if (window.visualViewport) {
      window.visualViewport.addEventListener('resize', syncAppViewport);
      window.visualViewport.addEventListener('scroll', syncAppViewport);
    }
    window.addEventListener('resize', syncAppViewport);
    window.addEventListener('orientationchange', () => setTimeout(syncAppViewport, 300));
    document.addEventListener('focusin', (event) => {
      const el = event.target;
      if (el && (el.id === 'composer' || el.tagName === 'TEXTAREA' || el.tagName === 'INPUT')) {
        setTimeout(syncAppViewport, 50);
        setTimeout(syncAppViewport, 260);
        setTimeout(syncAppViewport, 520);
      }
    });
    document.addEventListener('focusout', () => setTimeout(syncAppViewport, 120));
    syncAppViewport();
  }
  installViewportSync();

  function installZoomLock() {
    const blockGesture = (event) => event.preventDefault();
    for (const name of ['gesturestart', 'gesturechange', 'gestureend']) {
      document.addEventListener(name, blockGesture, { passive: false });
    }
    document.addEventListener('touchmove', (event) => {
      if (event.touches && event.touches.length > 1) event.preventDefault();
    }, { passive: false });
    document.addEventListener('dblclick', blockGesture, { passive: false });
  }
  installZoomLock();

  // Best-effort portrait lock. Android/PWA honors this after a gesture;
  // iOS ignores it, so the CSS orientation gate is the visual fallback.
  function lockPortrait() {
    try { screen.orientation?.lock?.('portrait').catch(() => {}); } catch (_) {}
  }
  document.addEventListener('pointerdown', lockPortrait, { once: true });

  if ('serviceWorker' in navigator && location.protocol !== 'file:') {
    navigator.serviceWorker.register('sw.js').catch(() => {});
  }

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) refreshOnResume();
  });
  window.addEventListener('pageshow', (event) => {
    if (event.persisted) refreshOnResume();
  });
  window.addEventListener('online', refreshOnResume);
  setInterval(() => {
    if (!document.hidden && !state.loading && !state.detailLoading) refreshSessionsOnly();
  }, 30000);
  // Poll only when the live stream has gone quiet. During an active stream
  // this is a no-op; when the stream is silently dead it recovers in ~2.5s.
  setInterval(syncCurrentSession, 2500);
  setInterval(pingPresence, 15000);
  setInterval(refreshQueue, 2500);

  loadSessions();
})();
