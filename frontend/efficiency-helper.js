(function () {
  function safeJSON(s) {
    try { return JSON.parse(s || '{}'); } catch { return {}; }
  }

  function getAllSpans(data) {
    return (data?.traces || []).flatMap(t => t.spans || []);
  }

  function getModelSpans(data) {
    return getAllSpans(data)
      .filter(sp => sp.span_type === 'model')
      .sort((a, b) => (a.started_at_ms || 0) - (b.started_at_ms || 0));
  }

  function getToolSpans(data) {
    return getAllSpans(data)
      .filter(sp => sp.span_type === 'tool')
      .sort((a, b) => (a.started_at_ms || 0) - (b.started_at_ms || 0));
  }

  function extractChoices(sp) {
    const out = safeJSON(sp?.output);
    return out.choices || out.response?.choices || [];
  }

  function modelVisibleText(sp) {
    const choice = extractChoices(sp)[0] || {};
    const msg = choice.message || {};
    const parts = [];
    if (typeof msg.reasoning === 'string') parts.push(msg.reasoning);
    if (typeof msg.reasoning_content === 'string') parts.push(msg.reasoning_content);
    if (typeof msg.content === 'string') parts.push(msg.content);
    else if (Array.isArray(msg.content)) parts.push(msg.content.map(p => typeof p === 'string' ? p : (p.text || '')).join('\n'));
    return parts.filter(Boolean).join('\n');
  }

  function toolPath(sp) {
    const inp = safeJSON(sp?.input);
    return inp.filePath || inp.file_path || inp.path || inp.outputPath || inp.output_path || inp.target_path || '';
  }

  // 取 span 对应的对话轮次号（用于命中论据可跳转）；拿不到返回 NaN。
  function spanRound(sp) {
    if (!sp) return NaN;
    const r = Number(sp.round_index);
    return Number.isFinite(r) && r > 0 ? r : NaN;
  }

  function baseName(p) {
    return String(p || '').split(/[\\/]/).filter(Boolean).pop() || String(p || '');
  }

  function hasExecutionFailure(rules) {
    return (rules || []).some(r => r && r.passed === false && (
      r.failed_label === '执行失败' ||
      r.name === '执行失败' ||
      r.failed_label === 'trace 失败'
    ));
  }

  function getTraceSignals(data) {
    const modelSpans = getModelSpans(data);
    const toolSpans = getToolSpans(data);
    const hasFinalAnswer = modelSpans.some(sp =>
      extractChoices(sp).some(c => c.message && c.message.content && c.message.content.trim().length > 0 && (!c.message.tool_calls || c.message.tool_calls.length === 0))
    );
    const truncatedByMax = data?.terminated_by === 'max_steps_limit';
    let noOpStreak = 0;
    let curStreak = 0;
    let toolIntentTurns = 0;
    let toolIntentCalls = 0;
    modelSpans.forEach(sp => {
      const choices = extractChoices(sp);
      const hasToolCallIntent = choices.some(c => c.message && Array.isArray(c.message.tool_calls) && c.message.tool_calls.length > 0);
      if (hasToolCallIntent) {
        toolIntentTurns++;
        choices.forEach(c => {
          if (c.message && Array.isArray(c.message.tool_calls)) {
            toolIntentCalls += c.message.tool_calls.length;
          }
        });
      }
      const hasContent = choices.some(c => c.message && c.message.content && c.message.content.trim().length > 0);
      if (!hasToolCallIntent && !hasContent) {
        curStreak++;
        noOpStreak = Math.max(noOpStreak, curStreak);
      } else {
        curStreak = 0;
      }
    });
    const realToolCalls = Number(data?.tool_calls ?? data?.features?.tool_calls ?? toolSpans.length) || 0;
    const hasToolIntentWithoutExecution = toolIntentCalls > 0 && realToolCalls === 0;
    return {
      modelSpans,
      toolSpans,
      hasFinalAnswer,
      truncatedByMax,
      noOpStreak,
      toolIntentTurns,
      toolIntentCalls,
      realToolCalls,
      hasToolIntentWithoutExecution,
      turnsN: modelSpans.length || (data?.trace_count || data?.turns || 0),
    };
  }

  function getDimensionApplicability(data) {
    const signals = getTraceSignals(data);
    return {
      response: true,
      thinking: true,
      resource: true,
      stability: signals.realToolCalls > 0,
      orchestration: signals.realToolCalls > 0,
      signals,
    };
  }

  // 循环类信号（串行重试 + 重复编辑），供「重复空转」维度复用
  function computeLoopSignals(data) {
    const f = data?.features || {};
    const toolSpans = getToolSpans(data);
    let maxSameToolArgs = Number(f.max_serial_run || 0);
    let prevKey = '', curRun = 0;
    let serialSpan = null, runStartSpan = null;
    toolSpans.forEach(sp => {
      const key = (sp.span_name || '') + '::' + (sp.input || '');
      if (key === prevKey) {
        curRun += 1;
      } else {
        curRun = 1;
        runStartSpan = sp;
      }
      prevKey = key;
      if (curRun > maxSameToolArgs) { maxSameToolArgs = curRun; serialSpan = runStartSpan; }
    });
    const editCounts = new Map();
    const editFirstSpan = new Map();
    const editSpans = new Map();
    toolSpans.forEach(sp => {
      const name = sp.span_name || '';
      if (!/write|create|edit|rewrite|save|update|append|insert|replace|apply_patch/i.test(name)) return;
      const path = toolPath(sp);
      if (!path) return;
      editCounts.set(path, (editCounts.get(path) || 0) + 1);
      if (!editFirstSpan.has(path)) editFirstSpan.set(path, sp);
      if (!editSpans.has(path)) editSpans.set(path, []);
      editSpans.get(path).push(sp);
    });
    let editPath = '', editCount = 0, editSpan = null;
    [...editCounts.entries()].forEach(([p, v]) => { if (v >= 3 && v > editCount) { editPath = p; editCount = v; editSpan = editFirstSpan.get(p); } });
    const hasRepetitiveEdit = editCount >= 3;
    return {
      maxSerialRun: maxSameToolArgs,
      serialTool: serialSpan ? (serialSpan.span_name || '') : '',
      serialRound: spanRound(serialSpan),
      serialSpanId: serialSpan ? (serialSpan.span_id || '') : '',
      hasRepetitiveEdit,
      editPath,
      editCount,
      editTool: editSpan ? (editSpan.span_name || '') : '',
      editRound: spanRound(editSpan),
      editSpanId: editSpan ? (editSpan.span_id || '') : '',
      editSpanIds: editPath ? (editSpans.get(editPath) || []).map(sp => sp.span_id || '').filter(Boolean) : [],
    };
  }

  // 重复空转：是否反复空转/循环（空转步数 + 串行重试 + 重复编辑）
  function computeThinkingMetric(data) {
    const signals = getTraceSignals(data);
    const loop = computeLoopSignals(data);
    const noOpPenalty = Math.min(signals.noOpStreak * 10, 30);
    const serialPenalty = loop.maxSerialRun >= 7 ? 30 : loop.maxSerialRun >= 5 ? 20 : loop.maxSerialRun >= 3 ? 10 : 0;
    const editPenalty = loop.hasRepetitiveEdit ? 20 : 0;
    const score = Math.max(0, Math.min(100, 100 - noOpPenalty - serialPenalty - editPenalty));
    return {
      score,
      hasFinalAnswer: signals.hasFinalAnswer,
      truncatedByMax: signals.truncatedByMax,
      noOpStreak: signals.noOpStreak,
      maxSerialRun: loop.maxSerialRun,
      hasRepetitiveEdit: loop.hasRepetitiveEdit,
      serialTool: loop.serialTool,
      serialRound: loop.serialRound,
      serialSpanId: loop.serialSpanId,
      editPath: loop.editPath,
      editCount: loop.editCount,
      editTool: loop.editTool,
      editRound: loop.editRound,
      editSpanId: loop.editSpanId,
      editSpanIds: loop.editSpanIds,
      turnsN: signals.turnsN,
      toolIntentTurns: signals.toolIntentTurns,
      toolIntentCalls: signals.toolIntentCalls,
      realToolCalls: signals.realToolCalls,
      hasToolIntentWithoutExecution: signals.hasToolIntentWithoutExecution,
    };
  }

  // 执行规整：执行模式是否规整完成（轨迹完成 + 规划 + 思考冗余等结构信号；不再评估耗时体感）
  function computeEfficiencyMetric(data) {
    const f = data?.features || {};
    const toolSpans = getToolSpans(data);
    const modelSpans = getModelSpans(data);
    const signals = getTraceSignals(data);
    const toolCalls = Number(data?.tool_calls ?? f.tool_calls ?? toolSpans.length) || 0;
    const durSec = Math.max(0, Math.round((data?.duration_ms || 0) / 1000));
    // 分桶仅用于耗时 KPI 的客观展示，不再参与维度打分
    const bucket = toolCalls < 3
      ? { full: 30, floor: 90, label: '简单任务' }
      : toolCalls < 10
        ? { full: 90, floor: 240, label: '中等任务' }
        : { full: 240, floor: 600, label: '复杂任务' };

    const hits = [];
    if (!signals.hasFinalAnswer) hits.push({ key: 'no_final_answer', label: '未产出最终答复', penalty: 35 });
    if (signals.truncatedByMax) hits.push({ key: 'truncated', label: '触达 max_steps 截断', penalty: 25 });
    if (signals.hasToolIntentWithoutExecution) hits.push({ key: 'tool_intent_no_exec', label: '工具意图未落地', penalty: 25 });

    const modelTexts = modelSpans.map(modelVisibleText);
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

    let score = Math.max(0, 100 - hits.reduce((sum, h) => sum + h.penalty, 0));
    if (hasExecutionFailure(data?.rules)) score = Math.min(score, 50);
    score = Math.round(Math.max(0, Math.min(100, score)));
    return { score, hits, toolCalls, durSec, bucket };
  }

  function computeStabilityMetric(data) {
    const applicability = getDimensionApplicability(data);
    if (!applicability.stability) {
      return { score: null, applicable: false, toolFailures: 0, failRate: 0 };
    }
    const f = data?.features || {};
    const toolFailures = Number(f.tool_failures || 0);
    let failRate = Number(f.tool_fail_rate || 0);
    if (failRate > 1) failRate = failRate / 100;
    failRate = Math.max(0, Math.min(1, failRate));
    let score = Math.round((1 - failRate) * 100 - toolFailures * 5);
    if (hasExecutionFailure(data?.rules)) score = Math.min(score, 50);
    score = Math.max(0, Math.min(100, score));
    const failSpan = getToolSpans(data).find(sp => sp.status_code != null && sp.status_code !== 0);
    return {
      score, applicable: true, toolFailures, failRate,
      failTool: failSpan ? (failSpan.span_name || '') : '',
      failRound: spanRound(failSpan),
      failSpanId: failSpan ? (failSpan.span_id || '') : '',
    };
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

  // 调度编排：工具调度是否高效（只看调度多样性；串行重试归「重复空转」维度）
  function computeOrchestrationMetric(data) {
    const applicability = getDimensionApplicability(data);
    if (!applicability.orchestration) {
      return { score: null, applicable: false, maxSerialRun: 0, toolCalls: 0, uniqueTools: 0, diversity: 0 };
    }
    const f = data?.features || {};
    const maxSerialRun = Number(f.max_serial_run || 0);
    const toolCalls = Number(f.tool_calls || data?.tool_calls || 0);
    const uniqueTools = Number(f.unique_tools || 0);
    const diversity = toolCalls > 0 ? uniqueTools / toolCalls : 0;
    let score;
    if (diversity >= 0.6) score = 100;
    else if (diversity >= 0.4) score = 85;
    else if (diversity >= 0.25) score = 70;
    else score = 50;
    score = Math.round(Math.max(0, Math.min(100, score)));
    return { score, applicable: true, maxSerialRun, toolCalls, uniqueTools, diversity };
  }

  function adjustedRadar(sessionLike) {
    if (!sessionLike?.radar) return null;
    const thinking = computeThinkingMetric(sessionLike);
    const stability = computeStabilityMetric(sessionLike);
    const resource = computeResourceMetric(sessionLike);
    const orchestration = computeOrchestrationMetric(sessionLike);
    return Object.assign({}, sessionLike.radar, {
      response: computeEfficiencyMetric(sessionLike).score,
      stability: stability.applicable ? stability.score : null,
      thinking: thinking.score,
      resource: resource.score,
      orchestration: orchestration.applicable ? orchestration.score : null,
    });
  }

  function adjustedScore(sessionLike) {
    const r = adjustedRadar(sessionLike);
    if (!r) return sessionLike?.score ?? 0;
    const values = ['response', 'stability', 'thinking', 'resource', 'orchestration']
      .map(k => r[k])
      .filter(v => typeof v === 'number' && !Number.isNaN(v));
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
    getModelSpans,
    getToolSpans,
    getTraceSignals,
    getDimensionApplicability,
    toolPath,
    baseName,
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
