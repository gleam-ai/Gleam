const TrayLink = (() => {
  const desk = window.gleamDesktop;
  if (!desk || !desk.tray) return { init() {}, push() {} };

  const MAX = 15;
  let timer = null;
  let seq = 0;

  function lang() {
    return (document.documentElement.getAttribute('lang') || '').startsWith(
      'en',
    )
      ? 'en'
      : 'zh';
  }

  async function push() {
    const mine = ++seq;
    let recents = [];
    try {
      const res = await api('GET', '/api/conversations');
      recents = (res.conversations || [])
        .filter((c) => c && c.id)
        .slice(0, MAX)
        .map((c) => ({ id: String(c.id), title: String(c.title || '') }));
    } catch {
      recents = [];
    }
    if (mine !== seq) return;
    try {
      await desk.tray.sync({ lang: lang(), recents });
    } catch {}
  }

  function soon() {
    clearTimeout(timer);
    timer = setTimeout(push, 300);
  }

  function init() {
    desk.tray.onAction((a) => {
      if (!a || typeof a.action !== 'string') return;
      if (a.action === 'new-chat') {
        const btn = $('#convo-new');
        if (btn) btn.click();
      } else if (a.action === 'settings') {
        showView('settings');
      } else if (a.action === 'open-convo' && a.id) {
        // Activating a conversation does not leave whatever page the user was
        // on (settings, schedules); the thread must be brought to the front.
        showView('goals');
        openConvo(String(a.id));
      }
    });

    const list = $('#convo-list');
    if (list)
      new MutationObserver(soon).observe(list, {
        childList: true,
        subtree: true,
      });
    new MutationObserver(soon).observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['lang'],
    });
    push();
  }

  return { init, push };
})();

TrayLink.init();
