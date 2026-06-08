(function () {
  function uniq(arr) {
    return [...new Set(arr.filter(Boolean))];
  }

  function param(name) {
    return new URLSearchParams(window.location.search).get(name);
  }

  // 部署在 agentic-aidp.bytedance.net 这种带网关前缀的环境下，本地回环兜底毫无意义且会污染缓存，
  // 因此仅在 localhost / 内网开发地址下才尝试 127.0.0.1。
  function isLocalEnv() {
    const h = window.location.hostname;
    return h === 'localhost' || h === '127.0.0.1' || h === '0.0.0.0';
  }

  function candidateBases() {
    const manual = param('api_base') || param('apiBase');
    const saved = isLocalEnv() ? localStorage.getItem('agenttrace.apiBase') : null;
    const origin = window.location.origin;
    const fallbacks = [];
    if (isLocalEnv()) {
      if (origin !== 'http://127.0.0.1:18080') fallbacks.push('http://127.0.0.1:18080');
      if (origin !== 'http://localhost:18080') fallbacks.push('http://localhost:18080');
    }
    return uniq([manual, saved, origin, ...fallbacks]);
  }

  // 当前页面挂在 /trace_sever/ 网关前缀下时，API 也要带同样的前缀（注意 sever 是网关侧的实际拼写）。
  function apiPrefix() {
    return window.location.pathname.startsWith('/trace_sever/') ? '/trace_sever' : '';
  }

  // 页面所有以 "/xxx" 开头的导航链接（明细/异常/详情等）需要改写成 "/trace_sever/xxx"，
  // 否则浏览器会跳到主域根路径，被网关以默认页接管，看上去像"回到主菜单"。
  function rewriteNavLinks(root) {
    const prefix = apiPrefix();
    if (!prefix) return;
    const scope = root || document;
    const apply = (el, attr) => {
      const raw = el.getAttribute(attr);
      if (!raw || !raw.startsWith('/') || raw.startsWith('//')) return;
      if (raw.startsWith(prefix + '/') || raw === prefix) return;
      el.setAttribute(attr, prefix + raw);
    };
    scope.querySelectorAll('a[href^="/"]:not([href^="//"])').forEach(a => apply(a, 'href'));
    // 列表行的 data-href 也要一并处理（点击后由 JS 跳转）。
    scope.querySelectorAll('[data-href^="/"]:not([data-href^="//"])').forEach(el => apply(el, 'data-href'));
  }
  function startNavRewriter() {
    rewriteNavLinks();
    // 业务页面通过 innerHTML 动态生成的 <a>/<tr data-href> 也需要改写，用 MutationObserver
    // 全局监听一次，比让每个页面手动调用更稳。
    if (typeof MutationObserver === 'undefined') return;
    const obs = new MutationObserver(muts => {
      for (const m of muts) {
        m.addedNodes.forEach(n => {
          if (n.nodeType === 1) rewriteNavLinks(n);
        });
      }
    });
    obs.observe(document.body, { childList: true, subtree: true });
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', startNavRewriter);
  } else {
    startNavRewriter();
  }
  window.__rewriteNavLinks = rewriteNavLinks;

  let resolvedBase = null;

  async function fetchJSON(path) {
    const errors = [];
    const bases = resolvedBase ? [resolvedBase] : candidateBases();
    const finalPath = apiPrefix() + path;
    for (const base of bases) {
      const url = base.replace(/\/$/, '') + finalPath;
      try {
        // same-origin 请求需要带 cookie 才能透传到上游 SSO。
        const resp = await fetch(url, { cache: 'no-store', credentials: 'same-origin' });
        if (!resp.ok) throw new Error('HTTP ' + resp.status + ' @ ' + url);
        const data = await resp.json();
        resolvedBase = base;
        if (isLocalEnv()) localStorage.setItem('agenttrace.apiBase', base);
        return data;
      } catch (err) {
        errors.push(err.message || String(err));
      }
    }
    throw new Error(errors.join(' | '));
  }

  function scoreBand(score) {
    if (score >= 85) return 'green';
    if (score >= 70) return 'orange';
    if (score >= 50) return 'purple';
    return 'red';
  }

  function chipText(session) {
    const failed = (session.rules || []).find(r => r && r.passed === false);
    if (failed) return failed.failed_label || failed.name || '异常';
    const score = session.score || 0;
    if (score >= 85) return '健康';
    if (score >= 70) return '注意';
    if (score >= 50) return '亚健康';
    return '严重';
  }

  function normalizeTrace(trace) {
    const spans = (trace.spans || []).map(sp => ({
      span_id: sp.span_id,
      parent_id: sp.parent_id,
      span_name: sp.span_name,
      span_type: sp.span_type,
      duration_ms: Number(sp.duration_ms || 0),
      started_at_ms: Number(sp.started_at_ms || 0),
      started_at: sp.started_at || '',
      status_code: Number(sp.status_code || 0),
      input: sp.input || '',
      output: sp.output || '',
      custom_tags: sp.custom_tags || '{}',
      user_prompt: sp.user_prompt || sp.userPrompt || '',
      prompt_source: sp.prompt_source || sp.promptSource || '',
      round_index: Number(sp.round_index || sp.roundIndex || 0),
    }));
    return {
      trace_id: trace.trace_id,
      span_id: trace.span_id,
      title: trace.title || '',
      user_prompt: trace.user_prompt || trace.userPrompt || '',
      round_count: Number(trace.round_count || trace.roundCount || 0),
      model_name: trace.model_name || '',
      turns: Number(trace.turns || 0),
      duration_ms: Number(trace.duration_ms || 0),
      llm_pure_ms: Number(trace.llm_pure_ms || 0),
      tool_ms: Number(trace.tool_ms || 0),
      input_tokens: Number(trace.input_tokens || 0),
      output_tokens: Number(trace.output_tokens || 0),
      started_at_ms: Number(trace.started_at_ms || 0),
      started_at: trace.started_at || '',
      status: trace.status || '',
      spans,
    };
  }

  function normalizeSession(raw) {
    const traces = (raw.traces || []).map(normalizeTrace);
    const allSpans = traces.flatMap(t => t.spans || []);
    const modelSpans = allSpans.filter(sp => sp.span_type === 'model');
    const turns = modelSpans.length || Number(raw.turns || 0) || traces.length;
    const durationMs = Number(raw.duration_ms || 0) || traces.reduce((sum, t) => sum + Number(t.duration_ms || 0), 0);
    const inputTokens = Number(raw.input_tokens || 0) || traces.reduce((sum, t) => sum + Number(t.input_tokens || 0), 0);
    const outputTokens = Number(raw.output_tokens || 0) || traces.reduce((sum, t) => sum + Number(t.output_tokens || 0), 0);
    const startedAtMs = Number(raw.started_at_ms || 0) || (traces[0] ? Number(traces[0].started_at_ms || 0) : 0);
    const helper = window.AgentTraceEfficiency;

    const session = {
      id: raw.id || raw.session_id,
      session_id: raw.session_id || raw.id,
      artifact_id: raw.artifact_id || '',
      title: raw.title || (raw.session_id ? ('Session ' + raw.session_id) : 'Session'),
      user: raw.user || raw.user_id || 'anonymous',
      user_id: raw.user_id || raw.user || '',
      trace: raw.trace || (traces[0] ? traces[0].trace_id : ''),
      started_at_ms: startedAtMs,
      started_at: raw.started_at || (traces[0] ? traces[0].started_at : ''),
      duration_ms: durationMs,
      input_tokens: inputTokens,
      output_tokens: outputTokens,
      trace_count: Number(raw.trace_count || 0) || traces.length,
      turns,
      tool_calls: Number(raw.tool_calls || 0) || Number(raw.features?.tool_calls || 0),
      features: raw.features || {},
      rules: raw.rules || [],
      radar: raw.radar || { response: 0, stability: 0, thinking: 0, resource: 0, orchestration: 0 },
      traces,
      terminated_by: raw.terminated_by || '',
    };

    if (helper && session.radar) {
      session.score = helper.adjustedScore(session);
      session.color = helper.scoreColor(session.score);
    } else {
      session.score = Number(raw.score || 0);
      session.color = raw.color || scoreBand(session.score);
    }
    session.chip = raw.chip || chipText(session);
    return session;
  }

  async function loadSessions() {
    const payload = await fetchJSON('/api/session-bundles?limit=2000');
    return {
      sessions: (payload.data || []).map(normalizeSession),
      limit: payload.limit || 0,
      offset: payload.offset || 0,
      apiBase: resolvedBase,
    };
  }

  async function loadSession(sessionId) {
    const payload = await fetchJSON('/api/session-bundles/' + encodeURIComponent(sessionId));
    return normalizeSession(payload);
  }

  window.AgentTraceDB = {
    loadSessions,
    loadSession,
    getApiBase: function () { return resolvedBase || candidateBases()[0] || ''; },
    scoreBand,
  };
})();
