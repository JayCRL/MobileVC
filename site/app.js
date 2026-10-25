(() => {
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => [...r.querySelectorAll(s)];

  const header = $('#header');
  const onScroll = () => header?.classList.toggle('scrolled', window.scrollY > 8);
  onScroll();
  window.addEventListener('scroll', onScroll, { passive: true });

  const nav = $('#nav');
  const navToggle = $('#nav-toggle');
  navToggle?.addEventListener('click', () => {
    const open = nav?.classList.toggle('open');
    navToggle.setAttribute('aria-expanded', String(Boolean(open)));
  });
  $$('#nav a').forEach((a) => a.addEventListener('click', () => {
    nav?.classList.remove('open');
    navToggle?.setAttribute('aria-expanded', 'false');
  }));

  const reveal = $$('[data-reveal]');
  if ('IntersectionObserver' in window) {
    const io = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add('in');
        io.unobserve(entry.target);
      }
    }, { rootMargin: '0px 0px -8% 0px', threshold: 0.08 });
    reveal.forEach((el) => io.observe(el));
  } else {
    reveal.forEach((el) => el.classList.add('in'));
  }

  const shots = {
    chat: {
      src: 'site/media/chat.png',
      title: '会话，是主角',
      desc: '用户消息、思考、工具调用与回复按时间线排列。发送走 DSH，不另起炉灶。',
      bullets: ['SSE 实时流 + durable 快照', '图片与文件直接进 prompt', '浅色 / 深色，随系统或手动切换'],
    },
    trace: {
      src: 'site/media/trace.png',
      title: '执行迹，默认说人话',
      desc: '简洁模式按思考与工具链分组，中文工具名，英文原文折叠；需要时再切完整。',
      bullets: ['思考分组，默认收起英文原文', '工具链显示耗时与状态', '完整模式保留参数和原始推理'],
    },
    sessions: {
      src: 'site/media/sessions.png',
      title: '在会话之间切换',
      desc: '最近会话、运行状态和最后一条消息。打开就是同一份持久化历史。',
      bullets: ['真实 session / cwd / 状态', '空闲与运行中一眼可见', '支持分叉与子会话'],
    },
    plugins: {
      src: 'site/media/plugins.png',
      title: '插件，也在手机上',
      desc: '查看 DSH 插件与 bundle 状态。启停仍由 Host 的 pluginManager 决定。',
      bullets: ['真实 pluginManager 数据', '查看启用状态与只读原因', '改动只在 Host 生效'],
    },
  };
  const order = ['chat', 'trace', 'sessions', 'plugins'];
  const shotEl = $('#shot');
  const shotTitle = $('#shot-title');
  const shotDesc = $('#shot-desc');
  const shotBullets = $('#shot-bullets');
  const shotIndex = $('#shot-index');
  $$('.gallery-tabs button').forEach((btn) => btn.addEventListener('click', () => {
    const key = btn.dataset.shot;
    const item = shots[key];
    if (!item) return;
    $$('.gallery-tabs button').forEach((b) => b.classList.toggle('active', b === btn));
    if (shotEl) {
      shotEl.classList.add('switching');
      setTimeout(() => {
        shotEl.src = item.src;
        shotEl.alt = `MobileVC ${item.title}`;
        shotEl.classList.remove('switching');
      }, 110);
    }
    if (shotTitle) shotTitle.textContent = item.title;
    if (shotDesc) shotDesc.textContent = item.desc;
    if (shotBullets) shotBullets.innerHTML = item.bullets.map((b) => `<li>${b}</li>`).join('');
    if (shotIndex) shotIndex.textContent = `${String(order.indexOf(key) + 1).padStart(2, '0')} / 04`;
  }));

  $$('[data-copy]').forEach((btn) => btn.addEventListener('click', async () => {
    const target = $(btn.dataset.copy);
    if (!target) return;
    const text = target.innerText;
    try {
      await navigator.clipboard.writeText(text);
      const old = btn.textContent;
      btn.textContent = '已复制';
      setTimeout(() => { btn.textContent = old; }, 1400);
    } catch {
      const range = document.createRange();
      range.selectNodeContents(target);
      const sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
    }
  }));

  const year = $('#year');
  if (year) year.textContent = String(new Date().getFullYear());
})();
