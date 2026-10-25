/* ============================================================================
 * dsh-startup-screen — 启动动画运行时（浏览器端，零依赖）
 * ----------------------------------------------------------------------------
 * 由 Host 半侧注入到 index.html，在 React 应用挂载之前接管首屏：
 *
 *   第一幕  黑幕 + DeepSeek 鲸鱼标识／"深度求索"／"DEEPSEEK"（字距收紧 + 扫光）
 *   过场    过曝洗白（黑 → 灰 → 暖白底板），对应参考片的硬切洗白
 *   第二幕  暖白"技术档案"底板：左上品牌块左→右擦出、右下 POWERED BY、进度条
 *   序列    需要访问许可 → 身份资料确认 → 接收访问需求 → 读取完毕 → 验证通过
 *           → 欢迎 <身份名称> 访问；每步中文大字 + 英文等宽字幕 + 底部长句日志
 *   交互    第 1、2 步等待用户确认（按钮 / Enter / 空格 / 点击），可跳过
 *   终幕    WELCOME TO / [DEEPSEEK HARNESS] / 鲸鱼标识，青红色散故障后揭幕
 *
 * 配置来自 window.__DSH_STARTUP__（Host 侧注入），也可用
 * window.__DSH_STARTUP_API__.replay() 重播。
 * ========================================================================== */
