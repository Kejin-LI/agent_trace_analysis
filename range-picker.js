/* ===== 共享日期范围选择器（苹果风迷你日历 + 时间精选） =====
 * 用法：
 *   window.mountRangePicker({
 *     container: HTMLElement,                    // 必填，宿主容器
 *     onChange: (range, customFrom?, customTo?) => {} // 可选，切换/应用时回调
 *   });
 *
 * localStorage:
 *   agenttrace.range:       '24h' | '7d' | '30d' | 'custom'
 *   agenttrace.customFrom:  ms (number)
 *   agenttrace.customTo:    ms (number)
 *
 * 也提供：window.getCurrentRange() => { range, customFrom, customTo }
 */
(function () {
  'use strict';

  const STORAGE_KEY = 'agenttrace.range';
  const STORAGE_FROM = 'agenttrace.customFrom';
  const STORAGE_TO = 'agenttrace.customTo';

  const pad = n => String(n).padStart(2, '0');

  // 注入一次性样式（仅插入未存在的 .seg 等基础样式，避免重复定义）
  function injectStylesOnce() {
    if (document.getElementById('range-picker-styles')) return;
    const css = document.createElement('style');
    css.id = 'range-picker-styles';
    css.textContent = `
      /* 共享 segment 按钮（如页面已有同名 .seg 样式可覆盖，此处兜底） */
      .rp-seg { background: rgba(120,120,128,0.12); border-radius: 9px; padding: 2px; display: inline-flex; }
      .rp-seg button { padding: 4px 12px; font-size: 12px; border-radius: 7px; color: var(--text-secondary); }
      .rp-seg button.active { background: white; color: var(--text-primary); box-shadow: 0 1px 2px rgba(0,0,0,0.06); }
    `;
    document.head.appendChild(css);
  }

  function fmtCustomLabel(from, to) {
    const f = (d) => `${pad(d.getMonth()+1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
    return `${f(from)} – ${f(to)}`;
  }

  /**
   * 主挂载函数
   */
  function mountRangePicker(opts) {
    const { container, onChange } = opts || {};
    if (!container) throw new Error('mountRangePicker: container is required');
    injectStylesOnce();

    // 容器需要 position: relative 以便 popover 定位
    if (getComputedStyle(container).position === 'static') {
      container.style.position = 'relative';
    }

    // 读取初始状态
    let curRange = localStorage.getItem(STORAGE_KEY) || '7d';
    let customFromMs = parseInt(localStorage.getItem(STORAGE_FROM) || '0', 10) || null;
    let customToMs = parseInt(localStorage.getItem(STORAGE_TO) || '0', 10) || null;

    // ===== 渲染骨架（按钮组 + popover） =====
    container.innerHTML = `
      <div class="seg rp-seg" data-rp-seg>
        <button data-range="24h">24h</button>
        <button data-range="7d">7d</button>
        <button data-range="30d">30d</button>
        <button data-range="custom" data-rp-custom-btn title="自定义日历区间">
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/></svg>
        </button>
      </div>
      <div data-rp-popover class="hidden absolute right-0 top-10 w-[360px] rounded-2xl p-4 z-50 text-[12px]"
           style="background: rgba(255,255,255,0.96); backdrop-filter: blur(30px) saturate(200%); -webkit-backdrop-filter: blur(30px) saturate(200%); border: 1px solid rgba(0,0,0,0.08); box-shadow: 0 20px 50px rgba(0,0,0,0.18);">
        <div class="flex items-center justify-between mb-3">
          <div class="text-[13px] font-semibold">自定义区间</div>
          <div class="text-[11px] text-[--text-tertiary]">按 session 首次提问时间</div>
        </div>

        <!-- Tabs：开始 / 结束 -->
        <div class="seg rp-seg w-full mb-3" data-rp-tabs>
          <button class="active flex-1" data-side="from">开始 · <span data-rp-from-label class="num-mono ml-1 text-[--text-secondary]">未选</span></button>
          <button class="flex-1" data-side="to">结束 · <span data-rp-to-label class="num-mono ml-1 text-[--text-secondary]">未选</span></button>
        </div>

        <!-- 日历头 -->
        <div class="flex items-center justify-between mb-2 px-1">
          <button data-rp-prev class="w-7 h-7 rounded-lg hover:bg-black/5 flex items-center justify-center">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7"/></svg>
          </button>
          <div data-rp-month class="text-[13px] font-semibold num-mono">—</div>
          <button data-rp-next class="w-7 h-7 rounded-lg hover:bg-black/5 flex items-center justify-center">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7"/></svg>
          </button>
        </div>

        <!-- 星期表头 -->
        <div class="grid grid-cols-7 gap-0 text-[10px] text-[--text-tertiary] mb-1 text-center">
          <div>日</div><div>一</div><div>二</div><div>三</div><div>四</div><div>五</div><div>六</div>
        </div>
        <!-- 日期网格 -->
        <div data-rp-grid class="grid grid-cols-7 gap-0.5 text-[12px]"></div>

        <!-- 时间精选 -->
        <div class="mt-3 pt-3 border-t border-black/5 flex items-center gap-2">
          <span class="text-[11px] text-[--text-tertiary]">时间</span>
          <input data-rp-hour type="number" min="0" max="23" value="00" class="w-12 bg-black/5 rounded-md px-2 py-1 num-mono text-center focus:outline-none focus:ring-2 focus:ring-[#0071E3]/30" />
          <span class="text-[--text-tertiary]">:</span>
          <input data-rp-min type="number" min="0" max="59" value="00" class="w-12 bg-black/5 rounded-md px-2 py-1 num-mono text-center focus:outline-none focus:ring-2 focus:ring-[#0071E3]/30" />
          <span class="text-[--text-tertiary]">:</span>
          <input data-rp-sec type="number" min="0" max="59" value="00" class="w-12 bg-black/5 rounded-md px-2 py-1 num-mono text-center focus:outline-none focus:ring-2 focus:ring-[#0071E3]/30" />
          <button data-rp-now class="ml-auto text-[11px] text-[--apple-blue] hover:underline">此刻</button>
        </div>

        <!-- 操作按钮 -->
        <div class="flex gap-2 mt-3">
          <button data-rp-clear class="flex-1 py-2 rounded-lg bg-black/5 hover:bg-black/10 text-[12px]">清空</button>
          <button data-rp-apply class="flex-1 py-2 rounded-lg text-white bg-[--apple-blue] hover:bg-[#0058B7] text-[12px] font-medium">应用</button>
        </div>
      </div>
    `;

    const $ = (sel) => container.querySelector(sel);
    const $$ = (sel) => container.querySelectorAll(sel);
    const seg = $('[data-rp-seg]');
    const pop = $('[data-rp-popover]');
    const customBtn = $('[data-rp-custom-btn]');
    const monthEl = $('[data-rp-month]');
    const gridEl = $('[data-rp-grid]');
    const fromLabel = $('[data-rp-from-label]');
    const toLabel = $('[data-rp-to-label]');
    const hourEl = $('[data-rp-hour]');
    const minEl = $('[data-rp-min]');
    const secEl = $('[data-rp-sec]');

    // ===== 状态 =====
    const today = new Date();
    let viewYear = today.getFullYear();
    let viewMonth = today.getMonth();
    let activeSide = 'from';
    const picked = {
      from: customFromMs ? new Date(customFromMs) : null,
      to: customToMs ? new Date(customToMs) : null,
    };

    // 初始化激活按钮
    const setSegActive = (range) => {
      $$('[data-rp-seg] button').forEach(b => b.classList.toggle('active', b.dataset.range === range));
    };
    setSegActive(curRange);

    if (picked.from) fromLabel.textContent = `${pad(picked.from.getMonth()+1)}/${pad(picked.from.getDate())} ${pad(picked.from.getHours())}:${pad(picked.from.getMinutes())}`;
    if (picked.to) toLabel.textContent = `${pad(picked.to.getMonth()+1)}/${pad(picked.to.getDate())} ${pad(picked.to.getHours())}:${pad(picked.to.getMinutes())}`;

    const sameDay = (a, b) => a && b && a.getFullYear()===b.getFullYear() && a.getMonth()===b.getMonth() && a.getDate()===b.getDate();
    const inRange = (d) => picked.from && picked.to && d > picked.from && d < picked.to;

    // ===== 渲染日历 =====
    const renderGrid = () => {
      monthEl.textContent = `${viewYear} 年 ${viewMonth + 1} 月`;
      const first = new Date(viewYear, viewMonth, 1);
      const startWeekday = first.getDay();
      const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
      const prevMonthDays = new Date(viewYear, viewMonth, 0).getDate();
      gridEl.innerHTML = '';

      const cells = [];
      // 上月尾
      for (let i = startWeekday - 1; i >= 0; i--) {
        cells.push({ y: viewYear, m: viewMonth - 1, d: prevMonthDays - i, muted: true });
      }
      // 本月
      for (let d = 1; d <= daysInMonth; d++) {
        cells.push({ y: viewYear, m: viewMonth, d, muted: false });
      }
      // 下月头补满 42 格
      while (cells.length < 42) {
        const last = cells[cells.length - 1];
        const nd = new Date(last.y, last.m, last.d + 1);
        cells.push({ y: nd.getFullYear(), m: nd.getMonth(), d: nd.getDate(), muted: nd.getMonth() !== viewMonth });
      }

      cells.forEach(c => {
        const date = new Date(c.y, c.m, c.d);
        const isToday = sameDay(date, today);
        const isFrom = sameDay(date, picked.from);
        const isTo = sameDay(date, picked.to);
        const isInside = inRange(date);

        const btn = document.createElement('button');
        btn.className = 'h-8 rounded-lg flex items-center justify-center num-mono transition-colors';
        if (c.muted) btn.style.color = 'var(--text-tertiary)';
        if (isFrom || isTo) {
          btn.style.background = 'var(--apple-blue)';
          btn.style.color = 'white';
          btn.style.fontWeight = '600';
        } else if (isInside) {
          btn.style.background = 'rgba(0,113,227,0.12)';
          btn.style.color = 'var(--apple-blue)';
        } else if (isToday) {
          btn.style.boxShadow = 'inset 0 0 0 1.5px var(--apple-blue)';
          btn.style.color = 'var(--apple-blue)';
          btn.style.fontWeight = '600';
        } else {
          btn.onmouseenter = () => { if (!c.muted) btn.style.background = 'rgba(0,0,0,0.05)'; };
          btn.onmouseleave = () => { btn.style.background = ''; };
        }
        btn.textContent = c.d;
        btn.addEventListener('click', (e) => {
          e.stopPropagation(); // 防止冒泡到 document 触发"点击外部关闭"
          const h = parseInt(hourEl.value || 0, 10);
          const mi = parseInt(minEl.value || 0, 10);
          const s = parseInt(secEl.value || 0, 10);
          const picked_dt = new Date(c.y, c.m, c.d, h, mi, s);
          picked[activeSide] = picked_dt;
          // 自动校正：from > to 时互换
          if (picked.from && picked.to && picked.from > picked.to) {
            const t = picked.from; picked.from = picked.to; picked.to = t;
          }
          fromLabel.textContent = picked.from ? `${pad(picked.from.getMonth()+1)}/${pad(picked.from.getDate())} ${pad(picked.from.getHours())}:${pad(picked.from.getMinutes())}` : '未选';
          toLabel.textContent = picked.to ? `${pad(picked.to.getMonth()+1)}/${pad(picked.to.getDate())} ${pad(picked.to.getHours())}:${pad(picked.to.getMinutes())}` : '未选';
          // 自动跳到下一个 tab
          if (activeSide === 'from' && !picked.to) switchSide('to');
          renderGrid();
        });
        gridEl.appendChild(btn);
      });
    };

    const switchSide = (side) => {
      activeSide = side;
      $$('[data-rp-tabs] button').forEach(b => b.classList.toggle('active', b.dataset.side === side));
      const cur = picked[side];
      if (cur) {
        hourEl.value = pad(cur.getHours());
        minEl.value = pad(cur.getMinutes());
        secEl.value = pad(cur.getSeconds());
      }
    };

    // ===== 事件绑定 =====

    // 快捷档位按钮
    $$('[data-rp-seg] button[data-range]').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const r = btn.dataset.range;
        if (r === 'custom') {
          pop.classList.toggle('hidden');
          return;
        }
        setSegActive(r);
        pop.classList.add('hidden');
        curRange = r;
        localStorage.setItem(STORAGE_KEY, r);
        if (typeof onChange === 'function') onChange(r);
      });
    });

    // tab 切换
    $$('[data-rp-tabs] button').forEach(b => {
      b.addEventListener('click', () => switchSide(b.dataset.side));
    });

    // 月份导航
    $('[data-rp-prev]').addEventListener('click', () => {
      viewMonth--; if (viewMonth < 0) { viewMonth = 11; viewYear--; }
      renderGrid();
    });
    $('[data-rp-next]').addEventListener('click', () => {
      viewMonth++; if (viewMonth > 11) { viewMonth = 0; viewYear++; }
      renderGrid();
    });
    // 此刻
    $('[data-rp-now]').addEventListener('click', () => {
      const n = new Date();
      hourEl.value = pad(n.getHours());
      minEl.value = pad(n.getMinutes());
      secEl.value = pad(n.getSeconds());
    });

    // 应用
    $('[data-rp-apply]').addEventListener('click', () => {
      if (!picked.from || !picked.to) return;
      setSegActive('custom');
      pop.classList.add('hidden');
      curRange = 'custom';
      customFromMs = picked.from.getTime();
      customToMs = picked.to.getTime();
      localStorage.setItem(STORAGE_KEY, 'custom');
      localStorage.setItem(STORAGE_FROM, String(customFromMs));
      localStorage.setItem(STORAGE_TO, String(customToMs));
      if (typeof onChange === 'function') onChange('custom', customFromMs, customToMs);
    });

    // 清空
    $('[data-rp-clear]').addEventListener('click', () => {
      picked.from = null; picked.to = null;
      fromLabel.textContent = '未选';
      toLabel.textContent = '未选';
      hourEl.value = '00'; minEl.value = '00'; secEl.value = '00';
      renderGrid();
    });

    // 内部点击不冒泡（防止 document 监听器关闭浮层）
    pop.addEventListener('click', (e) => e.stopPropagation());
    // 点击外部关闭
    document.addEventListener('click', (e) => {
      if (!pop.contains(e.target) && !seg.contains(e.target)) {
        pop.classList.add('hidden');
      }
    });

    // 初始化日历
    renderGrid();

    // 首次广播一次（让宿主页面立即同步过滤数据）
    if (typeof onChange === 'function') {
      if (curRange === 'custom' && customFromMs && customToMs) {
        onChange('custom', customFromMs, customToMs);
      } else {
        onChange(curRange);
      }
    }
  }

  // 工具：读取当前状态
  function getCurrentRange() {
    return {
      range: localStorage.getItem(STORAGE_KEY) || '7d',
      customFrom: parseInt(localStorage.getItem(STORAGE_FROM) || '0', 10) || null,
      customTo: parseInt(localStorage.getItem(STORAGE_TO) || '0', 10) || null,
    };
  }

  window.mountRangePicker = mountRangePicker;
  window.getCurrentRange = getCurrentRange;
})();
