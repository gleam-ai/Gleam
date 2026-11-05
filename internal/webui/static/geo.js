let geoLoadSeq = 0;

async function loadGEO() {
  bindGEOControls();
  const seq = ++geoLoadSeq;
  try {
    const data = await api('GET', '/api/geo?n=50');
    if (seq !== geoLoadSeq) return;
    renderGEOStats(data.stats || {});
    const pr = $('#geo-principles');
    if (pr) pr.textContent = String(data.principles || '').trim();
    const cb = $('#geo-enabled');
    // Don't overwrite a checkbox the user is currently operating.
    if (cb && document.activeElement !== cb)
      cb.checked = data.enabled !== false;
    renderGEOList(data.records || []);
  } catch (err) {
    if (seq !== geoLoadSeq) return;
    const wrap = $('#geo-list');
    if (wrap) loadError(wrap, err, () => loadGEO());
  }
}

function renderGEOStats(stats) {
  const total = $('#geo-total');
  if (!total) return;
  total.textContent = stats.total || 0;
  $('#geo-avg').textContent = stats.avg_score
    ? Math.round(stats.avg_score)
    : '—';
  $('#geo-best').textContent = stats.best_score ? stats.best_score : '—';
}

function renderGEOList(records) {
  const wrap = $('#geo-list');
  if (!wrap) return;
  wrap.innerHTML = '';
  if (!records.length) {
    wrap.innerHTML =
      '<p class="empty-hint">还没有分析记录。用「内容创作者」角色写点东西，或在这里手动分析一段文字。</p>';
    return;
  }
  records.forEach((r) => wrap.appendChild(geoCard(r)));
}

function geoCard(r) {
  const item = el('div', 'geo-item');
  const score = Number(r.score) || 0;
  const cls =
    score >= 80
      ? 'geo-score--high'
      : score >= 60
        ? 'geo-score--mid'
        : 'geo-score--low';
  const time = r.created_at
    ? new Date(r.created_at).toLocaleString('zh-CN', {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    : '';
  const src = r.source === 'manual' ? '手动分析' : '创作自动分析';
  let html =
    '<div class="geo-item-head">' +
    '<span class="geo-score ' +
    cls +
    '">' +
    score +
    '</span>' +
    '<div class="geo-item-meta"><div class="geo-item-title">' +
    esc(r.goal || '未命名内容') +
    '</div>' +
    '<div class="geo-item-sub">' +
    esc(src) +
    (time ? ' · ' + esc(time) : '') +
    '</div></div></div>';
  if (r.summary) html += '<p class="geo-summary">' + esc(r.summary) + '</p>';
  html += geoPairs('已做好', r.strengths) + geoPairs('待优化', r.weaknesses);
  if (Array.isArray(r.actionables) && r.actionables.length) {
    html +=
      '<div class="geo-sub-title">可以这样改</div><ol class="geo-actions">' +
      r.actionables
        .map(
          (a) =>
            '<li><span class="geo-pri geo-pri--' +
            prioKey(a.priority) +
            '">' +
            prioLabel(a.priority) +
            '</span><span class="geo-cat">' +
            esc(a.category || '语义') +
            '</span>' +
            esc(a.description) +
            '</li>',
        )
        .join('') +
      '</ol>';
  }
  item.innerHTML = html;
  return item;
}

function geoPairs(title, list) {
  if (!Array.isArray(list) || !list.length) return '';
  return (
    '<div class="geo-sub-title">' +
    title +
    '</div><ul class="geo-pairs">' +
    list.map((v) => '<li>' + esc(v) + '</li>').join('') +
    '</ul>'
  );
}

function prioKey(p) {
  const k = String(p || '').toLowerCase();
  return k === 'high' ? 'high' : k === 'low' ? 'low' : 'mid';
}

function prioLabel(p) {
  return prioKey(p) === 'high' ? '高' : prioKey(p) === 'low' ? '低' : '中';
}

function bindGEOControls() {
  const btn = $('#geo-analyze');
  if (btn && !btn.dataset.bound) {
    btn.dataset.bound = '1';
    btn.addEventListener('click', runGEOAnalyze);
  }
  const cb = $('#geo-enabled');
  if (cb && !cb.dataset.bound) {
    cb.dataset.bound = '1';
    cb.addEventListener('change', async () => {
      const on = cb.checked;
      cb.disabled = true;
      try {
        await api('POST', '/api/settings', { agent: { geo_enabled: on } });
        toast(
          on ? '已开启：创作完成后自动给出 GEO 建议' : '已关闭创作自动分析',
          'success',
        );
      } catch (err) {
        cb.checked = !on;
        toast('设置保存失败：' + err.message, 'error');
      } finally {
        cb.disabled = false;
      }
    });
  }
  const clear = $('#geo-clear');
  if (clear && !clear.dataset.bound) {
    clear.dataset.bound = '1';
    clear.addEventListener('click', async () => {
      const yes = await confirmModal(
        '所有 GEO 分析记录会被永久删除，无法恢复。',
        '清空 GEO 历史？',
        { okText: '清空', danger: true },
      );
      if (!yes) return;
      clear.disabled = true;
      try {
        await api('DELETE', '/api/geo/history');
        renderGEOList([]);
        renderGEOStats({});
        toast('已清空 GEO 历史', 'success');
      } catch (err) {
        toast('清空失败：' + err.message, 'error');
      } finally {
        clear.disabled = false;
      }
    });
  }
}

async function runGEOAnalyze() {
  const content = ($('#geo-content').value || '').trim();
  const goal = ($('#geo-goal').value || '').trim();
  const btn = $('#geo-analyze');
  const hint = $('#geo-analyze-hint');
  if (content.length < 10) {
    toast('请至少输入 10 个字再分析', 'warning');
    return;
  }
  btn.disabled = true;
  const controller = new AbortController();
  let elapsed = 0;
  const tick = setInterval(() => {
    elapsed += 1;
    hint.textContent = `分析中… 已用时 ${elapsed}s（最长约 90s）`;
  }, 1000);
  const guard = setTimeout(() => controller.abort(), 100000);
  hint.textContent = '分析中…';
  try {
    const data = await api(
      'POST',
      '/api/geo/analyze',
      { content, goal },
      { signal: controller.signal },
    );
    const box = $('#geo-result');
    box.hidden = false;
    box.replaceChildren(geoCard(data.record || data.suggestion || {}));
    hint.textContent = '分析完成，已存入历史';
    await loadGEO();
  } catch (err) {
    const timedOut = controller.signal.aborted;
    hint.textContent = timedOut ? '分析超时' : '分析失败';
    toast(
      timedOut ? '分析超时，请稍后重试' : '分析失败：' + err.message,
      'error',
    );
  } finally {
    clearInterval(tick);
    clearTimeout(guard);
    btn.disabled = false;
  }
}