(function () {
  'use strict'

  if (window.__DSH_STARTUP_LOADED__) return
  window.__DSH_STARTUP_LOADED__ = true

  var ROOT_ID = 'dsh-startup-root'

  /* ── DeepSeek 官方鲸鱼标识（取自 DSH 自带 favicon.svg 的路径数据）──────── */
  var WHALE_PATH =
    'M48.8354 10.0479C48.3232 9.79199 48.1025 10.2798 47.8032 10.5278C47.7007 10.6079 ' +
    '47.6143 10.7119 47.5273 10.8076C46.7793 11.624 45.9048 12.1597 44.7622 12.0957C43.0923 ' +
    '12 41.666 12.5356 40.4058 13.8398C40.1377 12.2319 39.2476 11.272 37.8926 10.6558C37.1836 ' +
    '10.3359 36.4668 10.0156 35.9702 9.31982C35.6235 8.82373 35.5293 8.27197 35.356 ' +
    '7.72754C35.2456 7.3999 35.1353 7.06396 34.7651 7.00781C34.3633 6.94385 34.2056 7.2876 ' +
    '34.0479 7.57568C33.418 8.75195 33.1733 10.0479 33.1973 11.3599C33.2524 14.312 34.4736 ' +
    '16.6641 36.8999 18.3359C37.1758 18.5278 37.2466 18.7197 37.1597 19C36.9946 19.5757 ' +
    '36.7974 20.1357 36.624 20.7119C36.5137 21.0801 36.3486 21.1597 35.9624 21C34.6309 ' +
    '20.4321 33.481 19.5918 32.4644 18.5757C30.7393 16.8721 29.1792 14.9917 27.2334 ' +
    '13.52C26.7764 13.1758 26.3193 12.856 25.8467 12.5518C23.8618 10.584 26.1069 8.96777 ' +
    '26.627 8.77588C27.1704 8.57568 26.8159 7.8877 25.0591 7.896C23.3022 7.90381 21.6953 ' +
    '8.50391 19.647 9.30371C19.3477 9.42383 19.0322 9.51172 18.7095 9.58398C16.8501 9.22363 ' +
    '14.9199 9.14355 12.9033 9.37598C9.10596 9.80762 6.07275 11.6396 3.84326 14.7681C1.16455 ' +
    '18.5278 0.53418 22.7998 1.30664 27.2559C2.11768 31.9521 4.46582 35.8398 8.07373 ' +
    '38.8799C11.8159 42.0322 16.1255 43.5762 21.041 43.2803C24.0269 43.104 27.3516 42.6963 ' +
    '31.1016 39.4561C32.0469 39.936 33.0396 40.1279 34.686 40.272C35.9546 40.3921 37.1758 ' +
    '40.208 38.1211 40.0078C39.6021 39.688 39.4995 38.2881 38.9639 38.0322C34.623 35.9678 ' +
    '35.5762 36.8081 34.71 36.1279C36.9155 33.4639 40.2402 30.6958 41.54 21.728C41.6426 ' +
    '21.0161 41.5557 20.5679 41.54 19.9917C41.5322 19.6396 41.6108 19.5039 42.0049 ' +
    '19.4639C43.0923 19.3359 44.1479 19.0317 45.1167 18.4878C47.9292 16.9199 49.064 14.3438 ' +
    '49.3315 11.2559C49.3711 10.7837 49.3237 10.2959 48.8354 10.0479ZM24.3262 ' +
    '37.8398C20.1196 34.4639 18.0791 33.3521 17.2358 33.3999C16.4482 33.4482 16.5898 ' +
    '34.3682 16.7632 34.9678C16.9443 35.5601 17.1812 35.9683 17.5117 36.4878C17.7402 ' +
    '36.832 17.8979 37.3442 17.2832 37.728C15.9282 38.584 13.5728 37.4399 13.4624 ' +
    '37.3838C10.7207 35.7358 8.42822 33.5601 6.81348 30.584C5.25342 27.7197 4.34766 ' +
    '24.6479 4.19775 21.3677C4.1582 20.5757 4.38672 20.2959 5.15869 20.1519C6.17529 ' +
    '19.96 7.22314 19.9199 8.23926 20.0718C12.5327 20.7119 16.1885 22.6719 19.2529 ' +
    '25.7759C21.002 27.5439 22.3252 29.6558 23.6885 31.7202C25.1377 33.9121 26.6978 36 ' +
    '28.6831 37.7119C29.3843 38.312 29.9434 38.7681 30.479 39.104C28.8643 39.2881 26.1699 ' +
    '39.3281 24.3262 37.8398ZM26.3433 24.6001C26.3433 24.248 26.6191 23.9678 26.9658 ' +
    '23.9678C27.0444 23.9678 27.1152 23.9839 27.1782 24.0078C27.2651 24.04 27.3438 ' +
    '24.0879 27.4067 24.1602C27.5171 24.272 27.5801 24.4321 27.5801 24.6001C27.5801 ' +
    '24.9521 27.3042 25.2319 26.9575 25.2319C26.6108 25.2319 26.3433 24.9521 26.3433 ' +
    '24.6001ZM32.6064 27.8799C32.2046 28.0479 31.8027 28.1919 31.4165 28.208C30.8179 ' +
    '28.2397 30.1641 27.9922 29.8096 27.688C29.2583 27.2158 28.8643 26.9521 28.6987 ' +
    '26.1279C28.6279 25.7759 28.6675 25.2319 28.7305 24.9199C28.8721 24.248 28.7144 ' +
    '23.8159 28.2495 23.4238C27.8716 23.104 27.3911 23.0161 26.8633 23.0161C26.666 ' +
    '23.0161 26.4849 22.9277 26.3511 22.856C26.1304 22.7441 25.9492 22.4639 26.1226 ' +
    '22.1201C26.1777 22.0078 26.4458 21.7358 26.5088 21.688C27.2256 21.272 28.0527 ' +
    '21.4077 28.8169 21.7197C29.5259 22.0161 30.0615 22.5601 30.834 23.3281C31.6216 ' +
    '24.2559 31.7632 24.5117 32.2124 25.208C32.5669 25.752 32.8901 26.312 33.1104 ' +
    '26.9521C33.2446 27.3521 33.0713 27.6802 32.6064 27.8799Z'

  /* ── 默认配置（与 Host 侧 lib/index.js 的 DEFAULTS 保持一致）──────────── */
  var DEFAULTS = {
    enabled: true,
    mode: 'session',            // always | session | daily
    theme: 'light',             // light | dark
    speed: 1,
    requireInteraction: true,   // 第 1、2 步等待用户确认
    allowSkip: true,
    sound: true,                // 界面音效（WebAudio 合成，无音频文件）
    soundVolume: 0.5,
    voice: true,                // 女声播报
    voiceLang: 'en',            // en | zh | both
    voiceName: '',              // '' = 自动挑女声（见 FEMALE_HINTS）
    voiceRate: 0.88,            // 电影旁白档：比朗读慢，字与字之间才有重量
    voicePitch: 0.85,           // 压低音调，深沉的 AI 女声
    voiceVolume: 0.9,
    voiceCue: true,             // 念台词前的一声轻提示
    sfx: 'audio',               // audio=真素材（用户音频里抠的音效）/ synth=现场合成
    voiceSource: 'asset',       // asset=优先用真声素材 / tts=全部 TTS 合成
    voiceLead: 240,             // 提示音之后留一小段空白再开口
    identity: 'JOYCE MOORE',    // ← 设置页可改的"身份名称"
    identityId: '0087',
    accessLevel: '3',
    brand1: 'DEEPSEEK',
    brand2: 'SYNTHESIZE INTELLIGENCE',
    brand3a: 'HARNESS',
    brand3b: 'OS',
    orgZh: '深度求索',
    orgEn: 'DEEPSEEK',
    orgSub: 'DEEPSEEK HARNESS',
    welcomeBox: 'DEEPSEEK HARNESS',
    footer: 'POWERED BY DEEPSEEK',
    accent: '#ff7500',
    glitchA: '#ff3b30',
    glitchB: '#2971b8'
  }

  /* ── 序列脚本：中文主字 + 英文等宽字幕 + 底部日志 ───────────────────────
     `say` 是专门给语音播报用的自然语句（照着参考片里女声念状态行的节奏，
     正好卡在各自文字出现的时间点上）。 */
  /* ── 序列脚本 ──────────────────────────────────────────────────────────
     文字与真声素材一一对应（来自你给的音频）：
       line-1.mp3 = V1 需要访问许可
       line-2.mp3 = V2 身份资料确认
       line-3.mp3 = V4 接收访问需求
       line-4.mp3 = V5 开始读取权限
       line-5.mp3 = V6 读取完毕 · 核验通过
     V3 是用户名字独白，按你的要求不用。
     `say` 是素材拿不到时的 TTS 兜底文本；`audio` 是素材文件名（不含扩展名）。 */
  var STEPS = [
    {
      zh: '需要访问许可',
      en: 'ACCESS PERMISSION REQUIRED',
      say: 'Access permission required.',
      audio: 'line-1',
      log: '等待用户授权…',
      logEn: 'AWAITING OPERATOR CONSENT',
      badge: 'AUTHORIZATION',
      action: 'grant',
      btnZh: '授予访问权限',
      btnEn: 'GRANT ACCESS',
      hold: 900,
      progress: 0.34
    },
    {
      zh: '身份资料确认',
      en: 'IDENTITY VERIFICATION',
      say: 'Identity verification.',
      audio: 'line-2',
      log: '核对身份资料…',
      logEn: 'MATCHING OPERATOR RECORD',
      badge: 'IDENTITY',
      action: 'confirm',
      btnZh: '确认身份',
      btnEn: 'CONFIRM IDENTITY',
      hold: 700,
      progress: 0.5
    },
    {
      zh: '接收访问需求',
      en: 'ACCESS REQUEST RECEIVED',
      say: 'Access request received.',
      audio: 'line-3',
      log: '接收访问需求，建立会话通道…',
      logEn: 'HANDSHAKE ESTABLISHED',
      badge: 'REQUEST',
      action: 'auto',
      hold: 1200,
      progress: 0.66,
      assembleLogo: true
    },
    {
      zh: '开始读取权限',
      en: 'PERMISSION READ STARTED',
      say: 'Beginning permission read.',
      audio: 'line-4',
      log: '开始读取权限，索引工作区与会话…',
      logEn: 'READING PERMISSION SET',
      badge: 'READ',
      action: 'auto',
      hold: 1200,
      progress: 0.8,
      hex: true
    },
    {
      zh: '读取完毕 · 核验通过',
      en: 'READ COMPLETE · VERIFIED',
      say: 'Read complete. Verification passed.',
      audio: 'line-5',
      log: '读取完毕，核验通过，权限已授予',
      logEn: 'PERMISSION AUTHORIZED',
      badge: 'VERIFY',
      action: 'auto',
      hold: 1600,
      progress: 0.93,
      reticle: true
    }
  ]

  function cfg() {
    var raw = (window.__DSH_STARTUP__ && typeof window.__DSH_STARTUP__ === 'object')
      ? window.__DSH_STARTUP__
      : {}
    var out = {}
    for (var k in DEFAULTS) if (Object.prototype.hasOwnProperty.call(DEFAULTS, k)) out[k] = DEFAULTS[k]
    for (var k2 in raw) {
      if (Object.prototype.hasOwnProperty.call(raw, k2) && raw[k2] !== undefined && raw[k2] !== null) {
        out[k2] = raw[k2]
      }
    }
    out.speed = Math.min(3, Math.max(0.35, Number(out.speed) || 1))
    out.identity = String(out.identity || DEFAULTS.identity).slice(0, 48)
    /* 代号取身份名称里的字母数字；纯中文名字取不到时退回 OPERATOR */
    out.identityId = String(out.identityId || out.identity).toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 12)
    if (!out.identityId) out.identityId = 'OPERATOR'
    return out
  }

  var C = cfg()

  /* 宿主采来的**真实事实**（批次 3）。取不到就整块空对象，界面显示 "—" ——
     不假装有数据，也不会因为某个服务缺失而报错。 */
  var F = (C.facts && typeof C.facts === 'object') ? C.facts : {}
  function factText(v) {
    return (v === null || v === undefined || v === '') ? '—' : String(v)
  }
  /** 短指纹：宿主给的那个；没有就一串破折号，绝不留空 */
  function factHash() { return F.hash ? String(F.hash) : '--------' }

  /** 重播：把运行时脚本重新挂一次（配置/身份名称改动后 preview 用它）。 */
  function injectSelf() {
    /* 重播（设置页「立即预览」/ 改完配置重放）：先把**这一轮**的声源收掉。
       脚本被重新注入后是一份全新的模块实例，BGM 的 audio 元素挂在旧闭包里，
       不在这里叫停就会和新实例的那一轨叠着放。
       bgm / voice 都是 var 提升的同一作用域变量，这里是运行期调用，不会是 undefined；
       仍然包 try 是因为重播路径绝不允许抛。 */
    try { bgm.stop(0) } catch (e) { /* 重播不许抛 */ }
    try { voice.stop() } catch (e) { /* 同上 */ }
    try { stopField() } catch (e) { /* 同上 */ }
    try { if (ui.traceTimer) { clearInterval(ui.traceTimer); ui.traceTimer = null } } catch (e) { /* 同上 */ }
    window.__DSH_STARTUP_LOADED__ = false
    var old = document.getElementById(ROOT_ID)
    if (old && old.parentNode) old.parentNode.removeChild(old)
    window.__DSH_STARTUP_SKIP__ = false
    var s = document.createElement('script')
    /* 正式运行时由 Host 路由提供（加时间戳绕开缓存）；
       离线预览页会把相对路径写进 __DSH_STARTUP_SCRIPT_URL__，file:// 下不加查询串。 */
    if (window.__DSH_STARTUP_SCRIPT_URL__) {
      s.src = window.__DSH_STARTUP_SCRIPT_URL__
    } else {
      s.src = '/mobilevc/startup/splash.js?t=' + Date.now()
    }
    document.head.appendChild(s)
  }

  /* Host 侧的首屏兜底脚本已经判定"这次不播"（未启用 / 本会话已播 / 今日已播）：
     这里不再建 DOM，但仍然暴露 API，方便设置页按需重播。 */
  if (window.__DSH_STARTUP_SKIP__ === true) {
    window.__DSH_STARTUP_API__ = {
      replay: injectSelf,
      skip: function () {},
      finish: function () {},
      config: C
    }
    return
  }

  /* 最后一道保险：无论前面哪一步抛异常，都不许把用户留在黑幕里 */
  setTimeout(function () {
    try {
      var r = document.getElementById(ROOT_ID)
      if (r && r.classList && r.classList.contains('dsu-boot')) {
        if (r.parentNode) r.parentNode.removeChild(r)
        document.documentElement.classList.remove('dsu-lock')
        if (document.body) document.body.style.overflow = ''
        window.__DSH_STARTUP_FAILED__ = true
      }
    } catch (e) { /* 兜底本身不许再抛 */ }
  }, 9000)

  /* ── 小工具 ───────────────────────────────────────────────────────────── */
  function el(tag, cls, text) {
    var n = document.createElement(tag)
    if (cls) n.className = cls
    if (text != null) n.textContent = text
    return n
  }

  /* 每个 svgWhale 实例都要一个独立的渐变 id，否则同页多个 SVG 会互相引用错 */
  var metalSeq = 0

  function svgWhale(cls) {
    var ns = 'http://www.w3.org/2000/svg'
    var svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 50 50')
    svg.setAttribute('aria-hidden', 'true')
    if (cls) svg.setAttribute('class', cls)

    /* 金属感靠「实心底 + 一层白/黑渐变高光」叠出来，**不能**把整个形状调半透明。
       第一版用 stop-opacity 给 currentColor 做深浅，实机截图证明那是"图标褪色"：
       浅色主题下深色形状被调淡就是发灰、无神，完全不像金属。
       所以：本体路径保持 currentColor 实心，再叠一条同形状的渐变层做镜面反光。 */
    var gid = 'dsu-metal-' + (++metalSeq)
    var defs = document.createElementNS(ns, 'defs')
    var lg = document.createElementNS(ns, 'linearGradient')
    lg.setAttribute('id', gid)
    lg.setAttribute('x1', '0.10'); lg.setAttribute('y1', '0')
    lg.setAttribute('x2', '0.62'); lg.setAttribute('y2', '1')
    var stops = [
      [0, '#ffffff', '0.24'],      /* 左上打高光（0.4 在浅色主题下太抢，会盖掉剪影） */
      [0.40, '#ffffff', '0'],
      [0.62, '#000000', '0'],
      [1, '#000000', '0.20']       /* 右下压阴影 */
    ]
    for (var i = 0; i < stops.length; i++) {
      var st = document.createElementNS(ns, 'stop')
      st.setAttribute('offset', String(stops[i][0]))
      st.setAttribute('stop-color', stops[i][1])
      st.setAttribute('stop-opacity', stops[i][2])
      lg.appendChild(st)
    }
    defs.appendChild(lg)
    svg.appendChild(defs)

    var p = document.createElementNS(ns, 'path')
    p.setAttribute('d', WHALE_PATH)
    p.setAttribute('fill-rule', 'nonzero')
    svg.appendChild(p)   /* 实心底：颜色由 CSS 的 fill: currentColor 继承 */

    var shine = document.createElementNS(ns, 'path')
    shine.setAttribute('d', WHALE_PATH)
    shine.setAttribute('fill-rule', 'nonzero')
    shine.style.fill = 'url(#' + gid + ')'
    svg.appendChild(shine)
    return svg
  }

  /** 逐字写入。两种模式：
   *   'fade'   —— 逐字淡入 + 上移（原行为，给底部小字日志用）
   *   'decode' —— 逐字"解码"：先落一个随机字形，再跳几帧锁定成真字。
   *               终端/AI 题材的经典语汇，也是"机械感"最省的来源。
   *  两种模式都靠 CSS 过渡收尾，解码结束后不需要再补动画。
   *
   *  替身字形**必须和目标字等宽**：作者当初刻意用不透明度而非截断，
   *  就是为了居中排版不左右抖动；用半角 hex 去替全角汉字会把这个优点丢掉。
   *  所以中文走全角片假名、拉丁走半角 hex。
   *  @returns 解码槽位数组（'fade' 模式返回空数组） */
  function typeIn(host, text, stagger, mode) {
    host.textContent = ''
    var frag = document.createDocumentFragment()
    var slots = []
    var n = 0
    for (var i = 0; i < text.length; i++) {
      var ch = text.charAt(i)
      if (ch === ' ') {
        frag.appendChild(document.createTextNode('\u00a0'))
        continue
      }
      var decode = mode === 'decode'
      /* 分隔圆点单独着色：整句有一个强调锚，读起来不再是平均的一排字。 */
      var s = el('span', ch === '·' ? 'dsu-ch dsu-sep' : 'dsu-ch', decode ? glyphFor(ch) : ch)
      s.style.transitionDelay = Math.round(n * stagger) + 'ms'
      if (decode) slots.push({ node: s, text: ch, delay: Math.round(n * stagger) })
      frag.appendChild(s)
      n++
    }
    host.appendChild(frag)
    return slots
  }

  /* 全角片假名（1em 宽）+ 半角 hex —— 两组都和目标字同宽 */
  var GLYPH_WIDE = '\u30a2\u30a4\u30a6\u30a8\u30aa\u30ab\u30ad\u30af\u30b1\u30b3' +
    '\u30b5\u30b7\u30b9\u30bb\u30bd\u30bf\u30c1\u30c4\u30c6\u30c8\u30ca\u30cb\u30cc\u30cd\u30ce'
  var GLYPH_NARROW = '0123456789ABCDEF'
  /** 目标字是不是全角（CJK / 全角标点）—— 决定用哪一组替身字形 */
  function glyphFor(ch) {
    var wide = ch.charCodeAt(0) > 0x2e7f
    var pool = wide ? GLYPH_WIDE : GLYPH_NARROW
    return pool.charAt(Math.floor(Math.random() * pool.length))
  }

  /* 解码调度：所有字共用一个 55ms 的 interval，而不是每个字一个 timer
     （一句中文 8 个字 + 一行英文字幕 20 多个字符，逐字开 timer 会很难看）。
     每个字在自己的入场延迟之后再多抖 100–280ms 才锁定。 */
  var decodeTimer = null
  var decodePending = []
  var DECODE_TICK = 55

  function startDecode(slots) {
    stopDecode()
    if (!slots || !slots.length) return
    decodePending = []
    for (var i = 0; i < slots.length; i++) {
      var s = slots[i]
      decodePending.push({
        node: s.node, text: s.text,
        lockAt: s.delay + 100 + Math.random() * 180
      })
    }
    var t0 = Date.now()
    decodeTimer = setInterval(function () {
      var now = Date.now() - t0
      var rest = []
      for (var j = 0; j < decodePending.length; j++) {
        var d = decodePending[j]
        if (now >= d.lockAt) d.node.textContent = d.text
        else { d.node.textContent = glyphFor(d.text); rest.push(d) }
      }
      decodePending = rest
      if (!decodePending.length) stopDecode()
    }, DECODE_TICK)
  }

  /** 停掉解码并把所有还没锁定的字强制锁成真字（跳过 / 揭幕时必须调） */
  function stopDecode() {
    if (decodeTimer) { clearInterval(decodeTimer); decodeTimer = null }
    for (var i = 0; i < decodePending.length; i++) {
      decodePending[i].node.textContent = decodePending[i].text
    }
    decodePending = []
  }

  function setIn(node, on, cls) {
    if (!node) return
    node.classList.toggle(cls || 'dsu-in', !!on)
  }

  /* ── DOM 搭建 ─────────────────────────────────────────────────────────── */
  var root = document.getElementById(ROOT_ID)
  if (!root) {
    root = el('div')
    root.id = ROOT_ID
    root.className = 'dsu-root'
    document.body.appendChild(root)
  }
  root.classList.toggle('dsu-dark', C.theme === 'dark')
  root.style.setProperty('--dsu-speed', String(C.speed))
  root.style.setProperty('--dsu-accent', C.accent)
  root.style.setProperty('--dsu-glitch-a', C.glitchA)
  root.style.setProperty('--dsu-glitch-b', C.glitchB)

  var ui = {}
  var clockTimer = null
  var html =
    '<div class="dsu-atmo"></div>' +
    '<canvas class="dsu-field" aria-hidden="true"></canvas>' +
    '<div class="dsu-beams"></div>' +
    '<div class="dsu-floor"></div>' +
    '<div class="dsu-plate"></div>' +
    '<div class="dsu-vignette"></div>' +
    '<div class="dsu-watermark"></div>' +
    '<div class="dsu-grain"></div>' +
    '<div class="dsu-black">' +
      '<div class="dsu-black-inner"></div>' +
      '<div class="dsu-sweep"></div>' +
    '</div>' +
    '<div class="dsu-wash"></div>' +
    '<div class="dsu-sheen"></div>' +
    '<header class="dsu-brand">' +
      '<div class="dsu-brand-1"></div><div class="dsu-brand-2"></div>' +
      '<div class="dsu-brand-3"><b></b><span></span></div>' +
    '</header>' +
    '<div class="dsu-stepcount">[ 00 / 06 ] INIT</div>' +
    '<div class="dsu-center">' +
      '<div class="dsu-status"><span class="dsu-status-zh"></span><i class="dsu-caret"></i></div>' +
      '<div class="dsu-sub-en"></div>' +
      '<div class="dsu-rule"></div>' +
      '<div class="dsu-logo"></div>' +
      '<div class="dsu-dots" hidden></div>' +
    '</div>' +
    '<div class="dsu-idline"></div>' +
    '<div class="dsu-facts dsu-facts-1"></div>' +
    '<div class="dsu-facts dsu-facts-2"></div>' +
    '<div class="dsu-reticle"></div>' +
    '<div class="dsu-hex"></div>' +
    '<div class="dsu-trace"></div>' +
    '<div class="dsu-welcome">' +
      '<div class="dsu-w1">WELCOME TO</div>' +
      '<div class="dsu-w2"></div>' +
      '<div class="dsu-w3"></div>' +
      '<div class="dsu-w4"></div>' +
    '</div>' +
    '<div class="dsu-action">' +
      '<div class="dsu-card" hidden></div>' +
      '<button class="dsu-btn" type="button">' +
        '<span class="dsu-btn-zh"></span><span class="dsu-btn-en"></span>' +
      '</button>' +
      '<div class="dsu-hint"></div>' +
    '</div>' +
    '<div class="dsu-frame">' +
      '<i class="dsu-corner dsu-c-tl"></i><i class="dsu-corner dsu-c-tr"></i>' +
      '<i class="dsu-corner dsu-c-bl"></i><i class="dsu-corner dsu-c-br"></i>' +
      '<div class="dsu-scanline"></div>' +
    '</div>' +
    '<div class="dsu-slats"></div>' +
    '<div class="dsu-voice"><i></i><i></i><i></i><i></i><i></i></div>' +
    '<div class="dsu-meta"><span class="dsu-meta-1"></span><span class="dsu-meta-2"></span></div>' +
    '<div class="dsu-checks"></div>' +
    '<div class="dsu-skip">SKIP ▸</div>' +
    '<footer class="dsu-foot"><span></span><i></i></footer>' +
    '<div class="dsu-subtitle">' +
      '<span class="dsu-sub-zh"></span><span class="dsu-sub-en2"></span>' +
    '</div>' +
    '<div class="dsu-progress"><i></i></div>' +
    '<div class="dsu-bloom"></div>' +
    '<div class="dsu-streak"></div>' +
    '<div class="dsu-tear"></div>' +
    '<div class="dsu-flash"></div>'

  root.innerHTML = html

  function q(sel) { return root.querySelector(sel) }
  ui.plate = q('.dsu-plate')
  ui.watermark = q('.dsu-watermark')
  ui.black = q('.dsu-black')
  ui.blackInner = q('.dsu-black-inner')
  ui.sweep = q('.dsu-sweep')
  ui.wash = q('.dsu-wash')
  ui.sheen = q('.dsu-sheen')
  ui.flash = q('.dsu-flash')
  ui.bloom = q('.dsu-bloom')
  ui.streak = q('.dsu-streak')
  ui.tear = q('.dsu-tear')
  ui.vignette = q('.dsu-vignette')
  ui.brand = q('.dsu-brand')
  ui.center = q('.dsu-center')
  ui.brand1 = q('.dsu-brand-1')
  ui.brand2 = q('.dsu-brand-2')
  ui.brand3b = q('.dsu-brand-3 b')
  ui.brand3s = q('.dsu-brand-3 span')
  ui.stepcount = q('.dsu-stepcount')
  ui.status = q('.dsu-status')
  ui.statusZh = q('.dsu-status-zh')
  ui.caret = q('.dsu-caret')
  ui.subEn = q('.dsu-sub-en')
  ui.rule = q('.dsu-rule')
  ui.logo = q('.dsu-logo')
  ui.dots = q('.dsu-dots')
  ui.idline = q('.dsu-idline')
  ui.fact1 = q('.dsu-facts-1')
  ui.fact2 = q('.dsu-facts-2')
  ui.reticle = q('.dsu-reticle')
  ui.hex = q('.dsu-hex')
  ui.trace = q('.dsu-trace')
  ui.welcome = q('.dsu-welcome')
  ui.w1 = q('.dsu-w1')
  ui.w2 = q('.dsu-w2')
  ui.w3 = q('.dsu-w3')
  ui.w4 = q('.dsu-w4')
  ui.action = q('.dsu-action')
  ui.card = q('.dsu-card')
  ui.btn = q('.dsu-btn')
  ui.btnZh = q('.dsu-btn-zh')
  ui.btnEn = q('.dsu-btn-en')
  ui.hint = q('.dsu-hint')
  ui.skip = q('.dsu-skip')
  ui.foot = q('.dsu-foot')
  ui.footText = q('.dsu-foot span')
  ui.subZh = q('.dsu-sub-zh')
  ui.subEn2 = q('.dsu-sub-en2')
  ui.progress = q('.dsu-progress')
  ui.bar = q('.dsu-progress i')
  ui.frame = q('.dsu-frame')
  ui.scanline = q('.dsu-scanline')
  ui.slats = q('.dsu-slats')
  ui.voice = q('.dsu-voice')
  ui.meta = q('.dsu-meta')
  ui.meta1 = q('.dsu-meta-1')
  ui.meta2 = q('.dsu-meta-2')
  ui.atmo = q('.dsu-atmo')
  ui.field = q('.dsu-field')
  ui.beams = q('.dsu-beams')
  ui.floor = q('.dsu-floor')
  ui.checks = q('.dsu-checks')

  /* 内容填充 */
  ui.brand1.textContent = C.brand1
  ui.brand2.textContent = C.brand2
  ui.brand3b.textContent = C.brand3a
  ui.brand3s.textContent = C.brand3b
  ui.footText.textContent = C.footer
  ui.w2.textContent = C.welcomeBox
  ui.w4.textContent = '欢迎 ' + C.identity + ' 访问'
  /* ID 那行挂到左上信息栏，和 REV / 阶段格成组（1.2.2 之前在屏幕中央偏右，
     会压在中英状态行上）。编号单独用 <b> 包起来走强调色，其余保持 muted。
     用 el() 建节点而不是 createTextNode —— 预览与测试用的最小 DOM stub 只保证
     createElement 这一条路径。 */
  ui.idline.textContent = ''
  ui.idline.appendChild(el('span', null, '· ID CONFIRMED : '))
  ui.idline.appendChild(el('b', null, C.identityId))

  ;(function blackStage() {
    var inner = ui.blackInner
    var whale = svgWhale('dsu-whale')
    inner.appendChild(whale)
    inner.appendChild(el('div', 'dsu-black-zh', C.orgZh))
    var en = el('div', 'dsu-black-en', C.orgEn)
    inner.appendChild(en)
    inner.appendChild(el('div', 'dsu-black-sub', C.orgSub))
    ui.blackEn = en
  })()

  ;(function logoStage() {
    ui.logo.appendChild(svgWhale())
    ui.logo.appendChild(el('div', 'dsu-logolabel', 'D E E P S E E K'))
  })()

  ;(function welcomeLogo() {
    ui.w3.appendChild(svgWhale())
    ui.w3.appendChild(el('span', 'dsu-w3label', 'D E E P S E E K  ·  H A R N E S S'))
  })()

  ;(function watermark() {
    var boxes = [
      [0.60, 0.02, 0.16, 0.96], [0.78, 0.02, 0.16, 0.96], [0.96, 0.02, 0.16, 0.96],
      [0.05, 0.02, 0.10, 0.5]
    ]
    for (var i = 0; i < boxes.length; i++) {
      var b = boxes[i]
      var depth = i < 1 ? ' dsu-wm-far' : i < 3 ? ' dsu-wm-mid' : ' dsu-wm-near'
      var n = el('div', 'dsu-wm' + depth)
      n.style.left = (b[0] * 100) + '%'
      n.style.top = (b[1] * 100) + '%'
      n.style.width = (b[2] * 100) + '%'
      n.style.height = (b[3] * 100) + '%'
      ui.watermark.appendChild(n)
    }
  })()

  /* 左侧六格阶段柱 + 品牌块下方的小字数据行 + 实时时钟 */
  ;(function slatsAndMeta() {
    for (var i = 0; i < STEPS.length + 1; i++) ui.slats.appendChild(el('i', 'dsu-slat'))
    /* REV 那行现在显示**真实版本与 profile**，不再写死 "REV 0.1.0 · WEB PROFILE" */
    ui.meta1.textContent = 'REV ' + factText(F.version) + ' · ' + factText(F.profile) + ' PROFILE'
    /* 两行真实自检读数：模型 / 沙箱模式 / 插件数 —— 全部来自宿主 ctx，不是装饰 */
    ui.fact1.textContent = ''
    ui.fact1.appendChild(el('span', null, '· MODEL : '))
    ui.fact1.appendChild(el('b', null, factText(F.model)))
    ui.fact2.textContent = ''
    ui.fact2.appendChild(el('span', null, '· SANDBOX : '))
    ui.fact2.appendChild(el('b', null, factText(F.sandbox)))
    ui.fact2.appendChild(el('span', null, ' · BUNDLES : '))
    /* 用 bundle 数（装了哪些包）而不是 plugin entry 数（配置树里的 191 行）：
       后者是内部条目，标成 "PLUGINS : 191" 会让人误以为装了 191 个用户插件。 */
    ui.fact2.appendChild(el('b', null, factText(F.bundleCount)))
    paintClock()
    clockTimer = setInterval(paintClock, 1000)
  })()

  /* 右侧自检清单：五项全部来自 collectFacts() 的真事实。
     取不到的项由 factText() 写成 "—"，不补假值。 */
  /* 底部信号轨迹：一条由 facts.hash 决定相位的低频波形 + 一条循环扫描游标。
     它不是装饰，换机器/换 profile 波形会变，读起来像这台机器自己的信号。 */
  ;(function trace() {
    var ns = 'http://www.w3.org/2000/svg'
    var svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 520 46')
    var base = document.createElementNS(ns, 'line')
    base.setAttribute('x1', '0'); base.setAttribute('y1', '23')
    base.setAttribute('x2', '520'); base.setAttribute('y2', '23')
    base.setAttribute('class', 'dsu-trace-base')
    svg.appendChild(base)
    for (var i = 0; i <= 20; i++) {
      var tick = document.createElementNS(ns, 'line')
      var tx = i * 26
      tick.setAttribute('x1', String(tx)); tick.setAttribute('y1', '19')
      tick.setAttribute('x2', String(tx)); tick.setAttribute('y2', '27')
      tick.setAttribute('class', 'dsu-trace-tick')
      svg.appendChild(tick)
    }
    var seed = String(F.hash || 'DSHBOOT')
    var h = 2166136261
    for (var k = 0; k < seed.length; k++) {
      h ^= seed.charCodeAt(k)
      h = Math.imul(h, 16777619) >>> 0
    }
    var phase = (h % 628) / 100
    /* 1.6.0：波形不再是冻结的装饰，按相位缓慢推进。
       90ms 一跳足够顺，且比逐帧重算路径省得多；低频漂移不会抢中央读数。 */
    function tracePathAt(now) {
      var drift = now * 0.00034
      var d = 'M0 23'
      for (var x = 0; x <= 520; x += 8) {
        var env = Math.sin((x / 520) * Math.PI)
        var amp = (Math.sin(x * 0.035 + phase + drift) * 5 +
          Math.sin(x * 0.011 + phase * 1.7 - drift * 0.62) * 3) * env
        d += ' L' + x + ' ' + (23 + amp).toFixed(1)
      }
      return d
    }
    var path = document.createElementNS(ns, 'path')
    path.setAttribute('d', tracePathAt(0))
    path.setAttribute('class', 'dsu-trace-line')
    svg.appendChild(path)
    var cursor = document.createElementNS(ns, 'line')
    cursor.setAttribute('x1', '0'); cursor.setAttribute('y1', '2')
    cursor.setAttribute('x2', '0'); cursor.setAttribute('y2', '44')
    cursor.setAttribute('class', 'dsu-trace-cursor')
    svg.appendChild(cursor)
    ui.trace.appendChild(svg)
    ui.tracePath = path
    ui.traceTimer = setInterval(function () { path.setAttribute('d', tracePathAt(Date.now())) }, 90)
  })()

  ;(function checks() {
    var rows = [
      ['PROFILE', F.profile],
      ['BUNDLES', F.bundleCount],
      ['MODEL', F.model],
      ['SANDBOX', F.sandbox],
      ['SESSIONS', F.sessions]
    ]
    var checksHead = el('div', 'dsu-checks-h', 'SELF-CHECK / ')
    checksHead.appendChild(el('span', 'dsu-live', 'LIVE'))
    ui.checks.appendChild(checksHead)
    for (var i = 0; i < rows.length; i++) {
      var row = el('div', 'dsu-check')
      row.appendChild(el('span', 'dsu-check-k', rows[i][0]))
      row.appendChild(el('span', 'dsu-check-v', factText(rows[i][1])))
      row.appendChild(el('i'))
      ui.checks.appendChild(row)
    }
  })()

  ;(function reticle() {
    var ns = 'http://www.w3.org/2000/svg'
    var svg = document.createElementNS(ns, 'svg')
    svg.setAttribute('viewBox', '0 0 400 400')
    function arc(cx, cy, r, a0, a1, cls, cap) {
      var rad = function (d) { return (d - 90) * Math.PI / 180 }
      var x0 = cx + r * Math.cos(rad(a0)), y0 = cy + r * Math.sin(rad(a0))
      var x1 = cx + r * Math.cos(rad(a1)), y1 = cy + r * Math.sin(rad(a1))
      var large = (a1 - a0) > 180 ? 1 : 0
      var p = document.createElementNS(ns, 'path')
      p.setAttribute('d', 'M' + x0.toFixed(2) + ' ' + y0.toFixed(2) +
        ' A' + r + ' ' + r + ' 0 ' + large + ' 1 ' + x1.toFixed(2) + ' ' + y1.toFixed(2))
      p.setAttribute('class', cls)
      if (cap) p.setAttribute('stroke-linecap', cap)
      return { node: p, x: x1, y: y1, x0: x0, y0: y0 }
    }
    var gA = document.createElementNS(ns, 'g')
    gA.setAttribute('class', 'dsu-ret-rot-a')
    /* 300° 弧，缺口留在正下方 6 点钟方向：底部字幕永远不会被压到，
       弧末端圆点落在 5 点钟位置，和原片一致。 */
    var a = arc(200, 200, 150, 215, 505, 'dsu-ring-a', 'round')
    gA.appendChild(a.node)
    var da = document.createElementNS(ns, 'circle')
    da.setAttribute('class', 'dsu-dot-a')
    da.setAttribute('cx', a.x.toFixed(2)); da.setAttribute('cy', a.y.toFixed(2)); da.setAttribute('r', '7')
    gA.appendChild(da)
    svg.appendChild(gA)

    var gB = document.createElementNS(ns, 'g')
    gB.setAttribute('class', 'dsu-ret-rot-b')
    var b = arc(200, 200, 104, 200, 490, 'dsu-ring-b', 'round')
    gB.appendChild(b.node)
    var db = document.createElementNS(ns, 'circle')
    db.setAttribute('class', 'dsu-dot-b')
    db.setAttribute('cx', b.x0.toFixed(2)); db.setAttribute('cy', b.y0.toFixed(2)); db.setAttribute('r', '4')
    gB.appendChild(db)
    svg.appendChild(gB)

    var gC = document.createElementNS(ns, 'g')
    var bl = arc(200, 200, 62, 118, 242, 'dsu-bracket', 'round')
    var br = arc(200, 200, 62, 298, 62, 'dsu-bracket', 'round')
    gC.appendChild(bl.node); gC.appendChild(br.node)
    svg.appendChild(gC)

    var pts = [[152, 132], [252, 268], [278, 150]]
    for (var i = 0; i < pts.length; i++) {
      var c = document.createElementNS(ns, 'circle')
      c.setAttribute('class', 'dsu-dot-o')
      c.setAttribute('cx', pts[i][0]); c.setAttribute('cy', pts[i][1]); c.setAttribute('r', '2.8')
      svg.appendChild(c)
    }

    /* 刻度环：48 格，落在外环内侧（r 132→141）。
       每格带一个递增的 animation-delay —— 准星出现时像仪表自检一样沿环逐格点亮，
       一圈约 1.5 秒，读完就停在暗态，不会一直闪。 */
    var gT = document.createElementNS(ns, 'g')
    gT.setAttribute('class', 'dsu-ret-ticks')
    var TICKS = 48
    for (var t = 0; t < TICKS; t++) {
      var deg = (t / TICKS) * 360 - 90
      var rad = deg * Math.PI / 180
      var long = t % 4 === 0
      var r0 = 133
      var r1 = long ? 143 : 138
      var line = document.createElementNS(ns, 'line')
      line.setAttribute('class', 'dsu-tick' + (long ? ' dsu-tick-major' : ''))
      line.setAttribute('x1', (200 + r0 * Math.cos(rad)).toFixed(2))
      line.setAttribute('y1', (200 + r0 * Math.sin(rad)).toFixed(2))
      line.setAttribute('x2', (200 + r1 * Math.cos(rad)).toFixed(2))
      line.setAttribute('y2', (200 + r1 * Math.sin(rad)).toFixed(2))
      line.style.animationDelay = Math.round(t * 32) + 'ms'
      gT.appendChild(line)
    }
    svg.appendChild(gT)

    /* 数据环：按 bundleCount 点亮对应格数（取不到就 12 格）。
       这一圈不是装饰，而是把"宿主真实装了哪些 bundle"变成可读刻度。 */
    var gD = document.createElementNS(ns, 'g')
    gD.setAttribute('class', 'dsu-ret-data')
    var DATA_TICKS = 40
    var lit = Math.max(6, Math.min(DATA_TICKS, Number(F.bundleCount) || 12))
    for (var d = 0; d < DATA_TICKS; d++) {
      var degD = (d / DATA_TICKS) * 360 - 90
      var radD = degD * Math.PI / 180
      var rD0 = 112
      var rD1 = d < lit ? 120 : 116
      var lineD = document.createElementNS(ns, 'line')
      lineD.setAttribute('class', 'dsu-tick-data' + (d < lit ? ' dsu-lit' : ''))
      lineD.setAttribute('x1', (200 + rD0 * Math.cos(radD)).toFixed(2))
      lineD.setAttribute('y1', (200 + rD0 * Math.sin(radD)).toFixed(2))
      lineD.setAttribute('x2', (200 + rD1 * Math.cos(radD)).toFixed(2))
      lineD.setAttribute('y2', (200 + rD1 * Math.sin(radD)).toFixed(2))
      lineD.style.animationDelay = Math.round(d * 26) + 'ms'
      gD.appendChild(lineD)
    }
    svg.appendChild(gD)

    /* 彗星弧：8% 周长的一段弧，绕环匀速扫过，给静态刻度一个"读取头" */
    var comet = document.createElementNS(ns, 'circle')
    comet.setAttribute('class', 'dsu-ret-comet')
    comet.setAttribute('cx', '200')
    comet.setAttribute('cy', '200')
    comet.setAttribute('r', '150')
    comet.setAttribute('pathLength', '100')
    comet.setAttribute('stroke-dasharray', '8 92')
    svg.appendChild(comet)

    /* 锁定冲击波：只在外环外扩，不穿过中央文字 */
    var ping = document.createElementNS(ns, 'circle')
    ping.setAttribute('class', 'dsu-ping')
    ping.setAttribute('cx', '200')
    ping.setAttribute('cy', '200')
    ping.setAttribute('r', '150')
    svg.appendChild(ping)

    ui.reticle.appendChild(svg)

    /* 雷达扫描扇区：conic 渐变 + 环形遮罩，只在中环那一圈扫。
       用 DOM 叠加层而不是 SVG 渐变 —— 角度渐变在 SVG 里要写 <foreignObject> 或大量小扇面，
       性价比太低。 */
    ui.reticleScan = el('div', 'dsu-ret-scan')
    ui.reticle.appendChild(ui.reticleScan)
  })()

  startField()

  /* ── 时间线引擎 ───────────────────────────────────────────────────────── */
  var timers = []
  var waiters = []
  var skipped = false
  var finished = false
  var hexTimer = null

  function ms(base) { return Math.round(base / C.speed) }

  function wait(base) {
    return new Promise(function (resolve) {
      if (skipped) return resolve()
      var done = false
      function fin() {
        if (done) return
        done = true
        var i = waiters.indexOf(fin)
        if (i >= 0) waiters.splice(i, 1)
        resolve()
      }
      var t = setTimeout(fin, Math.max(0, ms(base)))
      timers.push(t)
      waiters.push(fin)
    })
  }

  function waitAction() {
    return new Promise(function (resolve) {
      if (skipped || !C.requireInteraction) return resolve()
      var done = false
      function fin() {
        if (done) return
        done = true
        cleanup()
        resolve()
      }
      function onKey(e) {
        if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') { e.preventDefault(); fin() }
      }
      function onClick(e) { e.preventDefault(); fin() }
      function cleanup() {
        ui.btn.removeEventListener('click', onClick)
        ui.action.removeEventListener('click', onClick)
        document.removeEventListener('keydown', onKey)
        var i = waiters.indexOf(fin)
        if (i >= 0) waiters.splice(i, 1)
      }
      ui.btn.addEventListener('click', onClick)
      ui.action.addEventListener('click', onClick)
      document.addEventListener('keydown', onKey)
      waiters.push(fin)
    })
  }

  function releaseAll() {
    for (var i = 0; i < timers.length; i++) clearTimeout(timers[i])
    timers.length = 0
    stopDecode()   /* 跳过 / 揭幕时把还没锁定的替身字形强制锁成真字 */
    var list = waiters.slice()
    waiters.length = 0
    for (var j = 0; j < list.length; j++) { try { list[j]() } catch (err) { /* noop */ } }
  }

  function setProgress(p) {
    var pct = Math.max(0, Math.min(1, p))
    ui.bar.style.width = Math.round(pct * 100) + '%'
  }

  /** 六格阶段柱：走过的压暗、当前的拉满并点亮主题橙 */
  function setSlats(active) {
    var items = ui.slats.children
    for (var i = 0; i < items.length; i++) {
      var idx = i + 1
      items[i].classList.toggle('dsu-on', idx === active)
      items[i].classList.toggle('dsu-past', idx < active)
    }
  }

  /** 右侧自检清单：第 n 步点亮第 n 项，前面的转成冷色已读态 */
  function setChecks(active) {
    if (!ui.checks) return
    var items = ui.checks.querySelectorAll('.dsu-check')
    for (var i = 0; i < items.length; i++) {
      var idx = i + 1
      items[i].classList.toggle('dsu-on', idx === active)
      items[i].classList.toggle('dsu-past', idx < active)
    }
  }

  /** 扫描线自上而下扫一遍（底板洗白后一次，验准时再一次） */
  function sweep() {
    if (!ui.scanline) return
    ui.scanline.classList.remove('dsu-go')
    void ui.scanline.offsetWidth
    ui.scanline.classList.add('dsu-go')
  }

  function paintClock() {
    var d = new Date()
    var pad = function (n) { return (n < 10 ? '0' : '') + n }
    ui.meta2.textContent = pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds())
  }

  function setSubtitle(zh, en) {
    ui.subZh.classList.remove('dsu-in')
    ui.subEn2.classList.remove('dsu-in')
    setTimeout(function () {
      ui.subZh.textContent = zh || ''
      ui.subEn2.textContent = en || ''
      ui.subZh.classList.add('dsu-in')
      if (en) ui.subEn2.classList.add('dsu-in')
    }, ms(140))
  }

  function setStepCount(i, code) {
    var totalSteps = STEPS.length + 1
    ui.stepcount.textContent = '[ ' + String(i).padStart(2, '0') + ' / ' +
      String(totalSteps).padStart(2, '0') + ' ] ' + code
  }

  function startHex() {
    var lines = 12
    /* 种子来自真实 facts：同一台机器每次启动的"数据流"稳定，
       换 profile / 模型 / bundle 数就会变 —— 读起来像在读这台机器，而不是随机噪声。 */
    var seed = String(F.hash || 'DSHBOOT') + '|' + String(F.model || '') + '|' + String(F.bundleCount || '')
    var h = 2166136261
    for (var k = 0; k < seed.length; k++) {
      h ^= seed.charCodeAt(k)
      h = Math.imul(h, 16777619) >>> 0
    }
    var tick = 0
    function line() {
      var s = ''
      for (var i = 0; i < 26; i++) {
        h = (Math.imul(h, 1664525) + 1013904223 + (tick++)) >>> 0
        s += '0123456789ABCDEF'.charAt(h >>> 28)
        if (i % 2 === 1) s += ' '
      }
      return s
    }
    function paint() {
      var out = []
      for (var i = 0; i < lines; i++) out.push(line())
      ui.hex.innerHTML = out.join('<br>')
    }
    paint()
    ui.hex.classList.add('dsu-in')
    audio.tickRun()
    hexTimer = setInterval(paint, 110)
  }
  function stopHex() {
    if (hexTimer) { clearInterval(hexTimer); hexTimer = null }
    ui.hex.classList.remove('dsu-in')
  }

  /* ── 活体光场（1.6.0）──────────────────────────────────────────────────
   * 背景不再只有两团缓慢移动的 CSS 光斑，而是一层低分辨率的 Canvas：
   *   · 星尘：三层不同景深，中心区域自动降亮，不压文字；
   *   · 流光：四条横向贝塞尔光带，相位互相错开，像设备内部的数据流；
   *   · 边缘数据雨：只落在左右 18% 的留白里，极低亮度；
   *   · 底部水线：三条低频正弦，和底部信号轨迹接成一个系统。
   * Canvas 以 devicePixelRatio（上限 1.75）渲染，但所有图形都刻意保持低对比，
   * 既让画面持续有生命，又不会变成屏保。减少动态效果时只画一帧静态构图。
   * ========================================================================== */
  var fieldHandle = null

  function stopField() {
    if (!fieldHandle) return
    fieldHandle.stop()
    fieldHandle = null
  }

  function startField() {
    var cv = ui.field
    if (!cv || typeof cv.getContext !== 'function') return
    var ctx = null
    try { ctx = cv.getContext('2d') } catch (e) { return }
    if (!ctx) return

    var reduced = false
    try {
      reduced = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches)
    } catch (e) { /* 老 WebView 没有 matchMedia，按正常动效处理 */ }

    var dpr = Math.min(1.75, Math.max(1, Number(window.devicePixelRatio) || 1))
    var W = 0
    var H = 0
    var raf = 0
    var frames = 0
    var startedAt = Date.now()
    function fieldNow() { return Math.max(0, Date.now() - startedAt) }
    var palette = C.theme === 'dark'
      ? { cool: '111,211,232', warm: '255,177,102', dim: '224,241,246' }
      : { cool: '38,108,122', warm: '164,91,38', dim: '48,64,66' }

    /* 同一台机器、同一份 facts，光场构图一致 —— 不是每次刷新都换一张随机壁纸。 */
    var state = 2166136261
    var seed = String(F.hash || 'DSHBOOT') + '|' + String(F.profile || '') + '|' + String(F.bundleCount || '')
    for (var k = 0; k < seed.length; k++) {
      state ^= seed.charCodeAt(k)
      state = Math.imul(state, 16777619) >>> 0
    }
    function rand() {
      state = (Math.imul(state, 1664525) + 1013904223) >>> 0
      return state / 4294967296
    }

    var motes = []
    var ribbons = []
    var columns = []

    function resize() {
      W = Math.max(1, Number(cv.clientWidth) || Number(root.clientWidth) || Number(window.innerWidth) || 1600)
      H = Math.max(1, Number(cv.clientHeight) || Number(root.clientHeight) || Number(window.innerHeight) || 1020)
      cv.width = Math.round(W * dpr)
      cv.height = Math.round(H * dpr)
      if (cv.style) {
        cv.style.width = '100%'
        cv.style.height = '100%'
      }
      if (typeof ctx.setTransform === 'function') ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      spawn()
    }

    function spawn() {
      motes.length = 0
      ribbons.length = 0
      columns.length = 0
      var count = Math.round(Math.min(150, Math.max(74, (W * H) / 13500)))
      for (var i = 0; i < count; i++) {
        motes.push({
          x: rand() * W,
          y: rand() * H,
          z: 0.24 + rand() * 0.76,
          r: 0.35 + rand() * 1.35,
          ax: 10 + rand() * 54,
          ay: 7 + rand() * 38,
          sp: 0.000025 + rand() * 0.000075,
          ph: rand() * Math.PI * 2,
          tw: 0.00028 + rand() * 0.00066,
          warm: rand() < 0.24
        })
      }
      for (var r = 0; r < 4; r++) {
        ribbons.push({
          y: 0.20 + r * 0.18 + rand() * 0.055,
          amp: 0.028 + rand() * 0.042,
          sp: 0.000022 + rand() * 0.000026,
          ph: rand() * Math.PI * 2,
          wide: 5 + rand() * 8,
          warm: r === 1
        })
      }
      for (var c = 0; c < 12; c++) {
        columns.push({
          side: c % 2,
          x: c < 6 ? (0.025 + rand() * 0.12) : (0.855 + rand() * 0.12),
          y: rand(),
          len: 24 + rand() * 86,
          sp: 0.000006 + rand() * 0.000012
        })
      }
    }

    function drawRibbon(r, now) {
      var drift = now * r.sp
      var x0 = -W * 0.12
      var x1 = W * 1.12
      var y0 = H * (r.y + Math.sin(drift + r.ph) * r.amp)
      var y1 = H * (r.y + Math.cos(drift * 0.82 + r.ph) * r.amp * 1.25)
      var c1 = H * (r.y + Math.sin(drift * 0.64 + r.ph) * r.amp * 2.1)
      var c2 = H * (r.y + Math.cos(drift * 0.57 + r.ph * 1.4) * r.amp * 2.4)
      var rgb = r.warm ? palette.warm : palette.cool
      var grad = ctx.createLinearGradient(0, 0, W, 0)
      grad.addColorStop(0, 'rgba(' + rgb + ',0)')
      grad.addColorStop(0.22, 'rgba(' + rgb + ',' + (C.theme === 'dark' ? 0.055 : 0.045) + ')')
      grad.addColorStop(0.50, 'rgba(' + rgb + ',' + (C.theme === 'dark' ? 0.14 : 0.085) + ')')
      grad.addColorStop(0.78, 'rgba(' + rgb + ',' + (C.theme === 'dark' ? 0.050 : 0.040) + ')')
      grad.addColorStop(1, 'rgba(' + rgb + ',0)')
      ctx.strokeStyle = grad
      ctx.lineCap = 'round'
      ctx.beginPath()
      ctx.moveTo(x0, y0)
      ctx.bezierCurveTo(W * 0.28, c1, W * 0.70, c2, x1, y1)
      ctx.lineWidth = r.wide
      ctx.globalAlpha = 0.16
      ctx.stroke()
      ctx.lineWidth = 0.75
      ctx.globalAlpha = 0.72
      ctx.stroke()

      /* 光带上的读取头：沿同一条贝塞尔曲线移动的小光点。
         这是背景"活着"最直接的证据 —— 静止截图里仍能看到它留下的亮部。 */
      var u = ((now * 0.000034 + r.ph / (Math.PI * 2)) % 1 + 1) % 1
      var v = 1 - u
      var px = v * v * v * x0 + 3 * v * v * u * (W * 0.28) +
        3 * v * u * u * (W * 0.70) + u * u * u * x1
      var py = v * v * v * y0 + 3 * v * v * u * c1 +
        3 * v * u * u * c2 + u * u * u * y1
      var cdx = (px / W - 0.5) * 2
      var cdy = (py / H - 0.45) * 2
      var cometFade = Math.min(1, Math.max(0.16, Math.sqrt(cdx * cdx + cdy * cdy) * 1.4))
      ctx.beginPath()
      ctx.fillStyle = 'rgba(' + rgb + ',' + (0.11 * cometFade).toFixed(3) + ')'
      ctx.arc(px, py, r.wide * 2.4, 0, Math.PI * 2)
      ctx.fill()
      ctx.beginPath()
      ctx.fillStyle = 'rgba(' + rgb + ',' + (0.72 * cometFade).toFixed(3) + ')'
      ctx.arc(px, py, r.warm ? 1.25 : 1.05, 0, Math.PI * 2)
      ctx.fill()
      ctx.globalAlpha = 1
    }

    function draw(now, still) {
      ctx.clearRect(0, 0, W, H)
      ctx.globalCompositeOperation = C.theme === 'dark' ? 'lighter' : 'source-over'

      /* 一条极慢的垂直光带，像扫描仪走过整张底板。 */
      var sx = ((now * 0.012) % (W + 260)) - 130
      var sweep = ctx.createLinearGradient(sx - 150, 0, sx + 150, 0)
      sweep.addColorStop(0, 'rgba(' + palette.cool + ',0)')
      sweep.addColorStop(0.5, 'rgba(' + palette.cool + ',' + (C.theme === 'dark' ? 0.028 : 0.018) + ')')
      sweep.addColorStop(1, 'rgba(' + palette.cool + ',0)')
      ctx.fillStyle = sweep
      ctx.fillRect(sx - 150, 0, 300, H)

      for (var r = 0; r < ribbons.length; r++) drawRibbon(ribbons[r], now)

      /* 底部三条低频水线：把画面下半部从"空"变成"深"。 */
      for (var w = 0; w < 3; w++) {
        ctx.beginPath()
        for (var x = 0; x <= W; x += 26) {
          var wy = H * (0.745 + w * 0.052) +
            Math.sin(x * 0.0062 + now * 0.00022 + w * 1.7) * (3 + w * 1.4) +
            Math.sin(x * 0.013 - now * 0.00014 + w) * 1.6
          if (x === 0) ctx.moveTo(x, wy)
          else ctx.lineTo(x, wy)
        }
        ctx.strokeStyle = 'rgba(' + (w === 1 ? palette.warm : palette.cool) + ',' +
          (C.theme === 'dark' ? 0.050 : 0.032) + ')'
        ctx.lineWidth = 0.8
        ctx.stroke()
      }

      /* 边缘数据雨：只在左右留白里，中心永远干净。 */
      for (var c = 0; c < columns.length; c++) {
        var col = columns[c]
        var cy = ((col.y + now * col.sp) % 1.2) * H - col.len
        ctx.fillStyle = 'rgba(' + palette.dim + ',' + (C.theme === 'dark' ? 0.035 : 0.022) + ')'
        ctx.fillRect(Math.round(col.x * W), cy, 1, col.len)
      }

      /* 星尘：近层大而亮、远层小而暗；中心 30% 半径内降亮到 18%。 */
      for (var m = 0; m < motes.length; m++) {
        var dot = motes[m]
        var mx = dot.x + Math.sin(now * dot.sp + dot.ph) * dot.ax
        var my = dot.y + Math.cos(now * dot.sp * 0.83 + dot.ph) * dot.ay
        if (still) { mx = dot.x; my = dot.y }
        var nx = (mx / W - 0.5) * 2
        var ny = (my / H - 0.45) * 2
        var dist = Math.sqrt(nx * nx * 1.18 + ny * ny)
        var centerFade = Math.min(1, Math.max(0.18, dist * 1.32))
        var twinkle = 0.62 + 0.38 * Math.sin(now * dot.tw + dot.ph)
        var alpha = (0.045 + dot.z * 0.16) * centerFade * twinkle
        var rgbDot = dot.warm ? palette.warm : palette.cool
        ctx.beginPath()
        ctx.fillStyle = 'rgba(' + rgbDot + ',' + alpha.toFixed(3) + ')'
        ctx.arc(mx, my, dot.r * dot.z, 0, Math.PI * 2)
        ctx.fill()
        if (dot.r > 1.15) {
          ctx.beginPath()
          ctx.fillStyle = 'rgba(' + rgbDot + ',' + (alpha * 0.12).toFixed(3) + ')'
          ctx.arc(mx, my, dot.r * 4.2, 0, Math.PI * 2)
          ctx.fill()
        }
      }
      ctx.globalAlpha = 1
      ctx.globalCompositeOperation = 'source-over'
    }

    function frame() {
      if (!fieldHandle) return
      draw(fieldNow(), false)
      frames++
      if (fieldHandle) fieldHandle.frames = frames
      raf = window.requestAnimationFrame(frame)
    }

    function onResize() {
      resize()
      draw(fieldNow(), true)
    }
    function onVisibility() {
      if (document.hidden) {
        if (raf) { window.cancelAnimationFrame(raf); raf = 0 }
      } else if (!reduced && !raf) {
        raf = window.requestAnimationFrame(frame)
      }
    }

    fieldHandle = {
      frames: 0,
      width: function () { return W },
      height: function () { return H },
      draw: function (ms, still) { draw(ms || 0, !!still) },
      stop: function () {
        if (raf) { window.cancelAnimationFrame(raf); raf = 0 }
        window.removeEventListener('resize', onResize)
        document.removeEventListener('visibilitychange', onVisibility)
      }
    }
    window.__DSH_STARTUP_FIELD__ = fieldHandle
    window.addEventListener('resize', onResize)
    document.addEventListener('visibilitychange', onVisibility)
    resize()
    draw(0, true)
    if (!reduced) raf = window.requestAnimationFrame(frame)
  }

  /* ── 声音 ──────────────────────────────────────────────────────────────
   * 两条独立的通道，都不依赖任何音频文件：
   *   audio —— 界面音效，WebAudio 现场合成（咔哒 / 双音确认 / 噪声扫频 / 和声钟）
   *   voice —— 女声播报，Web Speech API 念每一行的英文（照参考片的女声节奏：
   *            7.3-9.3s 念状态行、19.6-21.2s 念 PERMISSION AUTHORIZED）
   * 浏览器策略要求"先有用户手势"，所以第一次点击/按键会补一次解锁 + 补播。 */
  function clamp01(v, lo, hi) {
    var n = Number(v)
    return isFinite(n) ? Math.min(hi, Math.max(lo, n)) : lo
  }

  /* 素材由 Host 路由下发，并带 24h 强缓存（见 lib/index.js 的 Cache-Control）。
     换过素材文件后浏览器仍会拿旧的那份，所以 URL 必须挂版本串破缓存；
     查询串在 Host 侧「取 basename 之前」就被剥掉，不影响白名单校验。
     audio（音效 / 台词素材）与下面的 bgm 共用这一个构造器。 */
  var ASSET_V = String(window.__DSH_STARTUP_VERSION__ || '0')
  function assetUrl(name) { return '/mobilevc/startup/asset/' + name + '.mp3?v=' + ASSET_V }

  var audio = (function () {
    var ctx = null
    function ac(force) {
      /* 语音素材要独立于「界面音效」开关：force=true 时无视 C.sound */
      if (!C.sound && !force) return null
      try {
        var AC = window.AudioContext || window.webkitAudioContext
        if (!AC) return null
        if (!ctx) ctx = new AC()
        if (ctx.state === 'suspended' && typeof ctx.resume === 'function') ctx.resume()
        return ctx
      } catch (e) { return null }
    }
    function vol(gain) { return Math.max(0.0002, (gain == null ? 0.05 : gain) * clamp01(C.soundVolume, 0, 1) * 2) }
    function tone(freq, dur, type, gain, delay, glideTo) {
      var c = ac(); if (!c) return
      try {
        var t0 = c.currentTime + (delay || 0)
        var o = c.createOscillator(), g = c.createGain()
        o.type = type || 'sine'
        o.frequency.setValueAtTime(freq, t0)
        if (glideTo) o.frequency.exponentialRampToValueAtTime(glideTo, t0 + dur)
        g.gain.setValueAtTime(0.0001, t0)
        g.gain.exponentialRampToValueAtTime(vol(gain), t0 + Math.min(0.02, dur * 0.3))
        g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur)
        o.connect(g); g.connect(c.destination)
        o.start(t0); o.stop(t0 + dur + 0.03)
      } catch (e) { /* 静默降级 */ }
    }
    function noise(dur, gain) {
      var c = ac(); if (!c) return
      try {
        var n = Math.floor(c.sampleRate * dur)
        var buf = c.createBuffer(1, n, c.sampleRate)
        var d = buf.getChannelData(0)
        for (var i = 0; i < n; i++) d[i] = (Math.random() * 2 - 1) * (1 - i / n)
        var src = c.createBufferSource(); src.buffer = buf
        var f = c.createBiquadFilter(); f.type = 'bandpass'
        f.frequency.value = 1500; f.Q.value = 0.6
        var g = c.createGain(); g.gain.value = vol(gain)
        src.connect(f); f.connect(g); g.connect(c.destination)
        src.start()
      } catch (e) { /* 静默降级 */ }
    }
    /* 从 /mobilevc/startup/asset/ 拉真素材（用户音频里抠出来的"哒"）并解码缓存；
       拿不到就自动回落到下面合成的音，功能不依赖网络/素材存在与否。 */
    var buffers = {}
    var fetching = {}
    function loadAsset(name) {
      if (C.sfx === 'synth') return
      if (buffers[name] || fetching[name]) return
      fetching[name] = true
      try {
        fetch(assetUrl(name))
          .then(function (r) { return r.ok ? r.arrayBuffer() : null })
          .then(function (ab) { return ab ? ac().decodeAudioData(ab) : null })
          .then(function (buf) { if (buf) buffers[name] = buf })
          .catch(function () { /* 没素材就走合成音 */ })
      } catch (e) { /* fetch 不存在也一样降级 */ }
    }
    /* 素材预加载：fetch + decodeAudioData 不需要用户手势，所以加载时就拉，
       这样第一步就能用真声，而不用等用户第一次点击。 */
    var pending = {}
    function ensureAsset(name) {
      if (buffers[name]) return Promise.resolve(true)
      if (pending[name]) return pending[name]
      if (C.voiceSource === 'tts') return Promise.resolve(false)
      pending[name] = new Promise(function (resolve) {
        try {
          fetch(assetUrl(name))
            .then(function (r) { return r.ok ? r.arrayBuffer() : null })
            .then(function (ab) { if (!ab) return null; return ac(true).decodeAudioData(ab) })
            .then(function (buf) {
              if (buf) { buffers[name] = buf; resolve(true) } else { resolve(false) }
            })
            .catch(function () { resolve(false) })
        } catch (e) { resolve(false) }
      })
      return pending[name]
    }

    /** 播素材并等它播完，resolve 实际时长(ms)。拿不到素材 resolve 0。 */
    function playAssetAndWait(name, gain) {
      return new Promise(function (resolve) {
        var c = ac(true)
        var buf = buffers[name]
        if (!c || !buf) { resolve(0); return }
        try {
          var src = c.createBufferSource()
          src.buffer = buf
          var g = c.createGain()
          g.gain.value = Math.min(1, (gain == null ? 0.85 : gain) * clamp01(C.voiceVolume, 0, 1) * 1.6)
          src.connect(g); g.connect(c.destination)
          src.onended = function () { resolve(Math.round(buf.duration * 1000)) }
          src.start()
        } catch (e) { resolve(0) }
      })
    }

    function playAsset(name, gain) {
      var c = ac(); if (!c) return false
      var buf = buffers[name]
      if (!buf) { loadAsset(name); return false }
      try {
        var src = c.createBufferSource()
        src.buffer = buf
        var g = c.createGain()
        g.gain.value = Math.min(1, (gain == null ? 0.55 : gain) * clamp01(C.soundVolume, 0, 1) * 2)
        src.connect(g); g.connect(c.destination)
        src.start()
        return true
      } catch (e) { return false }
    }

    return {
      resume: function () { ac(); loadAsset('sfx-tick'); loadAsset('sfx-ticks-run') },
      preload: function () { loadAsset('sfx-tick'); loadAsset('sfx-ticks-run') },
      ensureAsset: ensureAsset,
      playAssetAndWait: playAssetAndWait,
      /* 文字渐显 / 阶段推进 / 按钮确认 —— 优先用真素材 */
      tick: function () { if (!playAsset('sfx-tick', 0.45)) tone(1760, 0.035, 'square', 0.022) },
      confirm: function () { if (!playAsset('sfx-tick', 0.85)) { tone(660, 0.09, 'triangle', 0.07); tone(990, 0.17, 'triangle', 0.055, 0.085) } },
      tickRun: function () { playAsset('sfx-ticks-run', 0.5) },
      step: function () { if (!playAsset('sfx-tick', 0.6)) { tone(880, 0.05, 'sine', 0.05); tone(1320, 0.07, 'sine', 0.028, 0.045) } },
      hover: function () { tone(2200, 0.02, 'sine', 0.018) },
      confirm: function () { tone(660, 0.09, 'triangle', 0.07); tone(990, 0.17, 'triangle', 0.055, 0.085) },
      sweep: function () { noise(0.75, 0.035) },
      /* 念台词之前的两声轻提示：这一笔让 TTS 从"浏览器念字"变成"系统播报" */
      cue: function () { tone(1046.5, 0.05, 'sine', 0.028); tone(1568, 0.06, 'sine', 0.02, 0.075) },
      rise: function () { tone(220, 1.1, 'sine', 0.032, 0, 990) },
      chime: function () {
        tone(659.25, 0.9, 'sine', 0.055)
        tone(830.61, 0.9, 'sine', 0.04, 0.09)
        tone(987.77, 1.3, 'sine', 0.045, 0.18)
      }
    }
  })()

  /* ── 背景音乐 ──────────────────────────────────────────────────────────
   * 单独一条轨，走 HTMLAudio 元素而不是 WebAudio 解码，两个理由：
   *   1) 素材 2 分半 / 3.6 MB，decodeAudioData 要占 ~57 MB 内存；元素流式播放
   *      只占几百 KB，而且自带 loop —— 用户在第几步磨蹭多久都不会断。
   *   2) 音量用 30ms 小步手动走斜坡。闪避只要 200ms 级就够，不值得为它引入
   *      MediaElementSource —— 那条路一旦接错就是静音，远程很难查。
   * 起播策略：**立即尝试**。桌面端 Electron 的 webPreferences.autoplayPolicy 默认
   * 就是 no-user-gesture-required（Electron 官方文档原文），DSH 也没覆盖它，
   * 所以黑幕第一帧就能有音乐；web profile 走浏览器默认的
   * document-user-activation-required，会被拦下，那时 start() 自己复位，
   * 退回 unlock() 的第一次用户手势再起 —— 两条路都不需要人工判断宿主。 */
  var bgm = (function () {
    var el = null
    var started = false
    var failed = false
    var stopping = false
    var timer = null
    var level = 0        /* 当前音量 0..1 */
    var want = 0         /* 目标音量 */

    function base() { return clamp01(C.bgmVolume, 0, 1) }
    function on() { return C.bgm !== false && base() > 0 }
    /** 台词一响退到 35%（约 −9 dB），说完再抬回来 */
    function target(ducked) { return ducked && C.bgmDuck !== false ? base() * 0.35 : base() }

    /* 这里的守卫**不能用全局 finished**：finish() 会先把 finished 置真，
       斜坡会被自己的标志当场掐断，揭幕的淡出就变成硬切 —— 1.2.1 实机就是这个
       症状（音乐秒断）。所以用 bgm 自己的 stopping：只有"要往上抬"才拒绝。 */
    function ramp(to, ms) {
      want = Math.max(0, Math.min(1, to))
      if (timer || !el) return
      var step = Math.max(0.01, Math.abs(want - level) / Math.max(1, ms / 30))
      timer = setInterval(function () {
        if (!el || (stopping && want > 0)) { clearInterval(timer); timer = null; return }
        var d = want - level
        if (Math.abs(d) <= step) { level = want; clearInterval(timer); timer = null }
        else level += d > 0 ? step : -step
        try { el.volume = Math.max(0, Math.min(1, level)) } catch (e) {}
      }, 30)
    }

    function prepare() {
      if (el || failed || !on()) return
      try {
        el = document.createElement('audio')
        el.src = assetUrl('bgm')
        el.loop = true
        el.preload = 'auto'
        el.volume = 0
      } catch (e) { failed = true }
    }

    return {
      /** 提前建元素把文件缓冲起来，用户一点就能出声 */
      prepare: prepare,
      start: function () {
        if (started || failed || !on()) return
        if (!el) prepare()
        if (!el) return
        started = true
        try {
          var p = el.play()
          /* 被自动播放策略拦下（web profile 的浏览器默认）：复位，等 unlock() 再起。
             桌面端 Electron 默认宽松，这句通常不会执行。 */
          if (p && typeof p.catch === 'function') {
            p.catch(function () {
              started = false
              if (timer) { clearInterval(timer); timer = null }
              level = 0
              try { el.volume = 0; el.pause() } catch (e) {}
            })
          }
          ramp(target(false), 900)
        } catch (e) { started = false }
      },
      /** 台词通道开合时调用；把闪避关掉就直接抬回原音量 */
      duck: function (ducked) {
        if (!started || failed || !el || finished || stopping) return
        ramp(target(ducked), ducked ? 220 : 600)
      },
      /** 揭幕：淡出后停掉，避免和主界面抢声。重复调用只认第一次的时长。 */
      stop: function (ms) {
        if (!started || !el || stopping) return
        stopping = true
        var node = el
        var wait = ms == null ? 900 : ms
        ramp(0, wait)
        setTimeout(function () {
          if (timer) { clearInterval(timer); timer = null }
          try { node.pause() } catch (e) {}
        }, wait + 160)
      }
    }
  })()

  var voice = (function () {
    var supported = typeof window !== 'undefined' && !!window.speechSynthesis &&
      typeof window.SpeechSynthesisUtterance === 'function'
    var voices = []
    var picked = { en: null, zh: null }
    var meter = null
    var saidOnce = false
    var pending = { en: '', zh: '' }

    /* 想让它听起来像"AI 播报"，音色本身比什么都重要。
       同一台机器上，"Microsoft Zira Desktop" 是上世纪拼接合成，念长句塑料味很重；
       Edge 的 "... Online (Natural)"、Chrome 的 "Google ..."、macOS 的 Siri 才是神经网络音色。
       所以这里按分数挑，而不是挨个名字第一个命中。 */
    var NATURAL_HINTS = ['natural', 'neural', 'online', 'premium', 'enhanced', 'siri', 'google']
    var FEMALE_HINTS = [
      'aria', 'jenny', 'michelle', 'emma', 'ava', 'libby', 'sonia', 'maisie',
      'zira', 'samantha', 'karen', 'moira', 'tessa', 'victoria', 'serena', 'allison',
      'susan', 'fiona', 'hazel', 'female', 'woman',
      'xiaoxiao', 'xiaoyi', 'xiaomo', 'huihui', 'yaoyao', '晓晓', '晓伊', '女声'
    ]
    var MALE_HINTS = ['david', 'mark', 'guy', 'ryan', 'george', 'daniel', 'alex', 'fred',
      'male', 'yunxi', 'yunjian', 'kangkang']

    /** 给一个音色打分：神经网络 > 女声 > 语种吻合 > 本地 */
    function score(v, want) {
      var name = String(v.name || '').toLowerCase()
      var lang = String(v.lang || '').toLowerCase()
      var sc = 0
      for (var i = 0; i < NATURAL_HINTS.length; i++) {
        if (name.indexOf(NATURAL_HINTS[i]) !== -1) { sc += 60; break }
      }
      for (var j = 0; j < FEMALE_HINTS.length; j++) {
        if (name.indexOf(FEMALE_HINTS[j]) !== -1) { sc += 40; break }
      }
      for (var k = 0; k < MALE_HINTS.length; k++) {
        if (name.indexOf(MALE_HINTS[k]) !== -1) { sc -= 45; break }
      }
      if (lang.indexOf(want) === 0) sc += 25
      if (v.localService) sc += 4          /* 本地音色起播快，不会先卡半秒 */
      if (v.default) sc += 3
      return sc
    }

    /** 挑某语种最合适的一个音色（want 传 'en' / 'zh' 这样的两字母前缀） */
    function bestFor(want) {
      if (!voices.length) return null
      if (C.voiceName) {
        for (var i = 0; i < voices.length; i++) {
          var v = voices[i]
          if (v.name === C.voiceName && String(v.lang || '').toLowerCase().indexOf(want) === 0) return v
        }
      }
      var pool = []
      for (var j = 0; j < voices.length; j++) {
        if (String(voices[j].lang || '').toLowerCase().indexOf(want) === 0) pool.push(voices[j])
      }
      if (!pool.length) pool = voices.slice()
      var best = null
      var bs = -1e9
      for (var n = 0; n < pool.length; n++) {
        var sc = score(pool[n], want)
        if (sc > bs) { bs = sc; best = pool[n] }
      }
      return best
    }

    function setMeter(on) { if (meter) meter.classList.toggle('dsu-on', !!on) }

    function load() {
      if (!supported) return
      try { voices = window.speechSynthesis.getVoices() || [] } catch (e) { voices = [] }
      picked.en = bestFor('en')
      picked.zh = bestFor('zh')
    }

    /** 全大写名字有些引擎会一个字母一个字母念，转成 Title Case 更像在叫名字 */
    function spokenName(name) {
      return String(name || '').replace(/\S+/g, function (w) {
        return (/^[A-Z0-9.\-]+$/.test(w) && w.length > 1) ? w.charAt(0) + w.slice(1).toLowerCase() : w
      })
    }

    function utter(text, lang) {
      try {
        var u = new window.SpeechSynthesisUtterance(text)
        var v = lang.indexOf('zh') === 0 ? picked.zh : picked.en
        if (v) { u.voice = v; u.lang = v.lang || lang } else u.lang = lang
        /* 神经网络音色在 .95 / 1.02 这个档位最像"系统在播报"：
           再快就赶，再慢就假；音调略抬一点显得干净利落 */
        u.rate = clamp01(C.voiceRate, 0.5, 2)
        u.pitch = clamp01(C.voicePitch, 0, 2)
        u.volume = clamp01(C.voiceVolume, 0, 1)
        u.onstart = function () { setMeter(true) }
        u.onend = function () { setMeter(false) }
        u.onerror = function () { setMeter(false) }
        window.speechSynthesis.speak(u)
        return true
      } catch (e) { return false }
    }

    /** 一句话的时长估算（个别引擎不触发 onend 时兜底；中文按字、英文按字符） */
    function estimateMs(items) {
      var chars = 0
      var rate = clamp01(C.voiceRate, 0.5, 2) || 1
      for (var i = 0; i < items.length; i++) {
        var t = String(items[i].t || '')
        chars += /[\u4e00-\u9fa5]/.test(t) ? t.length * 1.7 : t.length
      }
      return Math.round((chars / 13) * 1000 / rate) + 460
    }

    function buildUtter(text, lang) {
      var u = new window.SpeechSynthesisUtterance(text)
      var v = lang.indexOf('zh') === 0 ? picked.zh : picked.en
      if (v) { u.voice = v; u.lang = v.lang || lang } else u.lang = lang
      u.rate = clamp01(C.voiceRate, 0.5, 2)
      u.pitch = clamp01(C.voicePitch, 0, 2)
      u.volume = clamp01(C.voiceVolume, 0, 1)
      return u
    }

    return {
      supported: supported,
      load: load,
      attach: function (node) { meter = node },

      /**
       * 念一行，返回"这一行实际花了多久(ms)"的 Promise。
       *
       * 三个关键决定：
       *  - **不 cancel 上一句**。原来每步都 cancel，用户点快一点、或者
       *    hold 短于台词长度，语音就被硬生生掐断 —— 那正是要避免的。
       *    speechSynthesis 本身是队列，不 cancel 就是顺序播报。
       *  - 提示音响过之后留 `voiceLead` 的空白再开口，这是旁白的呼吸感。
       *  - 等 `onend` 才 resolve，动画据此决定这一步停多久；个别引擎不触发
       *    onend 时按估算兜底，绝不把动画卡死。
       */
      line: function (en, zh, asset) {
        /* 有真声素材就优先用素材（音色就是你给的那个）；拿不到再回落 TTS */
        if (asset && C.voice && C.voiceSource !== 'tts' && !finished) {
          return audio.ensureAsset(asset).then(function (ok) {
            if (!ok || finished) return 0
            if (C.voiceCue !== false) audio.cue()
            bgm.duck(true)
            return audio.playAssetAndWait(asset, 0.85).then(function (ms) {
              bgm.duck(false)
              if (ms > 0) setMeter(true)
              return ms
            })
          }).then(function (ms) {
            if (ms > 0) { saidOnce = true; return ms }
            /* 素材拿不到，这一句改走 TTS；闪避由 ttsLine 自己收尾 */
            bgm.duck(true)
            return ttsLine(en, zh)
          })
        }
        return ttsLine(en, zh)
      },

      /** TTS 路径：素材缺失 / voiceSource=tts / 终幕那条动态名字 */
      tts: function (en, zh) { return ttsLine(en, zh) },

      /** 不等待的版本：设置页试听 / 任何"念完就行"的场合 */
      say: function (en, zh) { return this.line(en, zh) },

      /** 浏览器要求"先有用户手势"才放声：第一次点击/按键解锁 + 补播当前行 */
      unlock: function () {
        try { if (supported) window.speechSynthesis.resume() } catch (e) {}
        audio.resume()
        /* 预热：先无声念一个空格，把引擎唤醒，避免第一句被吃掉开头半个字 */
        if (supported && !saidOnce) {
          try {
            var w = new window.SpeechSynthesisUtterance(' ')
            w.volume = 0
            var v = picked.en || picked.zh
            if (v) { w.voice = v; w.lang = v.lang }
            window.speechSynthesis.speak(w)
          } catch (e) { /* noop */ }
        }
        if (!saidOnce && C.voice && (pending.en || pending.zh)) this.say(pending.en, pending.zh)
      },
      speakName: spokenName,
      current: function () {
        var a = picked.en ? picked.en.name : ''
        var b = picked.zh ? picked.zh.name : ''
        return a === b ? a : (a + (b ? '  /  ' + b : ''))
      },
      listVoices: function () { return voices.slice() },
      /** 只有这里才真的掐断语音（跳过 / 揭幕） */
      stop: function () { try { if (supported) window.speechSynthesis.cancel() } catch (e) {} setMeter(false) }
    }

    /** TTS 合成并等它念完，resolve 实际时长(ms)；拿不到引擎时 resolve 0。 */
    function ttsLine(en, zh) {
      return new Promise(function (resolve) {
        if (!supported || !C.voice || finished) { resolve(0); return }
        pending = { en: en || '', zh: zh || '' }
        var items = []
        var mode = C.voiceLang
        if (mode === 'zh') { if (zh) items.push({ t: zh, l: 'zh-CN' }) }
        else if (mode === 'both') {
          if (zh) items.push({ t: zh, l: 'zh-CN' })
          if (en) items.push({ t: en, l: 'en-US' })
        } else if (en) { items.push({ t: en, l: 'en-US' }) }
        if (!items.length) { resolve(0); return }

        saidOnce = true
        bgm.duck(true)
        if (C.voiceCue !== false) audio.cue()

        var startedAt = Date.now()
        var est = estimateMs(items)
        var lead = clamp01(C.voiceLead, 0, 1200)
        var settled = false
        var watchdog = null

        function settle() {
          if (settled) return
          settled = true
          if (watchdog) clearTimeout(watchdog)
          bgm.duck(false)
          setMeter(false)
          resolve(Math.max(0, Date.now() - startedAt))
        }

        setTimeout(function () {
          if (finished) { settle(); return }
          var last = items.length - 1
          for (var i = 0; i <= last; i++) {
            var u = buildUtter(items[i].t, items[i].l)
            if (i === last) {
              u.onstart = function () { setMeter(true) }
              u.onend = settle
              u.onerror = settle
            }
            try { window.speechSynthesis.speak(u) } catch (e) { if (i === last) settle() }
          }
          watchdog = setTimeout(settle, est + 900)
        }, lead)
      })
    }
  })()

  /* 语音/音效接线：声卡要等用户手势才能出声，这里挂一次性解锁 */
  voice.attach(q('.dsu-voice'))
  ;(function preloadVoices() {
    if (C.voiceSource === 'tts') return
    for (var i = 0; i < STEPS.length; i++) if (STEPS[i].audio) audio.ensureAsset(STEPS[i].audio)
    audio.ensureAsset('line-6')   /* 终幕欢迎语 */
  })()
  /* 背景音乐：建好元素并**立即起播**，不等任何点击。
     桌面端 Electron 的 autoplayPolicy 默认宽松，所以黑幕一上来就有音乐；
     web profile 的浏览器默认策略会拦下来，start() 会自己复位，退回
     unlock() 的第一次手势 —— 同一份代码，两种宿主都不用特判。 */
  bgm.prepare()
  bgm.start()
  ;(function wireSound() {
    voice.load()
    if (voice.supported && typeof window.speechSynthesis.addEventListener === 'function') {
      try { window.speechSynthesis.addEventListener('voiceschanged', function () { voice.load() }) } catch (e) {}
    }
    var unlocked = false
    function unlock() {
      if (unlocked) return
      unlocked = true
      voice.unlock()
      bgm.start()
      document.removeEventListener('pointerdown', unlock, true)
      document.removeEventListener('keydown', unlock, true)
      document.removeEventListener('touchstart', unlock, true)
    }
    document.addEventListener('pointerdown', unlock, true)
    document.addEventListener('keydown', unlock, true)
    document.addEventListener('touchstart', unlock, true)
  })()

  /* ── 时间线 ───────────────────────────────────────────────────────────── */
  function run() {
    return Promise.resolve()
      /* 第一幕：黑幕 --> DeepSeek 标识 */
      .then(function () { return wait(820) })
      .then(function () {
        ui.blackInner.classList.add('dsu-in')
        audio.rise()
        return wait(240)
      })
      .then(function () {
        ui.blackEn.classList.add('dsu-tighten')
        return wait(880)
      })
      .then(function () {
        ui.sweep.classList.add('dsu-go')
        return wait(760)
      })
      /* 过场：过曝洗白 */
      .then(function () {
        ui.blackInner.classList.add('dsu-out')
        audio.sweep()
        ui.wash.style.transition = 'opacity ' + ms(220) + 'ms linear'
        ui.wash.style.opacity = '1'
        return wait(260)
      })
      .then(function () {
        ui.plate.style.transition = 'opacity ' + ms(900) + 'ms ease'
        ui.plate.style.opacity = '1'
        ui.watermark.style.transition = 'opacity ' + ms(1100) + 'ms ease'
        ui.watermark.style.opacity = '1'
        ui.wash.style.transition = 'opacity ' + ms(720) + 'ms cubic-bezier(.2,.9,.2,1)'
        ui.wash.style.opacity = '0'
        ui.black.style.transition = 'opacity ' + ms(720) + 'ms ease'
        ui.black.style.opacity = '0'
        /* 底板正在被"洗"出来的时候，让一道掠光横掠过去 —— 金属面被灯扫过的读法。
           和洗白错的只差一点，观众会把它当成同一次曝光的一部分。 */
        ui.sheen.classList.add('dsu-go')
        return wait(760)
      })
      /* 第二幕：品牌块 / 页脚 / 进度条 */
      .then(function () {
        ui.brand.classList.add('dsu-in')
        ui.foot.classList.add('dsu-in')
        ui.stepcount.classList.add('dsu-in')
        ui.idline.classList.add('dsu-in')
        ui.fact1.classList.add('dsu-in')
        ui.fact2.classList.add('dsu-in')
        ui.frame.classList.add('dsu-in')
        ui.slats.classList.add('dsu-in')
        ui.meta.classList.add('dsu-in')
        ui.vignette.classList.add('dsu-in')
        ui.voice.classList.add('dsu-in')
        ui.atmo.classList.add('dsu-in')
        ui.field.classList.add('dsu-in')
        ui.beams.classList.add('dsu-in')
        ui.floor.classList.add('dsu-in')
        ui.checks.classList.add('dsu-in')
        ui.trace.classList.add('dsu-in')
        root.classList.add('dsu-cam')
        setChecks(0)
        setProgress(0.12)
        setStepCount(0, 'BOOT')
        setSlats(0)
        /* 自检行用真实数字：插件条目数 / 活跃数 / 会话数，全部来自宿主 ctx */
        setSubtitle(
          '系统自检中… 插件 ' + factText(F.plugins) + ' · 活跃 ' + factText(F.activePlugins) + ' · 会话 ' + factText(F.sessions),
          'SYSTEM SELF-CHECK · ' + factText(F.activePlugins) + '/' + factText(F.plugins) + ' ACTIVE'
        )
        if (C.allowSkip) ui.skip.classList.add('dsu-in')
        return wait(700)
      })
      .then(function () { ui.black.style.display = 'none'; sweep() })
      /* 六个阶段 */
      .then(function () { return stepLoop() })
      /* 终幕 */
      .then(function () { return finale() })
      .catch(function (err) {
        try { console.warn('[dsh-startup-screen] 动画异常，直接揭幕：', err) } catch (e2) {}
        return finish()
      })
  }

  function stepLoop() {
    var chain = Promise.resolve()
    STEPS.forEach(function (step, i) {
      chain = chain.then(function () {
        if (finished) return
        var n = i + 1
        setStepCount(n, step.badge)
        setSlats(n)
        setChecks(n)
        setProgress(step.progress)
        setSubtitle(step.log, step.logEn)
        audio.step()
        /* 语音在这里起头，但**不阻塞**下面的文字渐显 —— 两者同时发生 */
        var spoke = voice.line(step.say, step.zh, step.audio)

        ui.status.classList.toggle('dsu-zhline', true)
        /* 先摘掉 dsu-in 并强制一次样式计算，新的逐字 span 才会重新走渐显动画 */
        ui.status.classList.remove('dsu-in')
        ui.subEn.classList.remove('dsu-in')
        void ui.status.offsetWidth
        /* 主字与英文字幕都走"解码"：先落同宽的替身字形，再逐位锁定成真字 */
        var slots = typeIn(ui.statusZh, step.zh, 46, 'decode')
        slots = slots.concat(typeIn(ui.subEn, step.en, 13, 'decode'))
        startDecode(slots)
        ui.caret.classList.toggle('dsu-on', !!step.action && step.action !== 'auto')
        requestAnimationFrame(function () {
          if (finished) return
          ui.status.classList.add('dsu-in')
          ui.subEn.classList.add('dsu-in')
        })
        ui.rule.classList.add('dsu-in')

        if (step.assembleLogo) assembleLogo()
        if (step.reticle) {
          /* 照原片：准星出现时中央只留状态字，鲸鱼标识和虚线收掉，别挤成一团 */
          ui.logo.classList.remove('dsu-in')
          ui.dots.hidden = true
          ui.reticle.classList.add('dsu-in')
          /* 咬合：先摘掉再加，强制一次样式计算才能重放动画 */
          ui.reticle.classList.remove('dsu-lock')
          void ui.reticle.offsetWidth
          ui.reticle.classList.add('dsu-lock')
          sweep()
        }
        if (step.hex) startHex()

        /* 这一步停多久 = max(视觉基准, 实际播报时长 + 收尾留白) —— 台词长就多停，
           台词短也不会干等。用户点得快时同样等这一句念完，不掐断。 */
        function holdFor(spoken) {
          var target = Math.max(ms(step.hold), (spoken || 0) + ms(240))
          return wait(Math.max(0, target - (spoken || 0)))
        }

        if (step.action === 'grant' || step.action === 'confirm') {
          showAction(step)
          return wait(520).then(function () { return waitAction() }).then(function () {
            hideAction()
            audio.confirm()
            ui.caret.classList.remove('dsu-on')
            return spoke
          }).then(function () { return wait(ms(320)) })
        }
        if (step.hex) {
          return spoke.then(holdFor).then(function () { stopHex() })
        }
        return spoke.then(holdFor)
      })
    })
    return chain
  }

  function assembleLogo() {
    ui.logo.classList.add('dsu-in')
    ui.dots.hidden = false
    var path = ui.logo.querySelector('path')
    if (path && typeof path.getTotalLength === 'function') {
      try {
        var len = path.getTotalLength()
        path.style.fill = 'none'
        path.style.stroke = 'currentColor'
        path.style.strokeWidth = '1.1'
        path.style.strokeDasharray = len
        path.style.strokeDashoffset = len
        /* 参考片的"一笔画成"：先勾线，再落墨成面 */
        path.animate(
          [{ strokeDashoffset: len }, { strokeDashoffset: 0 }],
          { duration: ms(1400), easing: 'cubic-bezier(.55,.06,.24,1)', fill: 'forwards' }
        )
        setTimeout(function () {
          path.style.transition = 'fill ' + ms(420) + 'ms ease, stroke-width ' + ms(420) + 'ms ease'
          path.style.fill = ''
          path.style.strokeWidth = '0'
        }, ms(1450))
      } catch (err) { /* Web Animations 不可用时直接落墨 */ }
    }
    return wait(300)
  }

  function showAction(step) {
    ui.btnZh.textContent = step.btnZh
    ui.btnEn.textContent = step.btnEn
    ui.card.hidden = step.action !== 'confirm'
    if (step.action === 'confirm') {
      ui.card.innerHTML = ''
      /* 纯中文标签 + 数字/名称，去掉英文双写与主机行，别堆信息 */
      var rows = [
        ['身份名称', C.identity, true],
        ['身份编号', C.identityId, false],
        ['权限等级', C.accessLevel, false]
      ]
      rows.forEach(function (r) {
        var row = el('div', 'dsu-card-row')
        row.appendChild(el('span', null, r[0]))
        row.appendChild(el('b', r[2] ? 'dsu-zhname' : null, r[1]))
        ui.card.appendChild(row)
      })
    }
    ui.hint.innerHTML = '按 <kbd>Enter</kbd> / <kbd>Space</kbd>，或点击按钮继续'
    ui.action.classList.add('dsu-in')
    ui.btn.classList.add('dsu-pulse')
  }

  function hideAction() {
    ui.action.classList.remove('dsu-in')
    ui.btn.classList.remove('dsu-pulse')
  }

  function finale() {
    if (finished) return Promise.resolve()
    setStepCount(6, 'WELCOME')
    setSlats(6)
    setProgress(1)
    /* 底部日志和终幕里的「欢迎 xxx 访问」区分开，避免同一句出现两遍 */
    /* 首尾呼应：这里的短指纹和开机自检读的是同一批事实 —— 换机器就变，同一台机器稳定 */
    setSubtitle('身份验证完成，会话已建立', 'IDENTITY VERIFIED · SESSION ' + factHash())
    audio.chime()
    /* 终幕欢迎语：用从 Video Project 3 截出来的真声素材（line-6）。
       素材缺失 / voiceSource=tts / voice 关闭时自动静音，不会退回 TTS 念白 ——
       因为这条素材是「欢迎访问」整句，和动态身份名称无关。 */
    var welcome = voice.line(null, null, 'line-6')

    ui.status.classList.remove('dsu-in')
    ui.subEn.classList.remove('dsu-in')
    ui.rule.classList.remove('dsu-in')
    ui.reticle.classList.remove('dsu-in')
    stopHex()
    /* 中央区（状态行/分隔线/鲸鱼/虚线）整体退场：不清掉会和 WELCOME 版式叠字 */
    ui.center.classList.add('dsu-off')
    ui.dots.hidden = true

    return wait(360)
      .then(function () {
        ui.welcome.classList.add('dsu-in')
        ui.idline.classList.remove('dsu-in')
        ui.fact1.classList.remove('dsu-in')
        ui.fact2.classList.remove('dsu-in')
        ui.checks.classList.remove('dsu-in')
        ui.trace.classList.remove('dsu-in')
        return wait(420)
      })
      .then(function () {
        /* 结尾色散故障：分片位移 + 0.5px 模糊 + 混合模式，读起来像镜头色散 */
        var targets = [ui.w1, ui.w2, ui.w4]
        targets.forEach(function (t) {
          t.classList.add('dsu-glitch')
          t.setAttribute('data-text', t.textContent)
        })
        ui.w2.classList.add('dsu-glitch-box')   /* 黑底白字那块换加色混合 */
        void ui.welcome.offsetWidth
        targets.forEach(function (t) { t.classList.add('dsu-on') })
        /* 同一下里补一条撕裂带扫过全屏：色散层做"字在抖"，这层做"画面在撕" */
        ui.tear.classList.add('dsu-go')
        return wait(640)
      })
      /* 光晕从中心炸开 + 变形光条横扫 + 文字青色 halation */
      .then(function () {
        root.classList.add('dsu-peak')
        setTimeout(function () { root.classList.remove('dsu-peak') }, Math.max(600, ms(1200)))
        ui.bloom.classList.add('dsu-go')
        ui.streak.classList.add('dsu-go')
        return wait(430)
      })
      /* 光晕最亮那一下补一记极短高光（是"眨一下"，不是长时间糊白） */
      .then(function () {
        ui.flash.style.transition = 'opacity ' + ms(70) + 'ms linear'
        ui.flash.style.opacity = '0.32'
        return wait(90)
      })
      .then(function () {
        ui.flash.style.transition = 'opacity ' + ms(340) + 'ms cubic-bezier(0.4, 0, 0.2, 1)'
        ui.flash.style.opacity = '0'
        return wait(320)
      })
      /* 欢迎语没播完就不揭幕（素材缺失时这个 Promise 立刻 resolve，不影响节奏） */
      .then(function () { return welcome })
      /* 揭幕：整个机位后撤 + 轻微失焦，比单纯淡出更有镜头感 */
      .then(function () {
        root.classList.add('dsu-handoff')
        /* 音乐从"镜头后撤"这一刻就开始退，而不是等 finish()。
           要盖住 780ms 的后撤 + 900ms 的整体移出，1700ms 才收得住；
           只放在 finish() 里就只剩 900ms，且那时画面已经在淡出，听着还是断。 */
        bgm.stop(1700)
        return wait(780)
      })
      .then(function () { return finish() })
  }

  function finish() {
    if (finished) return
    finished = true
    releaseAll()
    stopHex()
    voice.stop()
    /* 跳过（Esc / SKIP）走的是这条路，没有上面的 1700ms 前摇，这里兜 900ms；
       正常揭幕时前面已经 stop 过，这一句会被幂等忽略。 */
    bgm.stop(900)
    if (clockTimer) { clearInterval(clockTimer); clockTimer = null }
    if (ui.traceTimer) { clearInterval(ui.traceTimer); ui.traceTimer = null }
    stopField()
    try {
      if (C.mode === 'session') window.sessionStorage.setItem('dsh-startup-screen:shown', '1')
      if (C.mode === 'daily') {
        window.localStorage.setItem('dsh-startup-screen:day', new Date().toISOString().slice(0, 10))
      }
    } catch (err) { /* 隐私模式下不可写，忽略 */ }
    /* 解锁：Host 注入的关键样式用 html.dsu-lock 锁滚动，这里必须一并摘掉，
       否则揭幕后主界面会永远滚不动。 */
    document.documentElement.classList.remove('dsu-lock')
    document.documentElement.style.overflow = ''
    if (document.body) document.body.style.overflow = ''
    root.classList.remove('dsu-boot')
    root.classList.add('dsu-gone')
    setTimeout(function () {
      if (root && root.parentNode) root.parentNode.removeChild(root)
      try { window.dispatchEvent(new CustomEvent('dsh-startup:done')) } catch (err) { /* noop */ }
    }, ms(900))
  }

  function requestSkip() {
    if (skipped || finished) return
    skipped = true
    releaseAll()
    finish()
  }

  if (C.allowSkip && ui.skip) {
    ui.skip.hidden = false
    ui.skip.addEventListener('click', function (e) { e.stopPropagation(); requestSkip() })
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') requestSkip()
    })
  } else if (ui.skip) {
    ui.skip.hidden = true
  }

  /* 对外 API：settings 面板里的"立即预览"按钮和重播都走它 */
  window.__DSH_STARTUP_API__ = {
    /* 设置页试听与冒烟测试用：直接念一行 */
    speak: function (en, zh) { voice.say(en, zh) },
    speakName: function (n) { return voice.speakName(n) },
    get voice() { return voice.current() },
    replay: injectSelf,
    skip: requestSkip,
    finish: finish,
    config: C
  }

  /* 锁滚动，动画期间不露出下面的应用 */
  document.documentElement.classList.add('dsu-lock')
  document.documentElement.style.overflow = 'hidden'
  if (document.body) document.body.style.overflow = 'hidden'
  setProgress(0.05)
  root.classList.remove('dsu-boot')

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { run() })
  } else {
    run()
  }
})()
