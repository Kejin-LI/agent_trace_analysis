(function () {
  function uniq(arr) {
    return [...new Set(arr.filter(Boolean))];
  }

  function param(name) {
    return new URLSearchParams(window.location.search).get(name);
  }

  function candidateBases() {
    const manual = param('api_base') || param('apiBase');
    const saved = localStorage.getItem('agenttrace.apiBase');
    const origin = window.location.origin;
    const fallbacks = [];
    if (origin !== 'http://127.0.0.1:18080') fallbacks.push('http://127.0.0.1:18080');
    if (origin !== 'http://localhost:18080') fallbacks.push('http://localhost:18080');
    return uniq([manual, saved, origin, ...fallbacks]);
  }

  let resolvedBase = null;

  async function fetchJSON(path) {
    const errors = [];
    const bases = resolvedBase ? [resolvedBase] : candidateBases();
    for (const base of bases) {
      const url = base.replace(/\/$/, '') + path;
      try {
        const resp = await fetch(url, { cache: 'no-store' });
        if (!resp.ok) throw new Error('HTTP ' + resp.status + ' @ ' + url);
        const data = await resp.json();
        resolvedBase = base;
        localStorage.setItem('agenttrace.apiBase', base);
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
    }));
    return {
      trace_id: trace.trace_id,
      span_id: trace.span_id,
      title: trace.title || '',
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
