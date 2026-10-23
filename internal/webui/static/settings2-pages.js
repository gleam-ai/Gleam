'use strict';

(() => {
  const {
    h,
    row,
    card,
    label,
    head,
    toggle,
    select,
    btn,
    badge,
    empty,
    ico,
    settings,
    save,
  } = S2;

  function prelude(stab, title, desc, right) {
    const panel = document.querySelector(
      `.settings-panel[data-stab="${stab}"]`,
    );
    let pre = panel.querySelector(':scope > .s2-page');
    if (!pre) {
      pre = h('div', { class: 's2-page s2-page--pre' });
      panel.prepend(pre);
      panel.classList.add('s2-legacy');
    }
    pre.replaceChildren(head(title, desc, right));
    return pre;
  }
  const fmtTime = (t) => {
    const d = new Date(t);
    if (isNaN(d)) return '';
    const pad = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };
  const copy = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      toast('已复制', 'success', 1500);
    } catch {
      toast('复制失败：浏览器不允许访问剪贴板', 'warning');
    }
  };

  S2.register('memory', async () => {
    const pre = prelude(
      'memory',
      '记忆',
      'Gleam 的长期记忆与会话上下文都保存在本机数据目录里。',
    );
    const [info, s, local] = await Promise.all([
      api('GET', '/api/info'),
      settings(true),
      api('GET', '/api/local-data').catch(() => ({})),
    ]);
    pre.append(
      label('记忆开关'),
      card(
        row({
          icon: 'compress',
          title: '会话上下文自动压缩',
          desc: '窗口外的旧对话自动摘要成长期上下文，省 token 不丢前情。',
          control: toggle(s.agent.context_compress, (v) =>
            save({ agent: { context_compress: v } }),
          ),
        }),
        row({
          icon: 'memory',
          title: '项目级记忆',
          desc: '打开后，任务沉淀的记忆带上当时的工作区，检索只回本工作区 + 全局的；关着就是全局共享（历史行为）。立即生效，不用重启。',
          control: toggle(
            s.memory.project_scope,
            (v) => save({ memory: { project_scope: v } }),
            { labelText: '项目级记忆' },
          ),
        }),
      ),
      label('记忆文件'),
      card(
        row({
          icon: 'memory',
          title: '长期记忆',
          desc: `${info.memory || 0} 条 · 可在记忆页检索、写入和删除`,
          control: btn('打开记忆页', () => showView('memory')),
        }),
        row({
          icon: 'folder',
          title: '数据目录',
          desc: local.data_dir
            ? `${local.data_dir} · ${local.file_count || 0} 个文件`
            : '—',
          control: local.data_dir
            ? h('button', {
                type: 'button',
                class: 's2-icon-btn',
                title: '复制路径',
                'aria-label': '复制路径',
                html: ico('copy', 15),
                onclick: () => copy(local.data_dir),
              })
            : null,
        }),
      ),
    );
    try {
      loadContext();
    } catch {}
  });

  let extTab = 'mcp';
  let extQ = '';
  S2.register('ext', async () => {
    const root = $('#s2-ext');
    const [mcp, skills, roles] = await Promise.all([
      api('GET', '/api/mcp').catch(() => ({ mcp: [] })),
      api('GET', '/api/skills').catch(() => ({ skills: [] })),
      api('GET', '/api/roles').catch(() => ({ roles: [] })),
    ]);
    const data = {
      mcp: mcp.mcp || [],
      skills: skills.skills || [],
      roles: roles.roles || [],
    };
    const tabs = [
      ['mcp', '插件', 'MCP 服务器'],
      ['skills', '技能', ''],
      ['roles', '内置角色', '专家角色'],
    ];
    const market = btn(
      '',
      () => showView('market'),
      'btn btn-secondary btn-sm',
    );
    market.innerHTML = ico('ext', 14) + '<span>市场</span>';
    const search = h('input', {
      class: 'input s2-search-input',
      type: 'search',
      placeholder: '搜索已安装的扩展',
      value: extQ,
      'aria-label': '搜索扩展',
    });
    const list = h('div', { class: 's2-card s2-card--list' });
    const tabBar = h(
      'div',
      { class: 's2-tabs', role: 'tablist' },
      ...tabs.map(([k, name]) =>
        h(
          'button',
          {
            type: 'button',
            role: 'tab',
            class: 's2-tab',
            'aria-selected': String(extTab === k),
            onclick: () => {
              extTab = k;
              S2.show('ext');
            },
          },
          name,
          h('span', { class: 's2-tab-count', text: String(data[k].length) }),
        ),
      ),
    );
    const addBtn =
      extTab === 'roles'
        ? null
        : btn('', () => showView('market'), 'btn btn-secondary btn-sm');
    if (addBtn) addBtn.innerHTML = ico('plus', 14) + '<span>添加</span>';
    function paintList() {
      const q = extQ.trim().toLowerCase();
      const items = data[extTab].filter(
        (x) =>
          !q ||
          `${x.name} ${x.description || ''} ${x.id || ''}`
            .toLowerCase()
            .includes(q),
      );
      if (!items.length) {
        list.replaceChildren(
          empty(
            extTab === 'roles' ? 'robot' : 'ext',
            q
              ? '没有匹配的扩展'
              : extTab === 'mcp'
                ? '还没有安装插件'
                : extTab === 'skills'
                  ? '还没有技能'
                  : '没有内置角色',
            q
              ? ''
              : extTab === 'roles'
                ? ''
                : '从市场安装，或在对话里让 Gleam 把常用流程固化成技能。',
            !q && extTab !== 'roles'
              ? btn('去市场看看', () => showView('market'))
              : null,
          ),
        );
        return;
      }
      list.replaceChildren(
        ...items.map((x) => {
          let control = null;
          if (extTab === 'mcp') {
            control = toggle(
              x.enabled,
              async (v) => {
                await api(
                  'POST',
                  `/api/mcp/${encodeURIComponent(x.name)}/enabled`,
                  { enabled: v },
                );
                x.enabled = v;
              },
              { labelText: `启用 ${x.name}` },
            );
          } else if (extTab === 'skills') {
            control = toggle(
              !x.disabled,
              async (v) => {
                await api(
                  'POST',
                  `/api/skills/${encodeURIComponent(x.name)}/enabled`,
                  { enabled: v },
                );
                x.disabled = !v;
              },
              { labelText: `启用 ${x.name}` },
            );
          } else control = badge('内置', 'muted');
          const sub =
            extTab === 'mcp'
              ? [
                  x.transport,
                  x.status || (x.connected ? '已连接' : ''),
                  x.tools != null
                    ? `${Array.isArray(x.tools) ? x.tools.length : x.tools} 个工具`
                    : '',
                ]
                  .filter(Boolean)
                  .join(' · ')
              : x.description || '';
          return row({
            icon:
              extTab === 'mcp'
                ? 'plug'
                : extTab === 'skills'
                  ? 'wand'
                  : 'robot',
            title: x.name || x.id,
            desc: sub || x.description || '',
            control,
          });
        }),
      );
    }
    search.addEventListener('input', () => {
      extQ = search.value;
      paintList();
    });
    root.replaceChildren(
      head(
        '扩展管理',
        '管理插件（MCP 服务器）、技能和内置角色。插件与技能可从市场安装。',
        market,
      ),
      h(
        'div',
        { class: 's2-toolbar' },
        tabBar,
        h(
          'div',
          { class: 's2-toolbar-right' },
          h(
            'div',
            { class: 's2-search' },
            h('span', { html: ico('search', 14) }),
            search,
          ),
          addBtn,
        ),
      ),
      list,
    );
    paintList();
  });

  S2.register('hooks', async () => {
    const root = $('#s2-hooks');
    const [{ jobs = [] }, s] = await Promise.all([
      api('GET', '/api/schedules').catch(() => ({ jobs: [] })),
      settings(),
    ]);
    const base = location.origin;
    const listCard = jobs.length
      ? h(
          'div',
          { class: 's2-card s2-card--list' },
          ...jobs.map((j) =>
            row({
              icon: 'hooks',
              title: j.name,
              desc: `POST ${base}/api/hooks/${encodeURIComponent(j.name)}${j.enabled ? '' : ' · 任务已暂停'}`,
              control: h(
                'div',
                { class: 's2-icon-btns' },
                h('button', {
                  type: 'button',
                  class: 's2-icon-btn',
                  title: '复制 curl 命令',
                  'aria-label': '复制 curl 命令',
                  html: ico('copy', 15),
                  onclick: () =>
                    copy(
                      `curl -X POST -H "X-Gleam-Token: $(cat ~/.gleam/webui.token)" ${base}/api/hooks/${encodeURIComponent(j.name)}`,
                    ),
                }),
                btn('触发一次', async () => {
                  const r = await api(
                    'POST',
                    `/api/hooks/${encodeURIComponent(j.name)}`,
                  ).catch((e) => ({ error: e.message }));
                  toast(
                    r.error
                      ? `触发失败：${r.error}`
                      : r.triggered
                        ? `已触发「${j.name}」`
                        : `「${j.name}」没有触发（可能正在运行）`,
                    r.error ? 'error' : 'success',
                  );
                }),
              ),
            }),
          ),
        )
      : card(
          empty(
            'hooks',
            '还没有可触发的钩子',
            '每个定时任务都会自动得到一个 HTTP 钩子；先创建定时任务。',
            btn('去定时任务', () => showView('schedules')),
          ),
        );
    root.replaceChildren(
      head(
        '钩子',
        '外部脚本、CI 或其他程序可以通过 HTTP 请求立刻触发某个定时任务。',
      ),
      label('配置来源'),
      card(
        row({
          icon: 'calendar',
          title: '定时任务',
          desc: `钩子来自定时任务列表（保存在数据目录 ${s.data_dir || ''}）。调度开关：${s.scheduler && s.scheduler.enabled ? '已开启' : '已关闭'}`,
          control: btn('管理', () => showView('schedules')),
        }),
      ),
      h(
        'div',
        { class: 's2-notice s2-notice--warn' },
        h('span', { html: ico('warn', 14) }),
        h('span', {
          text: `钩子没有单独的口令：任何能访问 ${location.host} 的程序都能触发它。Gleam 默认只监听本机地址，不要把这个端口暴露到局域网或公网。`,
        }),
      ),
      label(`钩子（${jobs.length}）`),
      listCard,
    );
  });

  const DESKTOP_ICONS = {
    'desktop.clipboard.read': 'clipboard',
    'desktop.clipboard.write': 'clipboard',
    'desktop.notify': 'bell',
    'desktop.screenshot': 'camera',
    'desktop.snippets': 'snippet',
  };
  const PERM_OPTS = [
    { value: 'default', label: '默认' },
    { value: 'readonly', label: '只读放行' },
    { value: 'user_approved', label: '需我批准' },
    { value: 'full_access', label: '完全访问' },
  ];
  function globalShortcutRow() {
    const desk = window.gleamDesktop;
    if (!desk || typeof desk.globalShortcut !== 'function') return null;
    let cur = '';
    const val = h('span', { class: 's2-keys' });
    const paint = () => {
      val.textContent = cur || '未设置';
      val.dataset.empty = String(!cur);
    };
    const rec = btn(
      '录制',
      () =>
        recordShortcut(val, (accel) => {
          cur = accel;
          paint();
        }),
      'btn btn-secondary btn-sm',
    );
    const clr = btn(
      '清除',
      async () => {
        const r = await desk
          .globalShortcut('set', '')
          .catch((e) => ({ ok: false, error: e.message }));
        if (!r.ok) {
          toast(r.error || '清除失败', 'error');
          return;
        }
        cur = '';
        paint();
        toast('已清除', 'success');
      },
      'btn btn-ghost btn-sm',
    );
    paint();
    desk
      .globalShortcut('get')
      .then((r) => {
        cur = (r && r.accelerator) || '';
        paint();
      })
      .catch(() => {});
    return row({
      icon: 'shortcuts',
      title: '全局唤起快捷键',
      desc: '在任何应用里按这个组合把 Gleam 拉到前台。只在本机注册，不联网。',
      control: h('div', { class: 's2-shortcut-ctl' }, val, rec, clr),
    });
  }

  function recordShortcut(labelEl, done) {
    const before = labelEl.textContent;
    labelEl.textContent = '按下组合键…（Esc 取消）';
    labelEl.dataset.empty = 'false';
    const onKey = (e) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === 'Escape') {
        document.removeEventListener('keydown', onKey, true);
        labelEl.textContent = before;
        return;
      }
      const accel = toAccelerator(e);
      if (!accel) {
        toast('至少要带一个修饰键（Ctrl / Alt / Shift / Win）', 'info');
        return;
      }
      document.removeEventListener('keydown', onKey, true);
      labelEl.textContent = before;
      window.gleamDesktop
        .globalShortcut('set', accel)
        .then((r) => {
          if (!r.ok) {
            toast(r.error || '注册失败（可能被别的程序占用了）', 'error');
            return;
          }
          done(r.accelerator);
          toast('已设置：' + r.accelerator, 'success');
        })
        .catch((err) => toast(err.message || '注册失败', 'error'));
    };
    document.addEventListener('keydown', onKey, true);
  }

  function toAccelerator(e) {
    const mods = [];
    if (e.ctrlKey) mods.push('CommandOrControl');
    if (e.altKey) mods.push('Alt');
    if (e.shiftKey) mods.push('Shift');
    if (e.metaKey) mods.push('Super');
    if (['Control', 'Alt', 'Shift', 'Meta'].includes(e.key) || !mods.length)
      return '';
    const k = e.key;
    let key = k;
    if (k === ' ') key = 'Space';
    else if (k.length === 1) key = k.toUpperCase();
    else if (!/^F\d{1,2}$/.test(k))
      key = k.length <= 12 ? k[0].toUpperCase() + k.slice(1) : '';
    return key ? mods.join('+') + '+' + key : '';
  }

  S2.register('computer', async () => {
    const root = $('#s2-computer');
    const { tools = [] } = await api('GET', '/api/tools').catch(() => ({
      tools: [],
    }));
    const desk = tools.filter((t) => t.name.startsWith('desktop.'));
    const shortcut = globalShortcutRow();
    root.replaceChildren(
      head(
        '电脑操控',
        'Gleam 能在这台电脑上做的桌面动作：读写剪贴板、截屏、发系统通知、插入文本片段。每个动作的放行档位都可以单独调。',
      ),
      h('div', {
        class: 's2-illus',
        'aria-hidden': 'true',
        html: KEYBOARD_SVG,
      }),
      label('桌面工具'),
      desk.length
        ? h(
            'div',
            { class: 's2-card s2-card--list' },
            ...desk.map((t) =>
              row({
                icon: DESKTOP_ICONS[t.name] || 'computer',
                title: t.name,
                desc: t.description,
                control: select(
                  PERM_OPTS,
                  t.overridden ? t.permission : 'default',
                  async (v) => {
                    const r = await api('POST', '/api/tools/permission', {
                      name: t.name,
                      permission: v,
                    });
                    toast(
                      `${t.name}：${PERM_LABELS[r.permission] || r.permission}`,
                      'success',
                      1800,
                    );
                  },
                  { labelText: `${t.name} 权限` },
                ),
              }),
            ),
          )
        : card(empty('computer', '这台电脑没有可用的桌面工具', '')),
      ...(shortcut ? [label('快捷键'), card(shortcut)] : []),
    );
  });
  const KEYBOARD_SVG = `<svg viewBox="0 0 400 150" width="100%" height="150" fill="none" stroke-linecap="round" stroke-linejoin="round">
    <defs><pattern id="s2dots" width="12" height="12" patternUnits="userSpaceOnUse"><circle cx="1.5" cy="1.5" r="1" fill="var(--line)"/></pattern></defs>
    <rect x="0" y="0" width="400" height="150" rx="12" fill="url(#s2dots)"/>
    <rect x="236" y="20" width="120" height="78" rx="8" fill="var(--panel)" stroke="var(--color-fg-faint)" stroke-width="1.5"/>
    <path d="M276 108h40M296 98v10" stroke="var(--color-fg-faint)" stroke-width="1.5"/>
    <rect x="250" y="34" width="56" height="8" rx="4" fill="var(--accent-fill)"/>
    <rect x="250" y="50" width="88" height="6" rx="3" fill="var(--line)"/><rect x="250" y="62" width="70" height="6" rx="3" fill="var(--line)"/>
    <rect x="44" y="52" width="170" height="74" rx="10" fill="var(--panel)" stroke="var(--color-fg-faint)" stroke-width="1.5"/>
    ${Array.from({ length: 3 }, (_, r) => Array.from({ length: 8 }, (_, c) => `<rect x="${56 + c * 19}" y="${62 + r * 16}" width="14" height="11" rx="2.5" fill="${r === 1 && c === 5 ? 'var(--accent-fill)' : 'none'}" stroke="var(--color-fg-faint)" stroke-width="1.2"/>`).join('')).join('')}
    <rect x="94" y="110" width="70" height="9" rx="3" stroke="var(--color-fg-faint)" stroke-width="1.2"/>
    <path d="M214 82c12 0 14-20 22-24" stroke="var(--color-accent)" stroke-width="1.5" stroke-dasharray="3 4"/>
  </svg>`;

  S2.register('git', async () => {
    const s = await settings(true);
    const g = s.git || {};
    const prefix = h('input', {
      class: 'input s2-input',
      value: g.branch_prefix || '',
      'aria-label': '分支前缀',
      placeholder: 'gleam/',
    });
    prefix.addEventListener('change', () =>
      save(
        { git: { branch_prefix: prefix.value.trim() } },
        '已保存，下次建分支生效',
      ),
    );
    const instr = h('textarea', {
      class: 'input s2-textarea',
      rows: 4,
      'aria-label': '提交说明指令',
      placeholder: '例如：使用 Conventional Commits，说明写中文。',
    });
    instr.value = g.commit_instructions || '';
    instr.addEventListener('change', () =>
      save(
        { git: { commit_instructions: instr.value.trim() } },
        '已保存，会附在编程任务的指引里',
      ),
    );
    $('#s2-git').replaceChildren(
      head(
        'Git',
        '由 Gleam 自己执行的分支、提交与推送（走安全门控；推送每次都要你批准）。模型在 shell.exec 里自己拼的 git 命令不归这里管。',
      ),
      label('分支'),
      card(
        row({
          icon: 'branch',
          title: '分支前缀',
          desc: '由 Gleam 创建的分支统一加上这个前缀；已经有了就不重复加。',
          control: prefix,
        }),
        row({
          icon: 'git',
          title: '始终强制推送',
          desc: '推送时使用 --force-with-lease——比裸 --force 安全：远端有新提交就拒绝，不会把别人的活覆盖掉。',
          control: toggle(
            !!g.force_push,
            (v) => save({ git: { force_push: v } }),
            { labelText: '始终强制推送' },
          ),
        }),
      ),
      label('提交说明'),
      card(
        row({
          icon: 'edit',
          title: '提交说明指令',
          desc: '写提交说明时的写法要求，会附在编程任务的指引里，由模型按它写。',
        }),
        h('div', { class: 's2-card-pad' }, instr),
      ),
    );
  });
  S2.register('worktrees', async () => {
    const view = await api('GET', '/api/worktrees').catch(() => ({}));
    const ws = view.workspace || '';
    const repo = !!view.repository;
    const on = !!view.enabled;

    const blocked = !on
      ? null
      : !ws
        ? '还没选工作区：没有可建副本的仓库，任务会在不限定目录的边界里执行。'
        : !repo
          ? `当前工作区不是 git 仓库${ws ? '（' + ws + '）' : ''}，任务会在工作区里直接执行——想用副本请先把工作区换到一个 git 仓库。`
          : null;

    const rows = view.rows || [];
    const list = rows.length
      ? card(
          ...rows.map((wt) =>
            row({
              icon: 'worktrees',
              title: wt.branch,
              titleExtra: wt.dirty ? badge('有改动') : null,
              desc: `${wt.path} · ${fmtTime(wt.created_at)}${wt.dirty_detail ? ' · ' + wt.dirty_detail : ''}`,
              control: btn('删除', () => removeWorktree(wt)),
            }),
          ),
        )
      : card(
          empty(
            'worktrees',
            blocked
              ? '这里现在建不了 worktree'
              : '还没有由 Gleam 管理的 worktree',
            blocked ||
              '打开上面的开关之后，下一个任务就会在自己的副本目录里执行。',
          ),
        );

    $('#s2-worktrees').replaceChildren(
      ...[
        head(
          'Worktrees',
          '为每个任务开一个 git worktree：任务在副本目录里改文件，主工作区全程不动。改动留在任务分支上，不会自动并回主工作区。',
        ),
        blocked
          ? h(
              'div',
              { class: 's2-notice s2-notice--warn' },
              h('span', { html: ico('warn', 14) }),
              h('span', { text: blocked }),
            )
          : null,
        label('执行方式'),
        card(
          row({
            icon: 'layers',
            title: '为任务创建 worktree',
            desc: on
              ? '这个功能开着：每个任务建一份副本，任务在副本里执行。'
              : '关着：任务直接在你的工作区里执行，改动就地落盘。',
            control: toggle(on, (v) =>
              save({ worktrees: { enabled: v } }, v ? '已开启' : '已关闭').then(
                () => S2.show('worktrees'),
              ),
            ),
          }),
        ),
        label('创建与清理'),
        card(
          row({
            icon: 'refresh',
            title: '创建前先 fetch',
            desc: '新建副本之前先同步远端（会出网，见「连接」页的远端仓库同步那一行）。',
            control: toggle(!!view.fetch_before_create, (v) =>
              save({ worktrees: { fetch_before_create: v } }),
            ),
          }),
          row({
            icon: 'trash',
            title: '自动删除',
            desc: '任务跑完后自动删掉没有未提交改动的副本；有改动的一律保留，不会被静默丢掉。',
            control: toggle(!!view.auto_delete, (v) =>
              save({ worktrees: { auto_delete: v } }),
            ),
          }),
          row({
            icon: 'layers',
            title: '数量上限',
            desc: '超过上限时清掉最早且干净的；有改动的一个都不清，所以实际数量可能停在上限之上。',
            control: select(
              [
                { value: 0, label: '不限制' },
                { value: 5, label: '5 个' },
                { value: 15, label: '15 个' },
                { value: 30, label: '30 个' },
              ],
              Number(view.max_count) || 0,
              (v) => save({ worktrees: { max_count: Number(v) } }),
              { labelText: '数量上限' },
            ),
          }),
        ),
        label('由 Gleam 管理的 Worktrees'),
        list,
      ].filter(Boolean),
    );
  });

  async function removeWorktree(wt) {
    const url = (force) =>
      `/api/worktrees/${encodeURIComponent(wt.task_id)}${force ? '?force=1' : ''}`;
    try {
      await api('DELETE', url(false));
    } catch (err) {
      if (err.status !== 409) {
        toast(err.message || String(err), 'error');
        return;
      }
      const ok = await confirmModal(
        `${err.message}。继续会把里面的内容一起丢掉，且丢了找不回来。`,
        '副本不能直接删除',
        { okText: '丢弃并删除', danger: true },
      );
      if (!ok) return;
      try {
        await api('DELETE', url(true));
      } catch (err2) {
        toast(err2.message || String(err2), 'error');
        return;
      }
    }
    toast('已删除', 'success', 1500);
    S2.show('worktrees');
  }

  S2.register('conn', async () => {
    const root = $('#s2-conn');
    root.replaceChildren(
      head('连接', '已有连接与 Gleam 的出网边界。'),
      label('连接与出网'),
    );
    try {
      loadConnections();
    } catch {}
  });

  S2.register('safety', async () => {
    const pre = prelude(
      'safety',
      '安全',
      '每一个会写文件、跑命令或出网的动作，执行前都要过 Gleam 的安全门控。',
    );
    const s = await settings(true);
    pre.append(
      label('扫描层级'),
      card(
        row({
          icon: 'scan',
          title: '静态检查',
          desc: '按规则判风险：路径越界、危险命令、敏感文件。所有动作都会经过，不能关闭。',
          control: h(
            'span',
            { class: 's2-always' },
            toggle(true, () => {}, { disabled: true, labelText: '静态检查' }),
            h('small', { text: '始终开启' }),
          ),
        }),
        row({
          icon: 'shield',
          title: '轻量扫描',
          desc: '对本来会自动放行的中高风险动作，先用辅助模型快筛一遍，被标记才交给你确认。',
          control: toggle(
            s.safety.ai_review,
            (v) => save({ safety: { ai_review: v } }),
            { labelText: '轻量扫描' },
          ),
        }),
        row({
          icon: 'search',
          title: '深度扫描',
          desc: '执行前把整段计划交给主模型审一遍。逐动作的轻量扫描看不见"每步都正常、连起来却在做另一件事"；这一步看的是全局。它否决时会停下来问你，你说继续就继续。',
          control: toggle(
            s.safety.deep_review,
            (v) => save({ safety: { deep_review: v } }),
            { labelText: '深度扫描' },
          ),
        }),
      ),
    );
  });

  S2.register('go', () => {
    prelude('go', 'Go 工具链', '编程任务里构建、测试 Go 项目要用到的工具链。');
  });
  S2.register('engine', () => {
    prelude('engine', '引擎', '规划与执行的上限、预算熔断和循环治理。');
  });

  let arQ = '';
  let arKind = '';
  S2.register('archived', async () => {
    const root = $('#s2-archived');
    const { goals = [] } = await api('GET', '/api/goals').catch(() => ({
      goals: [],
    }));
    const done = goals.filter((g) => g.status !== 'running');
    const KIND = { code: '编程', work: '目标', chat: '对话' };
    const STATUS = {
      success: '已完成',
      partial: '部分完成',
      failed: '失败',
      cancelled: '已取消',
    };
    const list = h('div', { class: 's2-card s2-card--list' });
    const search = h('input', {
      class: 'input s2-search-input',
      type: 'search',
      placeholder: '搜索任务',
      value: arQ,
      'aria-label': '搜索已归档任务',
    });
    const kinds = [{ value: '', label: '所有类型' }].concat(
      Object.entries(KIND).map(([k, v]) => ({ value: k, label: v })),
    );
    const kindSel = select(
      kinds,
      arKind,
      (v) => {
        arKind = v;
        paint();
      },
      { labelText: '任务类型' },
    );
    const delAll = btn(
      '全部删除',
      async () => {
        if (!done.length) return;
        if (
          !(await confirmModal(
            `将永久删除 ${done.length} 条已结束任务的记录（盘上的归档文件一并删除），无法恢复。`,
            '全部删除？',
            { okText: '全部删除', danger: true },
          ))
        )
          return;
        let ok = 0;
        for (const g of done) {
          try {
            await api('DELETE', `/api/goals/${encodeURIComponent(g.task_id)}`);
            ok++;
          } catch {}
        }
        toast(`已删除 ${ok} 条记录`, 'success');
        try {
          loadGoals();
        } catch {}
        S2.show('archived');
      },
      'btn btn-danger-soft btn-sm s2-danger',
      { disabled: done.length ? null : true },
    );
    function paint() {
      const q = arQ.trim().toLowerCase();
      const items = done.filter(
        (g) =>
          (!arKind || (g.task_mode || 'work') === arKind) &&
          (!q || (g.goal || '').toLowerCase().includes(q)),
      );
      if (!items.length) {
        list.replaceChildren(
          empty(
            'archived',
            q || arKind ? '没有匹配的任务' : '暂无已归档任务',
            q || arKind ? '' : '任务结束后会出现在这里，重启后也读得回来。',
          ),
        );
        return;
      }
      list.replaceChildren(
        ...items.map((g) =>
          row({
            icon: g.status === 'success' ? 'check' : 'archived',
            title: shorten(g.goal || g.task_id, 60),
            desc: `${STATUS[g.status] || g.status} · ${KIND[g.task_mode] || '目标'} · ${fmtTime(g.started_at)}`,
            control: h(
              'div',
              { class: 's2-icon-btns' },
              btn('打开', () => {
                showView('goals');
                openTaskDetail(g.task_id);
              }),
              h('button', {
                type: 'button',
                class: 's2-icon-btn s2-icon-btn--danger',
                title: '删除',
                'aria-label': '删除',
                html: ico('trash', 15),
                onclick: async () => {
                  if (
                    !(await confirmModal(
                      `「${shorten(g.goal, 40)}」的记录和归档文件会被永久删除。`,
                      '删除这条任务？',
                      { okText: '删除', danger: true },
                    ))
                  )
                    return;
                  try {
                    await api(
                      'DELETE',
                      `/api/goals/${encodeURIComponent(g.task_id)}`,
                    );
                    toast('已删除', 'success', 1500);
                    try {
                      loadGoals();
                    } catch {}
                    S2.show('archived');
                  } catch (err) {
                    toast(err.message, 'error');
                  }
                },
              }),
            ),
          }),
        ),
      );
    }
    search.addEventListener('input', () => {
      arQ = search.value;
      paint();
    });
    root.replaceChildren(
      head(
        '已归档',
        '已经结束的任务记录（保存在数据目录 tasks/ 下）。打开可查看过程和结果。',
        delAll,
      ),
      h(
        'div',
        { class: 's2-toolbar' },
        h(
          'div',
          { class: 's2-tabs', role: 'tablist' },
          h(
            'button',
            {
              type: 'button',
              role: 'tab',
              class: 's2-tab',
              'aria-selected': 'true',
            },
            '任务',
            h('span', { class: 's2-tab-count', text: String(done.length) }),
          ),
        ),
        h(
          'div',
          { class: 's2-toolbar-right' },
          h(
            'div',
            { class: 's2-search' },
            h('span', { html: ico('search', 14) }),
            search,
          ),
          kindSel,
        ),
      ),
      list,
    );
    paint();
  });

  S2.register('labs', () => {
    const root = $('#s2-labs');
    const ack = !!UIPrefs.get().labsAck;
    const box = h('input', { type: 'checkbox', id: 's2-labs-ack' });
    box.checked = ack;
    box.addEventListener('change', () => {
      UIPrefs.set({ labsAck: box.checked || undefined });
      S2.show('labs');
    });
    const kids = [
      head(
        '实验功能',
        '还在打磨中的能力，可能不稳定，也可能在之后的版本里改动或移除。',
      ),
      card(
        row({
          icon: 'warn',
          title: '实验功能可能不稳定',
          desc: '可能出现异常结果、额外的模型调用或更高的资源占用。',
        }),
        row({
          icon: 'labs',
          title: '随时可能变化',
          desc: '实验功能不保证向后兼容，正式发布前行为可能调整。',
        }),
        h(
          'label',
          { class: 's2-ack', for: 's2-labs-ack' },
          box,
          h('span', { text: '我了解风险，显示实验功能' }),
        ),
      ),
    ];
    if (ack) {
      kids.push(
        label('实验功能'),
        card(
          empty(
            'labs',
            '这个版本没有可开关的实验功能',
            '有新的实验功能时会出现在这里。',
          ),
        ),
      );
    }
    root.replaceChildren(...kids);
  });

  S2.register('network', async () => {
    const root = $('#s2-network');
    const [s, net] = await Promise.all([
      settings(true),
      api('GET', '/api/network').catch(() => ({
        proxy_env: {},
        proxy_keys: [],
      })),
    ]);
    const L = s.llm;
    const targets = [{ name: '主模型', model: L.model, body: {} }];
    const fastEntry = (L.models || []).find((m) => m.is_fast);
    if (L.fast_model) {
      // The auxiliary model may be a separate endpoint from another vendor;
      // do not test it against the primary model's URL.
      targets.push({
        name: '辅助模型',
        model: L.fast_model,
        body: {
          model: L.fast_model,
          base_url: (fastEntry && fastEntry.base_url) || L.base_url,
          protocol: (fastEntry && fastEntry.protocol) || L.protocol,
          provider_id: (fastEntry && fastEntry.provider_id) || L.provider_id,
          plan: (fastEntry && fastEntry.plan) || L.plan,
        },
      });
    }
    const results = new Map();
    const resultEl = (t) => {
      const r = results.get(t.name);
      if (!r)
        return h('small', {
          class: 's2-row-desc',
          text: `${L.base_url ? new URL(L.base_url, location.href).host : '—'} · ${t.model || '未配置模型'}`,
        });
      return h('small', {
        class: 's2-row-desc ' + (r.ok ? 's2-ok' : 's2-warn'),
        text: r.text,
      });
    };
    const list = h('div', { class: 's2-card s2-card--list' });
    function paint() {
      list.replaceChildren(
        ...targets.map((t) =>
          row({ icon: 'network', title: t.name, note: resultEl(t) }),
        ),
      );
    }
    const run = btn(
      '开始检测',
      async () => {
        run.disabled = true;
        for (const t of targets) {
          results.set(t.name, { ok: true, text: '检测中…' });
          paint();
          const t0 = performance.now();
          const r = await api('POST', '/api/llm/test', t.body).catch((e) => ({
            ok: false,
            message: e.message,
          }));
          const ms = Number.isFinite(r.latency_ms)
            ? r.latency_ms
            : Math.round(performance.now() - t0);
          results.set(
            t.name,
            r.kind === 'mock'
              ? { ok: true, text: '离线演示模型：没有发起网络请求' }
              : r.ok
                ? {
                    ok: true,
                    text: `连通 · ${ms} ms · ${r.base_url ? new URL(r.base_url).host : ''}`,
                  }
                : {
                    ok: false,
                    text: `不通：${r.message || r.kind}${r.http_status ? `（HTTP ${r.http_status}）` : ''}`,
                  },
          );
          paint();
        }
        run.disabled = false;
      },
      'btn btn-secondary btn-sm',
    );
    const keys = net.proxy_keys || [];
    const proxyDesc = keys.length
      ? keys.map((k) => `${k}=${net.proxy_env[k]}`).join(' · ')
      : '没有检测到 HTTPS_PROXY / HTTP_PROXY 环境变量：直接连接。';

    const curMode = net.proxy_mode || 'system';
    const modeSel = h(
      'select',
      { class: 's2-select', 'aria-label': '代理方式' },
      h('option', { value: 'system', text: '跟随系统' }),
      h('option', { value: 'manual', text: '手动' }),
      h('option', { value: 'none', text: '不使用' }),
    );
    modeSel.value = curMode;
    const urlIn = h('input', {
      class: 'input s2-input',
      placeholder: 'http://127.0.0.1:7890',
      'aria-label': '代理地址',
      value: net.proxy_url || '',
    });
    let proxyDirty = false;
    urlIn.addEventListener('input', () => {
      proxyDirty = true;
    });
    urlIn.hidden = curMode !== 'manual';
    modeSel.addEventListener('change', async () => {
      const v = modeSel.value;
      urlIn.hidden = v !== 'manual';
      if (v === 'manual') {
        urlIn.focus();
        return;
      }
      await save({ network: { proxy_mode: v } }, '已保存，重启 Gleam 后生效');
      S2.show('network');
    });
    const commitURL = async () => {
      const raw = urlIn.value.trim();
      if (!raw) {
        toast('先填代理地址，例如 http://127.0.0.1:7890', 'info');
        urlIn.focus();
        return;
      }
      // The displayed URL has its password redacted. Submitting it unchanged
      // would replace the real credential with the placeholder.
      if (!proxyDirty) {
        toast('代理地址没有改动', 'info');
        return;
      }
      try {
        await save(
          { network: { proxy_mode: 'manual', proxy_url: raw } },
          '已保存，重启 Gleam 后生效',
        );
        S2.show('network');
      } catch (err) {
        toast(err.message, 'error');
      }
    };
    urlIn.addEventListener('change', commitURL);
    urlIn.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        commitURL();
      }
    });
    const proxyCtl = h('div', { class: 's2-proxy-ctl' }, modeSel, urlIn);
    const proxyModeDesc =
      '跟随系统：按启动 Gleam 时的 HTTPS_PROXY / HTTP_PROXY / NO_PROXY 环境变量；' +
      '手动：只走下面这一条地址；不使用：直连，环境变量一律忽略。改完重启 Gleam 才切换。';
    const effDesc =
      curMode === 'none'
        ? '直连：代理已关闭，环境变量不再生效。'
        : curMode === 'manual'
          ? `手动：${net.proxy_url || '（还没填地址，保存后才生效）'}`
          : proxyDesc;
    root.replaceChildren(
      head('网络', 'Gleam 访问模型服务时的连通情况与代理方式。'),
      h('div', { class: 's2-label-row' }, label('连接检测'), run),
      list,
      label('代理'),
      card(
        row({
          icon: 'globe',
          title: '代理方式',
          desc: proxyModeDesc,
          control: proxyCtl,
        }),
        row({ icon: 'link', title: '当前生效', desc: effDesc }),
      ),
    );
    paint();
  });

  const ledger = $('#cx-conn-list') && $('#cx-conn-list').closest('.card');
  const connPanel = document.querySelector('.settings-panel[data-stab="conn"]');
  if (ledger && connPanel) {
    ledger.classList.add('s2-ledger');
    connPanel.classList.add('s2-legacy');
    connPanel.append(ledger);
  }

  S2.init();
})();
