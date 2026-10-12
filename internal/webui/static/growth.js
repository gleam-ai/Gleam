async function loadGrowth() {
  await Promise.all([loadGrowthStats(), loadGrowthTimeline()]);
}

async function loadGrowthStats() {
  try {
    const data = await api('GET', '/api/growth');
    const stats = data.stats;
    if (!stats) {
      document.getElementById('gs-level').textContent = '萌新微光';
      return;
    }
    document.getElementById('gs-level').textContent = stats.level || '萌新微光';
    document.getElementById('gs-tasks').textContent = stats.total_tasks || 0;
    document.getElementById('gs-skills').textContent = stats.total_skills || 0;
    document.getElementById('gs-avg').textContent = stats.avg_score
      ? Math.round(stats.avg_score)
      : '—';
    document.getElementById('gs-streak').textContent = stats.recent_streak || 0;
    const dur = stats.total_duration_seconds || 0;
    document.getElementById('gs-duration').textContent =
      dur > 0 ? formatDuration(dur) : '—';
    const tok = stats.total_tokens || 0;
    document.getElementById('gs-tokens').textContent =
      tok > 0 ? formatTokens(tok) : '—';
    document.getElementById('gs-week-tokens').textContent = stats.week_tokens
      ? formatTokens(stats.week_tokens)
      : '—';
    const progress = Math.round((stats.level_progress || 0) * 100);
    document
      .getElementById('gs-progress')
      .style.setProperty('--progress', String(progress / 100));
  } catch {}
}

async function loadGrowthTimeline() {
  const wrap = document.getElementById('growth-timeline');
  if (!wrap) return;
  try {
    const data = await api('GET', '/api/growth/recent?n=30');
    const entries = data.entries || [];
    if (!entries.length) {
      wrap.innerHTML =
        '<p class="empty-hint">尚无记录，完成第一个任务即可开始成长。</p>';
      return;
    }
    wrap.innerHTML = '';
    entries.forEach((e) => {
      const item = document.createElement('div');
      item.className = 'growth-item';
      const icon =
        e.type === 'skill_created'
          ? 'sparkle'
          : e.type === 'skill_used'
            ? 'bolt'
            : 'check';
      const time = new Date(e.time).toLocaleString('zh-CN', {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      });
      const scoreBadge = e.score
        ? ` <span class="growth-score">${e.score}/100</span>`
        : '';
      const desc = e.goal
        ? shorten(e.goal, 60)
        : e.skill_name || e.detail || '';
      item.innerHTML =
        '<span class="growth-item-icon growth-icon-' +
        icon +
        '"></span>' +
        '<div class="growth-item-body">' +
        '<div class="growth-item-title">' +
        esc(desc) +
        scoreBadge +
        '</div>' +
        '<div class="growth-item-meta">' +
        esc(typeLabel(e.type)) +
        ' · ' +
        esc(time) +
        '</div>' +
        '</div>';
      wrap.appendChild(item);
    });
  } catch {}
}

function typeLabel(type) {
  const labels = {
    task_completed: '任务完成',
    skill_created: '技能新增',
    skill_used: '技能使用',
    milestone: '里程碑',
    efficiency: '效率提升',
  };
  return labels[type] || type;
}

function formatTokens(n) {
  const v = Number(n) || 0;
  if (v >= 1000000) return (v / 1000000).toFixed(1) + 'M';
  if (v >= 1000) return (v / 1000).toFixed(1) + 'k';
  return String(v);
}

function formatTiers(tiers) {
  if (!tiers || typeof tiers !== 'object') return '';
  return Object.keys(tiers)
    .sort()
    .map((k) => `${k}: ${tiers[k]}`)
    .join('\n');
}

function parseTiers(text) {
  const tiers = {};
  const bad = [];
  String(text || '')
    .split('\n')
    .forEach((line, idx) => {
      if (!line.trim()) return;
      const i = line.indexOf(':');
      const name = i < 0 ? '' : line.slice(0, i).trim();
      const model = i < 0 ? '' : line.slice(i + 1).trim();
      if (name && model) tiers[name] = model;
      else
        bad.push(
          `第 ${idx + 1} 行「${line.trim().slice(0, 30)}」${i < 0 ? '缺少冒号' : !name ? '档位名为空' : '模型名为空'}（格式：档位: 模型ID）`,
        );
    });
  return { tiers, bad };
}

function formatDuration(sec) {
  if (sec < 60) return Math.round(sec) + '秒';
  if (sec < 3600) return Math.round(sec / 60) + '分钟';
  return (sec / 3600).toFixed(1) + '小时';
}
