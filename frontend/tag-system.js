(function () {
  function safeJSON(v) {
    if (!v) return null;
    if (typeof v === 'object') return v;
    try { return JSON.parse(v); } catch { return null; }
  }

  function tag(label, opts) {
    const source = opts.source || 'rule';
    const sourceColor = source === 'llm' ? 'purple' : source === 'system' ? 'green' : 'blue';
    return {
      label,
      source,
      severity: opts.severity || 'warn',
      color: sourceColor,
      detail: opts.detail || '',
      priority: opts.priority || 50,
    };
  }

  function normalizeRuleTag(rule) {
    const raw = String(rule?.failed_label || rule?.name || '').trim();
    const detail = rule?.detail || '';
    if (!raw) return null;
    if (/trace\s*失败|调用未完成|根.*失败/i.test(raw)) return tag('trace 失败', { severity: 'bad', color: 'red', detail, priority: 5 });
    if (/长上下文|Token|上下文超限/.test(raw)) return tag('长上下文超限', { severity: 'warn', color: 'orange', detail, priority: 30 });
    if (/成本|费用|超支/.test(raw)) return tag('成本超支', { severity: 'bad', color: 'red', detail, priority: 18 });
    if (/MCP.*重试|重试风暴/.test(raw)) return tag('MCP 重试风暴', { severity: 'bad', color: 'red', detail, priority: 10 });
    if (/Skill.*重试|重试过高/.test(raw)) return tag('Skill 重试过高', { severity: 'warn', color: 'orange', detail, priority: 22 });
    if (/工具.*失败|失败工具|工具失败/.test(raw)) return tag('工具失败', { severity: 'bad', color: 'red', detail, priority: 12 });
    if (/死循环|循环/.test(raw)) return tag('行为死循环', { severity: 'bad', color: 'red', detail, priority: 14 });
    if (/关键路径|耗时|响应/.test(raw)) return tag('关键路径过长', { severity: 'warn', color: 'orange', detail, priority: 28 });
    if (/排队|等待资源/.test(raw)) return tag('排队过久', { severity: 'warn', color: 'orange', detail, priority: 32 });
    if (/串行|并行度/.test(raw)) return tag('串行瓶颈', { severity: 'warn', color: 'orange', detail, priority: 34 });
    if (/显存|OOM/.test(raw)) return tag('显存 OOM 重跑', { severity: 'bad', color: 'red', detail, priority: 16 });
    if (/过度思考|慢思考/.test(raw)) return tag('慢思考无效', { severity: 'warn', color: 'purple', detail, priority: 38 });
    if (/思考过简/.test(raw)) return tag('思考过简', { severity: 'warn', color: 'purple', detail, priority: 42 });
    if (/轨迹异常|执行效率|步数|轮数/.test(raw)) return tag('轨迹异常', { severity: 'warn', color: 'orange', detail, priority: 26 });
    return tag(raw, { severity: 'warn', color: 'orange', detail, priority: 60 });
  }

  function score(v) {
    const n = Number(v);
    return Number.isFinite(n) ? n : null;
  }

  function clampScore(v) {
    const n = score(v);
    return n === null ? null : Math.max(0, Math.min(100, Math.round(n)));
  }

  function firstScore(values) {
    for (const v of values) {
      const n = clampScore(v);
      if (n !== null) return n;
    }
    return null;
  }

  function pickLLMResult(session) {
    return safeJSON(
      session?.llm_judge_result ||
      session?.llm_judge ||
      session?.llmJudgeResult ||
      session?.gpt55_judge_result
    );
  }

  function getSessionQualityScores(session, fallbackRuleScore) {
    const llm = pickLLMResult(session);
    const ruleScore = firstScore([
      session?.rule_score,
      session?.rule_eval_score,
      session?.ruleEvalScore,
      fallbackRuleScore,
    ]);
    const llmScore = firstScore([
      session?.llm_score,
      session?.llm_judge_score,
      session?.llmJudgeScore,
      session?.gpt55_score,
      llm?.score,
      llm?.total_score,
      llm?.overall_score,
      llm?.quality_score,
    ]);
    const storedCombined = firstScore([
      session?.combined_score,
      session?.quality_score,
      session?.health_score,
    ]);
    const combinedScore = storedCombined !== null
      ? storedCombined
      : (llmScore !== null && ruleScore !== null
        ? Math.round(ruleScore * 0.5 + llmScore * 0.5)
        : ruleScore);
    const source = llmScore !== null ? 'combined' : 'rule';
    const basis = llmScore !== null
      ? `规则 ${ruleScore ?? '--'} × 50% + GPT-5.5 ${llmScore} × 50%`
      : `规则 ${ruleScore ?? '--'} × 100%；GPT-5.5 未评估`;
    return { ruleScore, llmScore, combinedScore, source, basis };
  }

  function collectRuleTagsFromRules(rules) {
    return (rules || [])
      .filter(r => r && r.passed === false)
      .map(normalizeRuleTag)
      .filter(Boolean);
  }

  function collectLLMTags(result) {
    const llm = safeJSON(result);
    if (!llm) return [];
    const out = [];
    const sentimentScore = score(llm.sentiment_score);
    const resolvedScore = score(llm.resolved_score);
    const intentScore = score(llm.intent_match_score);
    const repeatScore = score(llm.repeat_loop_score);
    const actionScore = score(llm.actionability_score);
    const hallucinationScore = score(llm.hallucination_risk_score);
    const reason = llm.reason || llm.score_basis || '';

    if (llm.resolved === '未解决' || (resolvedScore !== null && resolvedScore < 50)) out.push(tag('问题未解决', { source: 'llm', severity: 'bad', color: 'red', detail: reason, priority: 8 }));
    else if (llm.resolved === '部分解决' || (resolvedScore !== null && resolvedScore < 70)) out.push(tag('部分解决', { source: 'llm', severity: 'warn', color: 'orange', detail: reason, priority: 24 }));

    if (llm.intent_match === '明显答非所问' || (intentScore !== null && intentScore < 50)) out.push(tag('答非所问', { source: 'llm', severity: 'bad', color: 'red', detail: reason, priority: 9 }));
    else if (llm.intent_match === '部分偏离' || (intentScore !== null && intentScore < 70)) out.push(tag('意图偏离', { source: 'llm', severity: 'warn', color: 'orange', detail: reason, priority: 25 }));

    if (llm.sentiment === '强负向' || (sentimentScore !== null && sentimentScore < 35)) out.push(tag('用户强负向', { source: 'llm', severity: 'bad', color: 'red', detail: reason, priority: 11 }));
    else if (llm.sentiment === '负向' || (sentimentScore !== null && sentimentScore < 60)) out.push(tag('用户负向', { source: 'llm', severity: 'warn', color: 'orange', detail: reason, priority: 27 }));

    if (llm.repeat_loop === '循环卡住' || (repeatScore !== null && repeatScore < 35)) out.push(tag('循环卡住', { source: 'llm', severity: 'bad', color: 'red', detail: reason, priority: 13 }));
    else if (llm.repeat_loop === '明显重复' || (repeatScore !== null && repeatScore < 70)) out.push(tag('重复空转', { source: 'llm', severity: 'warn', color: 'orange', detail: reason, priority: 29 }));

    if (llm.actionability === '空泛不可执行' || (actionScore !== null && actionScore < 50)) out.push(tag('建议空泛', { source: 'llm', severity: 'bad', color: 'red', detail: reason, priority: 20 }));
    else if (llm.actionability === '需要补充信息' || (actionScore !== null && actionScore < 70)) out.push(tag('需补充信息', { source: 'llm', severity: 'warn', color: 'purple', detail: reason, priority: 36 }));

    if (llm.hallucination_risk === '高' || (hallucinationScore !== null && hallucinationScore < 40)) out.push(tag('幻觉风险高', { source: 'llm', severity: 'bad', color: 'red', detail: reason, priority: 7 }));
    else if (llm.hallucination_risk === '中' || (hallucinationScore !== null && hallucinationScore < 70)) out.push(tag('幻觉风险', { source: 'llm', severity: 'warn', color: 'orange', detail: reason, priority: 23 }));

    return out;
  }

  function collectLLMTagsFromSession(session) {
    const full = safeJSON(
      session?.llm_judge_result ||
      session?.llm_judge ||
      session?.llmJudgeResult ||
      session?.gpt55_judge_result
    );
    if (full) return collectLLMTags(full);
    if (firstScore([session?.llm_score, session?.llm_judge_score, session?.llmJudgeScore]) === null) return [];
    return collectLLMTags({
      score: session?.llm_score ?? session?.llm_judge_score ?? session?.llmJudgeScore,
      sentiment_score: session?.llm_sentiment_score,
      resolved_score: session?.llm_resolved_score,
      intent_match_score: session?.llm_intent_match_score,
      repeat_loop_score: session?.llm_repeat_loop_score,
      actionability_score: session?.llm_actionability_score,
      hallucination_risk_score: session?.llm_hallucination_risk_score,
    });
  }

  function mergeTags(tags, max) {
    const seen = new Map();
    (tags || []).forEach(t => {
      if (!t || !t.label) return;
      const prev = seen.get(t.label);
      if (!prev || (t.priority || 99) < (prev.priority || 99)) seen.set(t.label, t);
    });
    const merged = [...seen.values()].sort((a, b) => {
      const sev = { bad: 0, warn: 1, info: 2 };
      return (sev[a.severity] ?? 9) - (sev[b.severity] ?? 9) || (a.priority || 99) - (b.priority || 99);
    });
    return typeof max === 'number' ? merged.slice(0, max) : merged;
  }

  function buildSessionTags(session, options) {
    const max = options && options.max;
    const ruleTags = collectRuleTagsFromRules(session?.rules || []);
    const llmTags = collectLLMTagsFromSession(session);
    const merged = mergeTags(ruleTags.concat(llmTags), max);
    return merged.length ? merged : [tag('健康', { source: 'system', severity: 'info', color: 'green', priority: 999 })];
  }

  function dimensionOf(label) {
    if (/问题|解决|意图|答非所问|情绪|重复|空转|建议|信息|幻觉/.test(label)) return '语义体验';
    if (/上下文|Token|成本|显存|OOM/.test(label)) return '资源维度';
    if (/工具|MCP|Skill|失败|trace/.test(label)) return '稳定维度';
    if (/关键路径|耗时|排队|响应/.test(label)) return '响应维度';
    if (/轨迹|步数|轮数|死循环|串行|思考/.test(label)) return '编排维度';
    return '其他';
  }

  window.AgentTraceTags = {
    collectRuleTagsFromRules,
    collectLLMTags,
    collectLLMTagsFromSession,
    mergeTags,
    buildSessionTags,
    getSessionQualityScores,
    dimensionOf,
  };
})();
