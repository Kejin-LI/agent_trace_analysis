(function () {
  function safeJSON(s) {
    try { return JSON.parse(s || '{}'); } catch { return {}; }
  }

  function getAllSpans(data) {
    return (data?.traces || []).flatMap(t => t.spans || []);
  }

  function toolPath(sp) {
    const inp = safeJSON(sp?.input);
    return inp.filePath || inp.file_path || inp.path || inp.outputPath || inp.output_path || inp.target_path || '';
  }

  function hasExecutionFailure(rules) {
    return (rules || []).some(r => r && r.passed === false && (
      r.failed_label === '执行失败' ||
      r.name === '执行失败' ||
      r.failed_label === 'trace 失败'
    ));
  }

  function computeThinkingMetric(data) {
    const spans = getAllSpans(data);
    const modelSpans = spans.filter(sp => sp.span_type === 'model').sort((a, b) => (a.started_at_ms || 0) - (b.started_at_ms || 0));
    const hasFinalAnswer = modelSpans.some(sp => {
      try {
        const out = JSON.parse(sp.output || '{}');
        const choices = out.choices || out.response?.choices || [];
        return choices.some(c => c.message && c.message.content && c.message.content.trim().length > 0 && (!c.message.tool_calls || c.message.tool_calls.length === 0));
      } catch {
        return false;
      }
    });
    const truncatedByMax = data?.terminated_by === 'max_steps_limit';
    let noOpStreak = 0;
    let curStreak = 0;
    modelSpans.forEach(sp => {
      let hasToolCall = false;
      try {
        const out = JSON.parse(sp.output || '{}');
        const choices = out.choices || out.response?.choices || [];
        hasToolCall = choices.some(c => c.message && c.message.tool_calls && c.message.tool_calls.length > 0);
      } catch {}
      const hasContent = (sp.output || '').trim().length > 0;
      if (!hasToolCall && !hasContent) {
        curStreak++;
        noOpStreak = Math.max(noOpStreak, curStreak);
      } else {
        curStreak = 0;
      }
    });
    const turnsN = modelSpans.length || (data?.trace_count || data?.turns || 0);
    const score = Math.max(0, Math.min(100,
      100
      - (hasFinalAnswer ? 0 : 35)
      - (truncatedByMax ? 25 : 0)
      - Math.min(noOpStreak * 10, 30)
    ));
    return { score, hasFinalAnswer, truncatedByMax, noOpStreak, turnsN };
  }

  function computeEfficiencyMetric(data) {
    const f = data?.features || {};
    const spans = getAllSpans(data);
    const toolSpans = spans.filter(sp => sp.span_type === 'tool').sort((a, b) => (a.started_at_ms || 0) - (b.started_at_ms || 0));
    const modelSpans = spans.filter(sp => sp.span_type === 'model').sort((a, b) => (a.started_at_ms || 0) - (b.started_at_ms || 0));
    const toolCalls = Number(data?.tool_calls ?? f.tool_calls ?? toolSpans.length) || 0;
    const durSec = Math.max(0, Math.round((data?.duration_ms || 0) / 1000));
    const bucket = toolCalls < 3
      ? { full: 30, floor: 90, label: '简单任务' }
      : toolCalls < 10
        ? { full: 90, floor: 240, label: '中等任务' }
        : { full: 240, floor: 600, label: '复杂任务' };

    const rawExperience = durSec <= bucket.full
      ? 100
      : Math.max(30, 100 - ((durSec - bucket.full) / (bucket.floor - bucket.full)) * 70);
    const experienceScore = Math.round(hasExecutionFailure(data?.rules) ? Math.min(rawExperience, 50) : rawExperience);

    const hits = [];
    let maxSameToolArgs = Number(f.max_serial_run || 0);
    let prevKey = '', curRun = 0;
    toolSpans.forEach(sp => {
      const key = (sp.span_name || '') + '::' + (sp.input || '');
      curRun = key === prevKey ? curRun + 1 : 1;
      prevKey = key;
      maxSameToolArgs = Math.max(maxSameToolArgs, curRun);
    });
    if (maxSameToolArgs >= 3) hits.push({ key: 'env_dead_end', label: '环境黑洞', penalty: 20 });

    const editCounts = new Map();
    toolSpans.forEach(sp => {
      const name = sp.span_name || '';
      if (!/write|create|edit|rewrite|save|update|append|insert|replace|apply_patch/i.test(name)) return;
      const path = toolPath(sp);
      if (!path) return;
      editCounts.set(path, (editCounts.get(path) || 0) + 1);
    });
    if ([...editCounts.values()].some(v => v >= 3)) hits.push({ key: 'repetitive_edit', label: '重复编辑', penalty: 20 });

    const modelTexts = modelSpans.map(sp => String(sp.output || sp.input || ''));
    const maxThinkingLen = modelTexts.reduce((m, t) => Math.max(m, t.length), 0);
    const avgThinkingLen = modelTexts.length ? modelTexts.reduce((a, t) => a + t.length, 0) / modelTexts.length : 0;
    if (maxThinkingLen > 12000 || (modelTexts.length >= 3 && maxThinkingLen > avgThinkingLen * 3 && maxThinkingLen > 4000)) {
      hits.push({ key: 'thinking_redundancy', label: '思考冗余', penalty: 10 });
    }

    const firstThinking = modelTexts.slice(0, 3).join('\n');
    const hasPlan = /Plan:|计划|步骤[:：]|(?:^|\n)\s*1[.)、]|首先/.test(firstThinking);
    if (toolCalls >= 3 && !hasPlan) hits.push({ key: 'planning_behavior', label: '缺少规划', penalty: 10 });

    const planCount = (firstThinking.match(/(?:^|\n)\s*\d+[.)、]/g) || []).length;
    const actualSteps = modelSpans.length || (data?.trace_count || data?.turns || 0);
    if (planCount >= 2 && (actualSteps < planCount || actualSteps > planCount * 1.5)) {
      hits.push({ key: 'plan_adherence', label: '规划偏离', penalty: 10 });
    }

    const hasTempCreate = toolSpans.some(sp => /mkdir|touch|write|create/i.test(sp.span_name || '') && /tmp|temp|mock|debug|test/i.test((sp.input || '') + (sp.span_name || '')));
    const tailHasCleanup = toolSpans.slice(-3).some(sp => /rm|delete|cleanup|remove/i.test((sp.input || '') + (sp.span_name || '')));
    if (hasTempCreate && !tailHasCleanup) hits.push({ key: 'test_residues', label: '测试残留', penalty: 5 });

    const ruleEfficiencyScore = Math.max(0, 100 - hits.reduce((sum, h) => sum + h.penalty, 0));
    const score = Math.round(0.4 * experienceScore + 0.6 * ruleEfficiencyScore);
    return { score, experienceScore, ruleEfficiencyScore, hits, toolCalls, durSec, bucket };
  }

  function computeStabilityMetric(data) {
    const f = data?.features || {};
    const toolFailures = Number(f.tool_failures || 0);
    let failRate = Number(f.tool_fail_rate || 0);
    if (failRate > 1) failRate = failRate / 100;
    failRate = Math.max(0, Math.min(1, failRate));
    let score = Math.round((1 - failRate) * 100 - toolFailures * 5);
    if (hasExecutionFailure(data?.rules)) score = Math.min(score, 50);
    score = Math.max(0, Math.min(100, score));
    return { score, toolFailures, failRate };
  }

  function computeResourceMetric(data) {
    const avgTokens = Number(data?.features?.avg_tokens_per_turn || 0);
    let score = 100;
    if (avgTokens > 76000) {
      score = avgTokens >= 128000
        ? 60
        : 100 - ((avgTokens - 76000) / (128000 - 76000)) * 40;
    }
    score = Math.round(Math.max(0, Math.min(100, score)));
    return { score, avgTokens };
  }

  function computeOrchestrationMetric(data) {
    const f = data?.features || {};
    const maxSerialRun = Number(f.max_serial_run || 0);
    const toolCalls = Number(f.tool_calls || data?.tool_calls || 0);
    const uniqueTools = Number(f.unique_tools || 0);
    const diversity = toolCalls > 0 ? uniqueTools / toolCalls : 0;
    let score = 100;
    if (maxSerialRun >= 7) score -= 30;
    else if (maxSerialRun >= 5) score -= 20;
    else if (maxSerialRun >= 3) score -= 10;

    if (toolCalls > 0) {
      if (diversity >= 0.6) score += 20;
      else if (diversity >= 0.4) score += 10;
    }
    score = Math.round(Math.max(0, Math.min(100, score)));
    return { score, maxSerialRun, toolCalls, uniqueTools, diversity };
  }

  function adjustedRadar(sessionLike) {
    if (!sessionLike?.radar) return null;
    return Object.assign({}, sessionLike.radar, {
      response: computeEfficiencyMetric(sessionLike).score,
      stability: computeStabilityMetric(sessionLike).score,
      thinking: computeThinkingMetric(sessionLike).score,
      resource: computeResourceMetric(sessionLike).score,
      orchestration: computeOrchestrationMetric(sessionLike).score,
    });
  }

  function adjustedScore(sessionLike) {
    const r = adjustedRadar(sessionLike);
    if (!r) return sessionLike?.score ?? 0;
    const values = ['response', 'stability', 'thinking', 'resource', 'orchestration'].map(k => r[k] || 0);
    let score = values.reduce((sum, v) => sum + v, 0) / values.length;
    if (hasExecutionFailure(sessionLike?.rules)) score = Math.min(score, 50);
    return Math.round(score);
  }

  function scoreColor(score) {
    return score >= 85 ? 'green' : score >= 70 ? 'orange' : score >= 50 ? 'purple' : 'red';
  }

  window.AgentTraceEfficiency = {
    safeJSON,
    getAllSpans,
    toolPath,
    hasExecutionFailure,
    computeThinkingMetric,
    computeEfficiencyMetric,
    computeStabilityMetric,
    computeResourceMetric,
    computeOrchestrationMetric,
    adjustedRadar,
    adjustedScore,
    scoreColor,
  };
})();
