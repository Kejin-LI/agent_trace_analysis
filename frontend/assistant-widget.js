// ===== Agent Synapse 全站智能助手浮窗 =====
// 自包含组件：气泡 + 面板 + 可见胶囊 + 流式对话 + 全局划词。
// 知识库常驻在后端 system prompt；详情页可现场压缩 trace 纪要做轨迹问答。
// 后端无状态：trace 由前端压缩后随请求传入，后端不读库、内存恒定。
(function () {
  if (window.__assistantWidgetMounted) return;
  window.__assistantWidgetMounted = true;

  // ---------- 工具 ----------
  function buildChatUrl() {
    // 优先复用 db-api 的 buildUrl（带网关前缀解析）；docs 等未引入 db-api 的页面回退到同源 basePath。
    if (window.AgentTraceDB && typeof window.AgentTraceDB.buildUrl === 'function') {
      return window.AgentTraceDB.buildUrl('/api/chat');
    }
    const p = window.location.pathname;
    const idx = p.lastIndexOf('/');
    const base = idx >= 0 ? p.slice(0, idx + 1) : '/';
    return window.location.origin + base + 'api/chat';
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  // 极简 Markdown → HTML（粗体 / 行内代码 / 有序无序列表 / 段落）。
  function mdToHtml(md) {
    const lines = String(md || '').replace(/\r\n/g, '\n').split('\n');
    const inline = (t) => esc(t)
      .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
      .replace(/`([^`]+)`/g, '<code>$1</code>');
    const out = [];
    let inUl = false, inOl = false;
    const closeLists = () => {
      if (inUl) { out.push('</ul>'); inUl = false; }
      if (inOl) { out.push('</ol>'); inOl = false; }
    };
    for (const raw of lines) {
      const line = raw.trim();
      if (!line) { closeLists(); continue; }
      let m;
      if ((m = line.match(/^#{1,4}\s+(.*)$/))) { closeLists(); out.push('<p class="aw-h">' + inline(m[1]) + '</p>'); continue; }
      if ((m = line.match(/^\d+\.\s+(.*)$/))) {
        if (!inOl) { closeLists(); out.push('<ol>'); inOl = true; }
        out.push('<li>' + inline(m[1]) + '</li>');
        continue;
      }
      if ((m = line.match(/^[-*]\s+(.*)$/))) {
        if (!inUl) { closeLists(); out.push('<ul>'); inUl = true; }
        out.push('<li>' + inline(m[1]) + '</li>');
        continue;
      }
      closeLists();
      out.push('<p>' + inline(line) + '</p>');
    }
    closeLists();
    return out.join('');
  }

  // 判断问题是否与当前 session 轨迹相关（启发式，不额外调模型）。
  const TRACE_HINT = /(轨迹|trace|这一?步|那一?步|这条|这次|本次|当前|这轮|那轮|为什么|为何|失败|报错|错误|空转|循环|卡住|重复|工具|调用|步骤|这段|上面|这里|根因|分析|会话|session|模型|输出|思考|跑偏|绕|兜圈|哪里|怎么|如何)/i;

  function hasTraceCtx() {
    return !!(window.AssistantTrace && typeof window.AssistantTrace.hasTrace === 'function' && window.AssistantTrace.hasTrace());
  }

  // ---------- 样式 ----------
  const style = document.createElement('style');
  style.textContent = `
  #aw-fab{position:fixed;right:24px;bottom:24px;z-index:2147483600;width:52px;height:52px;border-radius:9999px;border:1px solid rgba(255,255,255,0.6);cursor:pointer;display:flex;align-items:center;justify-content:center;background:linear-gradient(135deg,#0071E3,#BF5AF2);box-shadow:0 8px 28px rgba(0,113,227,0.32),0 2px 8px rgba(0,0,0,0.12);transition:transform .18s cubic-bezier(.34,1.56,.64,1),box-shadow .18s ease;}
  #aw-fab:hover{transform:translateY(-2px) scale(1.04);box-shadow:0 12px 34px rgba(0,113,227,0.4);}
  #aw-fab svg{width:24px;height:24px;color:#fff;transform-origin:50% 50%;animation:aw-spin 9s linear infinite;}
  #aw-fab:hover svg{animation-duration:3s;}
  @keyframes aw-spin{from{transform:rotate(0deg)}to{transform:rotate(360deg)}}
  @media (prefers-reduced-motion:reduce){#aw-fab svg{animation:none;}}
  #aw-fab.aw-open{transform:scale(0.9);opacity:0;pointer-events:none;}
  #aw-panel{position:fixed;right:24px;bottom:24px;z-index:2147483500;width:380px;max-width:calc(100vw - 32px);height:560px;max-height:calc(100vh - 48px);display:flex;flex-direction:column;border-radius:20px;overflow:hidden;font-family:'Inter',-apple-system,BlinkMacSystemFont,'SF Pro Display',sans-serif;letter-spacing:-0.01em;background:rgba(255,255,255,0.82);backdrop-filter:blur(30px) saturate(200%);-webkit-backdrop-filter:blur(30px) saturate(200%);border:1px solid rgba(0,0,0,0.06);box-shadow:0 20px 60px rgba(0,0,0,0.18);opacity:0;transform:translateY(12px) scale(0.98);pointer-events:none;transition:opacity .2s ease,transform .2s cubic-bezier(.34,1.4,.64,1);}
  #aw-panel.aw-show{opacity:1;transform:translateY(0) scale(1);pointer-events:auto;}
  .aw-head{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid rgba(0,0,0,0.06);background:rgba(255,255,255,0.5);}
  .aw-logo{width:30px;height:30px;border-radius:9px;flex-shrink:0;display:flex;align-items:center;justify-content:center;background:linear-gradient(135deg,#0071E3,#BF5AF2);}
  .aw-logo svg{width:18px;height:18px;color:#fff;}
  .aw-title{font-size:14px;font-weight:600;color:#1D1D1F;line-height:1.2;}
  .aw-head-spacer{flex:1;}
  .aw-icon-btn{width:28px;height:28px;border-radius:8px;border:none;background:transparent;cursor:pointer;display:flex;align-items:center;justify-content:center;color:#6E6E73;transition:background .15s ease;}
  .aw-icon-btn:hover{background:rgba(0,0,0,0.06);color:#1D1D1F;}
  .aw-icon-btn svg{width:16px;height:16px;}
  .aw-body{flex:1;overflow-y:auto;padding:16px;display:flex;flex-direction:column;gap:12px;}
  .aw-msg{max-width:88%;font-size:13px;line-height:1.7;border-radius:14px;padding:9px 12px;word-break:break-word;}
  .aw-msg.user{align-self:flex-end;background:linear-gradient(135deg,#0071E3,#0a84ff);color:#fff;border-bottom-right-radius:5px;}
  .aw-msg.assistant{align-self:flex-start;background:rgba(0,0,0,0.045);color:#1D1D1F;border-bottom-left-radius:5px;}
  .aw-msg.assistant p{margin:0 0 6px;}
  .aw-msg.assistant p:last-child{margin-bottom:0;}
  .aw-msg.assistant p.aw-h{font-weight:600;margin-top:4px;}
  .aw-msg.assistant ul,.aw-msg.assistant ol{margin:4px 0 6px;padding-left:18px;}
  .aw-msg.assistant li{margin:3px 0;}
  .aw-msg.assistant code{background:rgba(0,0,0,0.06);padding:1px 5px;border-radius:4px;font-family:'SF Mono',monospace;font-size:11.5px;}
  .aw-msg.assistant.err{background:rgba(255,69,58,0.08);color:#c4291c;}
  .aw-quote-in-msg{display:block;font-size:11.5px;opacity:0.82;border-left:2px solid rgba(255,255,255,0.5);padding-left:8px;margin-bottom:5px;}
  .aw-empty{display:flex;flex-direction:column;align-items:stretch;padding:4px 2px;}
  .aw-greet{font-size:18px;font-weight:600;color:#1D1D1F;line-height:1.4;letter-spacing:-0.02em;margin-bottom:18px;}
  .aw-greet .aw-wave{display:inline-block;margin-left:2px;}
  .aw-sugs{display:flex;flex-direction:column;gap:10px;}
  .aw-sug{display:flex;align-items:center;gap:10px;padding:13px 14px;border-radius:13px;background:rgba(118,118,128,0.07);border:1px solid transparent;cursor:pointer;text-align:left;transition:background .15s ease,border-color .15s ease,transform .12s ease;}
  .aw-sug:hover{background:rgba(0,113,227,0.07);border-color:rgba(0,113,227,0.16);transform:translateY(-1px);}
  .aw-sug-txt{flex:1;font-size:13px;line-height:1.5;color:#1D1D1F;}
  .aw-sug-arrow{flex-shrink:0;width:16px;height:16px;color:#C7C7CC;transition:color .15s ease,transform .15s ease;}
  .aw-sug:hover .aw-sug-arrow{color:#0071E3;transform:translateX(2px);}
  .aw-sug-arrow svg{width:16px;height:16px;}
  .aw-cursor{display:inline-block;width:7px;height:14px;background:#0071E3;border-radius:2px;vertical-align:text-bottom;margin-left:1px;animation:aw-blink 1s steps(2) infinite;}
  @keyframes aw-blink{0%,50%{opacity:1}50.01%,100%{opacity:0}}
  .aw-foot{border-top:1px solid rgba(0,0,0,0.06);padding:10px 12px 12px;background:rgba(255,255,255,0.5);}
  .aw-capsules{display:flex;flex-wrap:wrap;gap:6px;margin-bottom:8px;}
  .aw-cap{display:inline-flex;align-items:center;gap:5px;padding:3px 9px;border-radius:9999px;font-size:11px;font-weight:500;border:1px solid transparent;user-select:none;}
  .aw-cap .dot{width:6px;height:6px;border-radius:9999px;}
  .aw-cap-kb{background:rgba(0,113,227,0.08);color:#0071E3;border-color:rgba(0,113,227,0.14);}
  .aw-cap-kb .dot{background:#0071E3;}
  .aw-cap-trace{cursor:pointer;transition:background .15s ease;}
  .aw-cap-trace.auto{background:rgba(110,110,115,0.1);color:#6E6E73;border-color:rgba(0,0,0,0.06);}
  .aw-cap-trace.auto .dot{background:#A1A1A6;}
  .aw-cap-trace.on{background:rgba(48,209,88,0.12);color:#1e8e3e;border-color:rgba(48,209,88,0.2);}
  .aw-cap-trace.on .dot{background:#30D158;}
  .aw-cap-trace.off{background:rgba(0,0,0,0.04);color:#A1A1A6;border-color:rgba(0,0,0,0.05);}
  .aw-cap-trace.off .dot{background:#C7C7CC;}
  .aw-quote{display:none;max-width:100%;margin-bottom:8px;font-size:11px;line-height:1.45;color:#A1A1A6;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;cursor:pointer;}
  .aw-quote.show{display:block;}
  .aw-quote::before{content:'|';display:inline-block;margin-right:6px;color:rgba(120,120,128,0.72);font-weight:600;}
  .aw-quote:hover{color:#6E6E73;}
  .aw-input-row{position:relative;}
  .aw-input{display:block;width:100%;resize:none;border:1px solid rgba(0,0,0,0.1);border-radius:14px;padding:12px 52px 12px 12px;font-size:13px;line-height:1.5;font-family:inherit;color:#1D1D1F;background:rgba(255,255,255,0.9);min-height:72px;max-height:152px;outline:none;transition:border-color .15s ease;}
  .aw-input:focus{border-color:#0071E3;}
  .aw-send{position:absolute;right:8px;bottom:8px;width:34px;height:34px;border-radius:10px;border:none;cursor:pointer;display:flex;align-items:center;justify-content:center;background:linear-gradient(135deg,#0071E3,#BF5AF2);color:#fff;transition:opacity .15s ease,transform .15s ease;box-shadow:0 4px 12px rgba(0,113,227,0.18);}
  .aw-send:hover{transform:scale(1.05);}
  .aw-send:disabled{opacity:0.4;cursor:not-allowed;transform:none;}
  .aw-send svg{width:17px;height:17px;}
  @media (max-width:480px){#aw-panel{right:8px;bottom:8px;width:calc(100vw - 16px);height:calc(100vh - 16px);}}
  `;
  document.head.appendChild(style);

  // ---------- DOM ----------
  // Synapse 突触：与平台 logo 完全一致（中心核 + 4 个卫星节点 = 5 个点）。FAB 与头部 logo 共用。
  const ICON_SYNAPSE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M6.5 6.5L12 11M18 6L12 11M12 11L8 17.5M12 11L17 17"/><circle cx="6.5" cy="6.5" r="1.9" fill="currentColor" stroke="none"/><circle cx="18" cy="6" r="1.5" fill="currentColor" stroke="none"/><circle cx="8" cy="17.5" r="1.5" fill="currentColor" stroke="none"/><circle cx="17" cy="17" r="1.8" fill="currentColor" stroke="none"/><circle cx="12" cy="11" r="2.4" fill="currentColor" stroke="none"/></svg>';
  const ICON_LOGO = ICON_SYNAPSE;
  const ICON_CLOSE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>';
  const ICON_SEND = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2 11 13M22 2l-7 20-4-9-9-4 20-7z"/></svg>';
  const ICON_ARROW = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6"/></svg>';

  const fab = document.createElement('button');
  fab.id = 'aw-fab';
  fab.setAttribute('aria-label', '打开智能助手');
  fab.innerHTML = ICON_SYNAPSE;

  const panel = document.createElement('div');
  panel.id = 'aw-panel';
  panel.innerHTML = `
    <div class="aw-head">
      <div class="aw-logo">${ICON_LOGO}</div>
      <div class="aw-title">Agent Synapse智能助手</div>
      <div class="aw-head-spacer"></div>
      <button class="aw-icon-btn" id="aw-close" title="收起">${ICON_CLOSE}</button>
    </div>
    <div class="aw-body" id="aw-body"></div>
    <div class="aw-foot">
      <div class="aw-capsules" id="aw-capsules"></div>
      <div class="aw-quote" id="aw-quote" title="点击取消引用"></div>
      <div class="aw-input-row">
        <textarea class="aw-input" id="aw-input" rows="1" placeholder="问问平台口径、概念或排查思路…"></textarea>
        <button class="aw-send" id="aw-send" disabled>${ICON_SEND}</button>
      </div>
    </div>`;

  document.body.appendChild(fab);
  document.body.appendChild(panel);

  const bodyEl = panel.querySelector('#aw-body');
  const inputEl = panel.querySelector('#aw-input');
  const sendBtn = panel.querySelector('#aw-send');
  const capsulesEl = panel.querySelector('#aw-capsules');
  const quoteEl = panel.querySelector('#aw-quote');

  // ---------- 状态 ----------
  const state = {
    open: false,
    running: false,
    messages: [],          // {role, content, quote?}
    quote: '',             // 当前划词引用
    traceMode: 'auto',     // auto | on | off （仅详情页有意义）
    abortCtrl: null,
  };

  const SUGGESTIONS = [
    '我该怎么使用这个平台？',
    '大盘里的雷达图怎么看？',
    '发现异常聚类后该怎么分析？',
  ];

  const SUGGESTIONS_TRACE = [
    '这次会话为什么健康度偏低？',
    '哪一步是关键路径上的瓶颈？',
    '帮我定位失败或空转的环节',
    '总结这条轨迹的主要问题',
  ];

  // ---------- 渲染 ----------
  function renderMessages() {
    if (!state.messages.length && !state.running) {
      const onDetail = hasTraceCtx();
      const greet = onDetail ? '我已读到本次会话的完整轨迹，想分析点什么？' : '今天我能为你做些什么？';
      const sugs = onDetail ? SUGGESTIONS_TRACE : SUGGESTIONS;
      bodyEl.innerHTML =
        `<div class="aw-empty">` +
          `<div class="aw-greet">Hi <span class="aw-wave">👋</span>，${esc(greet)}</div>` +
          `<div class="aw-sugs">` +
            sugs.map(q => `<button class="aw-sug" type="button"><span class="aw-sug-txt">${esc(q)}</span><span class="aw-sug-arrow">${ICON_ARROW}</span></button>`).join('') +
          `</div>` +
        `</div>`;
      bodyEl.querySelectorAll('.aw-sug').forEach(el => {
        el.addEventListener('click', () => { inputEl.value = el.querySelector('.aw-sug-txt').textContent; updateSendState(); send(); });
      });
      return;
    }
    const html = state.messages.map(m => {
      if (m.role === 'user') {
        const q = m.quote ? `<span class="aw-quote-in-msg">${esc(m.quote)}</span>` : '';
        return `<div class="aw-msg user">${q}${esc(m.content)}</div>`;
      }
      const cls = m.error ? 'aw-msg assistant err' : 'aw-msg assistant';
      const cursor = m.pending ? '<span class="aw-cursor"></span>' : '';
      return `<div class="${cls}">${mdToHtml(m.content)}${cursor}</div>`;
    }).join('');
    bodyEl.innerHTML = html;
    bodyEl.scrollTop = bodyEl.scrollHeight;
  }

  function renderCapsules() {
    const caps = ['<span class="aw-cap aw-cap-kb"><span class="dot"></span>知识库</span>'];
    if (hasTraceCtx()) {
      const label = { auto: '轨迹·自动', on: '轨迹·始终', off: '轨迹·关闭' }[state.traceMode];
      caps.push(`<span class="aw-cap aw-cap-trace ${state.traceMode}" id="aw-cap-trace" title="点击切换：自动 / 始终 / 关闭携带本会话轨迹"><span class="dot"></span>${label}</span>`);
    }
    capsulesEl.innerHTML = caps.join('');
    const traceCap = capsulesEl.querySelector('#aw-cap-trace');
    if (traceCap) traceCap.addEventListener('click', () => {
      state.traceMode = { auto: 'on', on: 'off', off: 'auto' }[state.traceMode];
      renderCapsules();
    });
  }

  function renderQuote() {
    if (state.quote) {
      quoteEl.textContent = state.quote;
      quoteEl.classList.add('show');
    } else {
      quoteEl.textContent = '';
      quoteEl.classList.remove('show');
    }
  }

  function updateSendState() {
    sendBtn.disabled = state.running || !inputEl.value.trim();
  }

  function autoGrow() {
    inputEl.style.height = 'auto';
    inputEl.style.height = Math.min(Math.max(inputEl.scrollHeight, 72), 152) + 'px';
  }

  // ---------- 打开 / 关闭 ----------
  function openPanel() {
    state.open = true;
    fab.classList.add('aw-open');
    panel.classList.add('aw-show');
    renderCapsules();
    renderQuote();
    renderMessages();
    setTimeout(() => inputEl.focus(), 60);
  }
  function closePanel() {
    state.open = false;
    fab.classList.remove('aw-open');
    panel.classList.remove('aw-show');
  }

  // ---------- 发送 ----------
  function decideTrace(question) {
    if (!hasTraceCtx()) return false;
    if (state.traceMode === 'off') return false;
    if (state.traceMode === 'on') return true;
    // auto：有划词引用，或问题命中轨迹相关关键词时才携带
    return !!state.quote || TRACE_HINT.test(question);
  }

  async function send() {
    const question = inputEl.value.trim();
    if (!question || state.running) return;
    const quote = state.quote;

    state.messages.push({ role: 'user', content: question, quote });
    const asst = { role: 'assistant', content: '', pending: true };
    state.messages.push(asst);

    inputEl.value = '';
    state.quote = '';
    autoGrow();
    state.running = true;
    updateSendState();
    renderQuote();
    renderMessages();

    // 组装请求：仅历史的纯文本对话（不含本条 pending）。
    const history = state.messages
      .filter(m => !m.pending && (m.role === 'user' || m.role === 'assistant'))
      .slice(0, -1)
      .slice(-8)
      .map(m => ({ role: m.role, content: m.content }));

    const payload = { question, history };
    if (quote) payload.selected_text = quote;
    if (decideTrace(question)) {
      try {
        const summary = window.AssistantTrace.buildSummary();
        if (summary) {
          payload.trace_summary = summary;
          payload.session_id = window.AssistantTrace.sessionId() || '';
          payload.session_title = window.AssistantTrace.sessionTitle() || '';
        }
      } catch (e) { /* 轨迹压缩失败则退化为纯知识库问答 */ }
    }

    try {
      await streamChat(payload, (delta) => {
        asst.content += delta;
        renderMessages();
      });
      asst.pending = false;
      renderMessages();
    } catch (err) {
      if (err && err.name === 'AbortError') {
        asst.pending = false;
        if (!asst.content) { asst.content = '_已停止。_'; }
        renderMessages();
      } else {
        asst.pending = false;
        asst.error = true;
        asst.content = '请求失败：' + (err && err.message ? err.message : String(err));
        renderMessages();
      }
    } finally {
      state.running = false;
      updateSendState();
    }
  }

  async function streamChat(payload, onDelta) {
    const ctrl = new AbortController();
    state.abortCtrl = ctrl;
    const resp = await fetch(buildChatUrl(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify(payload),
      signal: ctrl.signal,
    });
    if (!resp.ok || !resp.body) {
      const t = await resp.text().catch(() => '');
      throw new Error('HTTP ' + resp.status + (t ? (' · ' + t.slice(0, 160)) : ''));
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buf = '';
    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const blocks = buf.split('\n\n');
        buf = blocks.pop();
        for (const block of blocks) {
          let ev = '', dataStr = '';
          for (const ln of block.split('\n')) {
            if (ln.startsWith('event:')) ev = ln.slice(6).trim();
            else if (ln.startsWith('data:')) dataStr += ln.slice(5).trim();
          }
          if (!dataStr) continue;
          let data;
          try { data = JSON.parse(dataStr); } catch (e) { continue; }
          if (ev === 'error') throw new Error(data.error || '对话失败');
          if (ev === 'done') continue;
          if (typeof data.delta === 'string') onDelta(data.delta);
        }
      }
    } finally {
      if (state.abortCtrl === ctrl) state.abortCtrl = null;
    }
  }

  // ---------- 全局划词 ----------
  function captureSelection() {
    if (!state.open || state.running) return;
    const sel = window.getSelection ? window.getSelection() : null;
    if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return;
    // 忽略在浮窗自身内部的选择，避免把助手回答自己引用进去。
    const anchor = sel.anchorNode;
    const el = anchor && anchor.nodeType === 3 ? anchor.parentNode : anchor;
    if (el && panel.contains(el)) return;
    const text = String(sel.toString() || '').replace(/\s+/g, ' ').trim();
    if (!text) return;
    state.quote = text.length > 160 ? text.slice(0, 160) + '…' : text;
    renderQuote();
  }

  // ---------- 事件 ----------
  fab.addEventListener('click', openPanel);
  panel.querySelector('#aw-close').addEventListener('click', closePanel);
  quoteEl.addEventListener('click', () => { state.quote = ''; renderQuote(); });
  sendBtn.addEventListener('click', send);
  inputEl.addEventListener('input', () => { autoGrow(); updateSendState(); });
  inputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
  });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && state.open) closePanel(); });
  document.addEventListener('mouseup', () => requestAnimationFrame(captureSelection));
})();
