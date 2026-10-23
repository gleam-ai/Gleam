import { BrowserWindow } from 'electron';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Lang } from './locale';
import type { RuntimeProbe } from './runtimes';

export interface SplashState {
  status: string;
  items: RuntimeProbe[];
  total: number;
  holdSeconds: number;
}

function brandDataUrl(): string {
  const candidates = [
    join(process.resourcesPath, 'icon.png'),
    join(__dirname, '..', '..', '..', 'assets', 'icons', 'gleam-128.png'),
  ];
  for (const p of candidates) {
    if (existsSync(p))
      return 'data:image/png;base64,' + readFileSync(p).toString('base64');
  }
  return '';
}

function copy(lang: Lang) {
  return lang === 'zh'
    ? {
        title: 'Gleam 环境准备',
        heading: '环境准备中',
        sub: '正在准备运行环境，请稍候，完成后自动进入主界面。',
        starting: '正在启动…',
        hold: '（{n} 秒后自动进入主界面）',
        entering: '（正在进入主界面…）',
      }
    : {
        title: 'Gleam setup',
        heading: 'Preparing environment',
        sub: 'Getting the runtime ready — the main window opens as soon as this finishes.',
        starting: 'Starting…',
        hold: ' (opening the main window in {n}s)',
        entering: ' (opening the main window…)',
      };
}

const html = (brand: string, lang: Lang) => {
  const t = copy(lang);
  const js = (v: unknown) => JSON.stringify(v);
  return `<!doctype html><html lang="${lang === 'zh' ? 'zh-CN' : 'en'}"><head><meta charset="utf-8">
<title>${t.title}</title><style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin: 0; height: 100vh; display: flex; flex-direction: column; align-items: center;
         justify-content: center; gap: 14px; background: #F5F5F2; color: #1A1A18;
         font: 13px/1.6 "Microsoft YaHei", "Segoe UI", system-ui, sans-serif; user-select: none; }
  .mark { width: 72px; height: 72px; display: grid; place-items: center; }
  .mark img { width: 72px; height: 72px; object-fit: contain; }
  h1 { margin: 4px 0 0; font-size: 22px; font-weight: 600; letter-spacing: .01em; }
  .sub { color: #6B6B66; font-size: 13px; margin: 0; }
  .bar { width: 340px; height: 6px; border-radius: 3px; background: #E4E4DF; overflow: hidden; margin-top: 6px; }
  .bar span { display: block; height: 100%; width: 0; border-radius: 3px; background: #7DBE6A; transition: width .18s ease; }
  .cap { color: #8A8A84; font-size: 12px; margin: 2px 0 0; min-height: 18px; }
  ul { list-style: none; margin: 10px 0 0; padding: 0; width: 400px; display: flex; flex-direction: column; gap: 6px; }
  li { display: flex; align-items: flex-start; gap: 8px; font-size: 12px; color: #4A4A46; }
  li .dot { flex: none; width: 14px; text-align: center; }
  li.ok .dot { color: #4E9A5F; }
  li.miss .dot { color: #B4801F; }
  li b { font-weight: 600; color: #1A1A18; }
  li small { display: block; color: #8A8A84; font-size: 11.5px; }
</style></head><body>
  <div class="mark">${brand ? `<img src="${brand}" alt="">` : ''}</div>
  <h1>${t.heading}</h1>
  <p class="sub">${t.sub}</p>
  <div class="bar"><span id="fill"></span></div>
  <p class="cap" id="cap">${t.starting}</p>
  <ul id="items"></ul>
<script>
  var HOLD = ${js(t.hold)};
  var ENTERING = ${js(t.entering)};
  window.render = function (s) {
    var okCount = s.items.filter(function (i) { return i.ok; }).length;
    document.getElementById('fill').style.width = Math.round((okCount / Math.max(1, s.total)) * 100) + '%';
    document.getElementById('items').innerHTML = s.items.map(function (i) {
      return '<li class="' + (i.ok ? 'ok' : 'miss') + '"><span class="dot">' + (i.ok ? '✓' : '!') + '</span>'
        + '<span><b>' + i.label + '</b>' + (i.version ? ' · ' + i.version : '')
        + '<small>' + i.detail + '</small></span></li>';
    }).join('');
    var cap = document.getElementById('cap');
    if (!s.holdSeconds) { cap.textContent = s.status; return; }
    var left = s.holdSeconds;
    cap.textContent = s.status + HOLD.replace('{n}', String(left));
    var t = setInterval(function () {
      left -= 1;
      if (left <= 0) { clearInterval(t); cap.textContent = s.status + ENTERING; return; }
      cap.textContent = s.status + HOLD.replace('{n}', String(left));
    }, 1000);
  };
</script></body></html>`;
};

export class Splash {
  private win: BrowserWindow;

  constructor(private readonly lang: Lang) {
    this.win = new BrowserWindow({
      width: 560,
      height: 470,
      resizable: false,
      minimizable: false,
      maximizable: false,
      fullscreenable: false,
      frame: false,
      show: false,
      backgroundColor: '#F5F5F2',
    });
    this.win.loadURL(
      'data:text/html;charset=utf-8,' +
        encodeURIComponent(html(brandDataUrl(), this.lang)),
    );
    this.win.once('ready-to-show', () => this.win.show());
  }

  render(state: SplashState): void {
    if (this.win.isDestroyed()) return;
    void this.win.webContents
      .executeJavaScript(`window.render(${JSON.stringify(state)})`)
      .catch(() => {});
  }

  close(): void {
    if (!this.win.isDestroyed()) this.win.destroy();
  }
}
