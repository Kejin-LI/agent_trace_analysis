(function () {
  function uniq(arr) {
    return [...new Set(arr.filter(Boolean))];
  }

  function param(name) {
    return new URLSearchParams(window.location.search).get(name);
  }

  function safeJSON(v) {
    if (!v || typeof v !== 'string') return null;
    try { return JSON.parse(v); } catch { return null; }
  }

  function normalizeCustomTags(tags) {
    if (!tags) return {};
    if (typeof tags === 'string') return safeJSON(tags) || {};
    if (typeof tags === 'object') return tags;
    return {};
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

  // 基础 path：取当前页面所在目录，用于把 "/api/..." 拼到正确的网关前缀下
  // （例如在 /trace_sever/sessions 页面下基础 path 为 "/trace_sever/"）。
  // 让浏览器原生处理，避免硬编码网关名。
  function basePath() {
    const p = window.location.pathname;
    const idx = p.lastIndexOf('/');
    return idx >= 0 ? p.slice(0, idx + 1) : '/';
  }

  let resolvedBase = null;

  async function fetchJSON(path) {
    const errors = [];
    const bases = resolvedBase ? [resolvedBase] : candidateBases();
    // 把 "/api/x" 转成 "<basePath>api/x"，复用浏览器原生 URL 解析。
    const finalPath = path.startsWith('/') ? basePath() + path.slice(1) : path;
    for (const base of bases) {
      const url = base.replace(/\/$/, '') + finalPath;
      try {
        // same-origin 请求需要带 cookie 才能透传到上游 SSO。
        const resp = await fetch(url, { cache: 'no-store', credentials: 'same-origin' });
        if (!resp.ok) throw new Error('HTTP ' + resp.status + ' @ ' + url);
        resolvedBase = base;
        if (isLocalEnv()) localStorage.setItem('agenttrace.apiBase', base);
        // 204 No Content 或空 body：视为"暂无数据"，返回 null 而非解析空 body 报错。
        if (resp.status === 204) return null;
        const text = await resp.text();
        if (!text) return null;
        return JSON.parse(text);
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

  function stripQuestionAnswerResultPayload(text) {
    const raw = String(text || '');
    const marker = '"type":"question_answer_result"';
    let idx = raw.indexOf(marker);
    if (idx < 0) idx = raw.indexOf('"type": "question_answer_result"');
    if (idx < 0) return raw;
    const start = raw.lastIndexOf('{', idx);
    if (start < 0) return raw;
    let depth = 0, inStr = false, esc = false;
    for (let i = start; i < raw.length; i++) {
      const ch = raw[i];
      if (inStr) {
        if (esc) esc = false;
        else if (ch === '\\') esc = true;
        else if (ch === '"') inStr = false;
        continue;
      }
      if (ch === '"') inStr = true;
      else if (ch === '{') depth++;
      else if (ch === '}') {
        depth--;
        if (depth === 0) {
          try {
            const payload = JSON.parse(raw.slice(start, i + 1));
            if (payload && payload.type === 'question_answer_result') {
              return `${raw.slice(0, start)} ${raw.slice(i + 1)}`.replace(/\s+/g, ' ').trim();
            }
          } catch {}
          return raw;
        }
      }
    }
    return raw;
  }

  function parseOptionalScore(value) {
    if (value === null || value === undefined || value === '') return null;
    const num = Number(value);
    if (!Number.isFinite(num)) return null;
    return Math.max(0, Math.min(100, Math.round(num)));
  }

  function stripSyntheticPromptPayload(text) {
    const raw = stripBusinessWrappedPrompt(stripQuestionAnswerResultPayload(text));
    const lower = raw.toLowerCase();
    if (lower.includes('/root/neeko-workspace/delivery/')
      && lower.includes('process only entries with sheetrow')
      && lower.includes('return only json')) {
      return '';
    }
    if (lower.startsWith('continue if you have next steps')
      && lower.includes('stop and ask for clarification')
      && lower.includes('unsure how to proceed')) {
      return '';
    }
    return raw;
  }

  function stripBusinessWrappedPrompt(text) {
    const raw = String(text || '').trim();
    if (!raw) return '';
    const idx = raw.indexOf('用户原始查询');
    if (idx < 0) return raw;
    let segment = raw.slice(idx + '用户原始查询'.length).trim();
    segment = segment.replace(/^[:：\s]+/, '');
    if (!segment) return raw;
    let cut = segment.length;
    [
      ' batch_id:',
      ' batch_id：',
      '\nbatch_id:',
      '\nbatch_id：',
      ' 候选列表:',
      ' 候选列表：',
      ' 专家候选列表:',
      ' 专家候选列表：',
      '\n候选列表:',
      '\n候选列表：',
      '\n专家候选列表:',
      '\n专家候选列表：'
    ].forEach(marker => {
      const pos = segment.indexOf(marker);
      if (pos >= 0 && pos < cut) cut = pos;
    });
    segment = segment.slice(0, cut).trim();
    return segment || raw;
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
      custom_tags: normalizeCustomTags(sp.custom_tags || sp.customTags),
      input_tokens: Number(sp.input_tokens || sp.inputTokens || sp.prompt_tokens || sp.promptTokens || 0),
      output_tokens: Number(sp.output_tokens || sp.outputTokens || sp.completion_tokens || sp.completionTokens || 0),
      total_tokens: Number(sp.total_tokens || sp.totalTokens || 0),
      user_prompt: stripSyntheticPromptPayload(sp.user_prompt || sp.userPrompt || ''),
      prompt_source: sp.prompt_source || sp.promptSource || '',
      round_index: Number(sp.round_index || sp.roundIndex || 0),
    }));
    return {
      trace_id: trace.trace_id,
      span_id: trace.span_id,
      title: stripSyntheticPromptPayload(trace.title || ''),
      user_prompt: stripSyntheticPromptPayload(trace.user_prompt || trace.userPrompt || ''),
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

  function normalizeArtifactPublicationStatus(value) {
    const status = String(value || '').trim().toLowerCase();
    if (status === 'published' || status === 'unpublished') return status;
    return '';
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
    const llmJudgeResult = raw.llm_judge_result || raw.llmJudgeResult || raw.llm_judge || raw.gpt55_judge_result || null;
    const persistedScore = parseOptionalScore(raw.score);

    const session = {
      id: raw.id || raw.session_id,
      session_id: raw.session_id || raw.id,
      artifact_id: raw.artifact_id || '',
      artifact_publication_status: normalizeArtifactPublicationStatus(raw.artifact_publication_status),
      title: stripSyntheticPromptPayload(raw.title) || (raw.session_id ? ('Session ' + raw.session_id) : 'Session'),
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
      rule_score: raw.rule_score ?? raw.ruleScore ?? null,
      llm_score: raw.llm_score ?? raw.llmScore ?? raw.llm_judge_score ?? raw.llmJudgeScore ?? null,
      llm_judge_score: raw.llm_judge_score ?? raw.llmJudgeScore ?? raw.llm_score ?? raw.llmScore ?? null,
      llm_judge_model: raw.llm_judge_model || raw.llmJudgeModel || '',
      llm_eval_status: raw.llm_eval_status || raw.llmEvalStatus || '',
      llm_eval_version: raw.llm_eval_version || raw.llmEvalVersion || 0,
      llm_evaluated_at: raw.llm_evaluated_at || raw.llmEvaluatedAt || '',
      llm_sentiment_score: raw.llm_sentiment_score ?? raw.llmSentimentScore ?? null,
      llm_resolved_score: raw.llm_resolved_score ?? raw.llmResolvedScore ?? null,
      llm_intent_match_score: raw.llm_intent_match_score ?? raw.llmIntentMatchScore ?? null,
      llm_efficiency_feel_score: raw.llm_efficiency_feel_score ?? raw.llmEfficiencyFeelScore ?? null,
      llm_actionability_score: raw.llm_actionability_score ?? raw.llmActionabilityScore ?? null,
      llm_hallucination_risk_score: raw.llm_hallucination_risk_score ?? raw.llmHallucinationRiskScore ?? null,
      llm_judge_result: typeof llmJudgeResult === 'string' ? (safeJSON(llmJudgeResult) || null) : llmJudgeResult,
      combined_score: raw.combined_score ?? raw.combinedScore ?? null,
    };

    if (persistedScore !== null) {
      session.score = persistedScore;
      session.color = raw.color || scoreBand(session.score);
      if (helper && session.radar) session.derived_rule_score = helper.adjustedScore(session);
    } else if (helper && session.radar) {
      session.score = helper.adjustedScore(session);
      session.color = helper.scoreColor(session.score);
    } else {
      session.score = parseOptionalScore(raw.score) ?? 0;
      session.color = raw.color || scoreBand(session.score);
    }
    session.chip = raw.chip || chipText(session);
    return session;
  }

  async function loadSessions(opts) {
    const params = new URLSearchParams({ limit: '2000' });
    if (opts && opts.startTime) params.set('start_time', opts.startTime);
    if (opts && opts.endTime) params.set('end_time', opts.endTime);
    if (opts && opts.artifactStatus) params.set('artifact_status', opts.artifactStatus);
    const payload = await fetchJSON('/api/session-bundles?' + params.toString());
    const sessions = (payload?.data || []).map(normalizeSession);
    return {
      sessions,
      total: Number(payload?.total || 0) || sessions.length,
      limit: payload?.limit || 0,
      offset: payload?.offset || 0,
      apiBase: resolvedBase,
    };
  }

  async function loadDashboardSummary(opts) {
    const params = new URLSearchParams();
    if (opts && opts.startTime) params.set('start_time', opts.startTime);
    if (opts && opts.endTime) params.set('end_time', opts.endTime);
    if (opts && opts.artifactStatus) params.set('artifact_status', opts.artifactStatus);
    return await fetchJSON('/api/dashboard-summary' + (params.toString() ? '?' + params.toString() : ''));
  }

  async function loadSession(sessionId, opts) {
    const metaOnly = opts && opts.metaOnly;
    const params = new URLSearchParams();
    if (metaOnly) params.set('meta_only', '1');
    if (opts && opts.artifactStatus) params.set('artifact_status', opts.artifactStatus);
    const qs = params.toString();
    const url = '/api/session-bundles/' + encodeURIComponent(sessionId) + (qs ? '?' + qs : '');
    const payload = await fetchJSON(url);
    if (!payload) return null; // 204：DB 暂无缓存，等完整加载
    return normalizeSession(payload);
  }

  window.AgentTraceDB = {
    loadSessions,
    loadDashboardSummary,
    loadSession,
    getApiBase: function () { return resolvedBase || candidateBases()[0] || ''; },
    // buildUrl 把 "/api/x" 解析成与 fetchJSON 完全一致的最终 URL（含网关前缀），
    // 供 fetch/EventSource 等直接复用，避免本地与生产环境路径不一致。
    buildUrl: function (path) {
      const base = (resolvedBase || candidateBases()[0] || '').replace(/\/$/, '');
      const finalPath = path.startsWith('/') ? basePath() + path.slice(1) : path;
      return base + finalPath;
    },
    scoreBand,
  };
})();
