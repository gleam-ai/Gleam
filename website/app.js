(() => {
  "use strict";

  // ── Glow Canvas: floating particles that drift upward like embers ──
  const canvas = document.getElementById("glow-canvas");
  if (canvas) {
    const ctx = canvas.getContext("2d");
    let w, h, particles, raf;

    function resize() {
      w = canvas.width = window.innerWidth;
      h = canvas.height = window.innerHeight;
    }

    function initParticles() {
      const count = Math.min(80, Math.floor((w * h) / 15000));
      particles = [];
      for (let i = 0; i < count; i++) {
        particles.push({
          x: Math.random() * w,
          y: Math.random() * h,
          r: Math.random() * 1.5 + 0.5,
          vx: (Math.random() - 0.5) * 0.3,
          vy: -(Math.random() * 0.4 + 0.1),
          alpha: Math.random() * 0.5 + 0.1,
          pulse: Math.random() * Math.PI * 2,
        });
      }
    }

    function draw() {
      ctx.clearRect(0, 0, w, h);
      for (const p of particles) {
        p.x += p.vx;
        p.y += p.vy;
        p.pulse += 0.02;

        if (p.y < -10) { p.y = h + 10; p.x = Math.random() * w; }
        if (p.x < -10) p.x = w + 10;
        if (p.x > w + 10) p.x = -10;

        const a = p.alpha * (0.6 + 0.4 * Math.sin(p.pulse));
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(245,166,35,${a})`;
        ctx.fill();

        // soft glow halo
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.r * 4, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(245,166,35,${a * 0.1})`;
        ctx.fill();
      }
      raf = requestAnimationFrame(draw);
    }

    resize();
    initParticles();
    draw();

    let resizeTimer;
    window.addEventListener("resize", () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => { resize(); initParticles(); }, 200);
    });

    // pause when tab not visible
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        cancelAnimationFrame(raf);
      } else {
        draw();
      }
    });
  }

  // ── Nav scroll state ──
  const nav = document.getElementById("nav");
  if (nav) {
    const onScroll = () => {
      nav.classList.toggle("scrolled", window.scrollY > 20);
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
  }

  // ── Scroll Reveal (IntersectionObserver) ──
  const reveals = document.querySelectorAll(".reveal");
  if (reveals.length) {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            const delay = parseInt(e.target.dataset.delay || "0", 10);
            setTimeout(() => e.target.classList.add("visible"), delay);
            observer.unobserve(e.target);
          }
        }
      },
      { threshold: 0.15, rootMargin: "0px 0px -40px 0px" }
    );
    reveals.forEach((el) => observer.observe(el));
  }

  // ── Animated Counters ──
  const statValues = document.querySelectorAll(".stat-value[data-target]");
  if (statValues.length) {
    const counterObserver = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          const el = e.target;
          const target = parseInt(el.dataset.target, 10);
          const suffix = el.dataset.suffix || "";
          const duration = 1200;
          const start = performance.now();

          function tick(now) {
            const progress = Math.min((now - start) / duration, 1);
            const eased = 1 - Math.pow(1 - progress, 3); // ease-out cubic
            const current = Math.round(eased * target);
            el.textContent = current + suffix;
            if (progress < 1) requestAnimationFrame(tick);
          }

          requestAnimationFrame(tick);
          counterObserver.unobserve(el);
        }
      },
      { threshold: 0.5 }
    );
    statValues.forEach((el) => counterObserver.observe(el));
  }

  // ── Terminal Typing Animation ──
  const typedCmd = document.getElementById("typed-cmd");
  const typedOutput = document.getElementById("typed-output");
  if (typedCmd && typedOutput) {
    const commands = [
      {
        cmd: 'gleam goal "organize my markdown files"',
        out: [
          "▸ Planning 4 steps...",
          "  ✓ Scanned 23 files",
          "  ✓ Grouped by topic (5 categories)",
          "  ✓ Moved 18 files, updated 3 links",
          "  ✓ Summary written to INDEX.md",
          "Score: 94/100 — done.",
        ],
      },
      {
        cmd: 'gleam goal "summarize recent git changes"',
        out: [
          "▸ Planning 2 steps...",
          "  ✓ Read last 15 commits",
          "  ✓ Summary: 3 features, 2 fixes, 1 refactor",
          "Score: 97/100 — done.",
        ],
      },
      {
        cmd: 'gleam goal "review PR #42 for security issues"',
        out: [
          "▸ Planning 3 steps...",
          "  ✓ Fetched diff (142 lines changed)",
          "  ✓ Found 1 potential SQL injection",
          "  ✓ Suggested parameterized query fix",
          "Score: 91/100 — 1 issue flagged.",
        ],
      },
    ];

    let cmdIdx = 0;

    function typeCommand(entry, callback) {
      typedCmd.textContent = "";
      typedOutput.innerHTML = "";
      let i = 0;
      const chars = entry.cmd.split("");

      function typeChar() {
        if (i < chars.length) {
          typedCmd.textContent += chars[i];
          i++;
          setTimeout(typeChar, 30 + Math.random() * 40);
        } else {
          setTimeout(() => showOutput(entry.out, callback), 400);
        }
      }
      typeChar();
    }

    function showOutput(lines, callback) {
      let lineIdx = 0;
      function addLine() {
        if (lineIdx < lines.length) {
          const div = document.createElement("div");
          div.textContent = lines[lineIdx];
          div.style.opacity = "0";
          div.style.transform = "translateY(4px)";
          div.style.transition = "opacity 0.3s, transform 0.3s";
          typedOutput.appendChild(div);
          requestAnimationFrame(() => {
            div.style.opacity = "1";
            div.style.transform = "translateY(0)";
          });
          lineIdx++;
          setTimeout(addLine, 200 + Math.random() * 150);
        } else {
          setTimeout(callback, 3000);
        }
      }
      addLine();
    }

    function loop() {
      typeCommand(commands[cmdIdx], () => {
        cmdIdx = (cmdIdx + 1) % commands.length;
        setTimeout(loop, 500);
      });
    }

    // start when terminal scrolls into view
    const terminal = typedCmd.closest(".hero-terminal");
    if (terminal) {
      const termObs = new IntersectionObserver(
        (entries) => {
          if (entries[0].isIntersecting) {
            termObs.disconnect();
            setTimeout(loop, 600);
          }
        },
        { threshold: 0.3 }
      );
      termObs.observe(terminal);
    }
  }

  // ── Bento Card Mouse Glow ──
  document.querySelectorAll(".bento-card").forEach((card) => {
    card.addEventListener("mousemove", (e) => {
      const rect = card.getBoundingClientRect();
      card.style.setProperty("--mouse-x", `${e.clientX - rect.left}px`);
      card.style.setProperty("--mouse-y", `${e.clientY - rect.top}px`);
    });
  });

  // ── i18n: Auto Chinese/English switching ──
  const translations = {
    en: {
      "nav.features": "Features",
      "nav.showcase": "How it works",
      "nav.download": "Download",
      "nav.cta": "Get Started",
      "hero.badge": "v0.0.8 · Open Source · MIT License",
      "hero.title": "Your tasks,<br /><span class=\"glow-text\">handled.</span>",
      "hero.subtitle": "Gleam is a local-first AI agent that plans, executes, and reflects on your work. One 10 MB binary. Zero cloud dependency. Your data stays on your machine.",
      "hero.download": "Download for Windows",
      "hero.github": "Star on GitHub",
      "stats.size": "Binary size",
      "stats.tools": "Built-in tools",
      "stats.tokens": "Token savings",
      "stats.deps": "External deps",
      "features.eyebrow": "Capabilities",
      "features.title": "Not a chatbot.<br /><span class=\"glow-text\">A reliable colleague.</span>",
      "features.plan.title": "Plan → Execute → Reflect",
      "features.plan.desc": "Every task runs through a closed loop: decompose into steps, execute with DAG concurrency, then self-score 0–100. Low scores trigger automatic replanning. No more forgotten follow-ups.",
      "features.plan.step1": "Plan",
      "features.plan.step2": "Execute",
      "features.plan.step3": "Reflect",
      "features.local.title": "Local-first",
      "features.local.desc": "Conversations, memory, tasks, and audit logs default to your machine. Only real LLM calls leave your network — and you pick the provider.",
      "features.llm.title": "9 LLM providers",
      "features.llm.desc": "GLM, DeepSeek, Kimi, Qwen, OpenAI, Anthropic, OpenRouter, and more. Multi-model tier routing — use a strong model for planning, a fast one for summaries.",
      "features.tools.title": "27 built-in tools",
      "features.tools.desc": "File, shell, web, git, clipboard, screenshots, memory, scheduling — plus MCP for unlimited extensions. Install from the marketplace in one click.",
      "features.roles.title": "7 expert roles",
      "features.roles.desc": "Analyst, engineer, PM, researcher, ops, creator — or define your own. Each role brings its own system prompt, tool set, and model tier.",
      "features.skills.title": "Skill crystallization",
      "features.skills.desc": "Successful executions become reusable, versioned skills with success-rate tracking. Failed runs auto-optimize parameters via LLM revision. Your workflows get better every time.",
      "features.skills.s1": "Code review",
      "features.skills.s2": "File organize",
      "features.skills.s3": "Git workflow",
      "showcase.eyebrow": "Interface",
      "showcase.title": "Designed to stay<br /><span class=\"glow-text\">out of your way.</span>",
      "showcase.s1": "Conversation & task execution",
      "showcase.s2": "Multi-provider model config",
      "showcase.s3": "Fine-grained engine controls",
      "download.eyebrow": "Get started",
      "download.title": "One binary.<br /><span class=\"glow-text\">Zero setup.</span>",
      "download.sub": "Released <time id=\"dl-date\" datetime=\"2026-10-12\">2026-10-12</time> · MIT License",
      "download.badge": "Recommended",
      "download.winDesc": "One-click installer · Tray icon · Auto-update · Bundled runtimes",
      "download.winCli": "Windows CLI",
      "download.winCliDesc": "Single-file binary",
      "download.mac": "macOS",
      "download.macDesc": "Apple Silicon",
      "download.linux": "Linux",
      "download.linuxDesc": "x86_64 binary",
      "download.alt": "Or install from source: <code>go install gleam/cmd/gleam@latest</code>",
      "footer.releases": "Releases",
      "footer.license": "MIT License",
      "footer.issues": "Issues",
      "footer.wechat": "WeChat",
      "footer.mp": "Official Account · 宁的AI小站",
      "footer.appreciate": "Appreciate",
      "footer.bottom": "Built with Go, running locally. © 2026 Gleam"
    },
    zh: {
      "nav.features": "功能",
      "nav.showcase": "工作原理",
      "nav.download": "下载",
      "nav.cta": "立即开始",
      "hero.badge": "v0.0.8 · 开源 · MIT 许可",
      "hero.title": "你的任务，<br /><span class=\"glow-text\">交给我。</span>",
      "hero.subtitle": "Gleam 是一个本地优先的 AI 智能体，能规划、执行并反思你的工作。10 MB 单文件二进制，零云端依赖，数据留在你的机器上。",
      "hero.download": "下载 Windows 版",
      "hero.github": "在 GitHub 上 Star",
      "stats.size": "二进制大小",
      "stats.tools": "内置工具",
      "stats.tokens": "Token 节省",
      "stats.deps": "外部依赖",
      "features.eyebrow": "能力",
      "features.title": "不是聊天机器人。<br /><span class=\"glow-text\">是可靠的同事。</span>",
      "features.plan.title": "规划 → 执行 → 反思",
      "features.plan.desc": "每个任务都经过闭环：分解为步骤、DAG 并发执行、自评 0–100 分。低分自动重新规划，不再遗漏后续跟进。",
      "features.plan.step1": "规划",
      "features.plan.step2": "执行",
      "features.plan.step3": "反思",
      "features.local.title": "本地优先",
      "features.local.desc": "对话、记忆、任务和审计日志默认留在本机。只有真实 LLM 调用会出网——而且你可以选择提供商。",
      "features.llm.title": "9 家 LLM 提供商",
      "features.llm.desc": "智谱 GLM、DeepSeek、Kimi、通义、OpenAI、Anthropic、OpenRouter 等。多模型分层路由——规划用强模型，摘要用快模型。",
      "features.tools.title": "27 种内置工具",
      "features.tools.desc": "文件、Shell、Web、Git、剪贴板、截图、记忆、调度——还有 MCP 无限扩展。市场一键安装。",
      "features.roles.title": "7 个专家角色",
      "features.roles.desc": "分析师、工程师、产品经理、研究员、运维、创作者——或自定义。每个角色都有专属系统提示、工具集和模型档位。",
      "features.skills.title": "技能结晶",
      "features.skills.desc": "成功执行会固化为可复用、带版本的成功率追踪技能。失败运行通过 LLM 修正自动优化参数。工作流越用越好。",
      "features.skills.s1": "代码审查",
      "features.skills.s2": "文件整理",
      "features.skills.s3": "Git 工作流",
      "showcase.eyebrow": "界面",
      "showcase.title": "设计得<br /><span class=\"glow-text\">不打扰你。</span>",
      "showcase.s1": "对话与任务执行",
      "showcase.s2": "多提供商模型配置",
      "showcase.s3": "精细引擎控制",
      "download.eyebrow": "开始使用",
      "download.title": "一个二进制。<br /><span class=\"glow-text\">零配置。</span>",
      "download.sub": "发布于 <time id=\"dl-date\" datetime=\"2026-10-12\">2026-10-12</time> · MIT 许可",
      "download.badge": "推荐",
      "download.winDesc": "一键安装 · 托盘图标 · 自动更新 · 内置运行时",
      "download.winCli": "Windows 命令行",
      "download.winCliDesc": "单文件二进制",
      "download.mac": "macOS",
      "download.macDesc": "Apple Silicon",
      "download.linux": "Linux",
      "download.linuxDesc": "x86_64 二进制",
      "download.alt": "或从源码安装：<code>go install gleam/cmd/gleam@latest</code>",
      "footer.releases": "版本",
      "footer.license": "MIT 许可",
      "footer.issues": "问题",
      "footer.wechat": "微信",
      "footer.mp": "公众号 · 宁的AI小站",
      "footer.appreciate": "赞赏",
      "footer.bottom": "用 Go 构建，本地运行。© 2026 Gleam"
    }
  };

  function detectLanguage() {
    const saved = localStorage.getItem("gleam-lang");
    if (saved && (saved === "en" || saved === "zh")) return saved;
    const lang = navigator.language || navigator.userLanguage;
    return lang && lang.toLowerCase().startsWith("zh") ? "zh" : "en";
  }

  function applyLanguage(lang) {
    const dict = translations[lang];
    if (!dict) return;
    document.querySelectorAll("[data-i18n]").forEach((el) => {
      const key = el.getAttribute("data-i18n");
      if (dict[key]) {
        el.innerHTML = dict[key];
      }
    });
    document.documentElement.lang = lang;
    const toggleBtn = document.getElementById("lang-toggle");
    if (toggleBtn) {
      toggleBtn.textContent = lang === "en" ? "中文" : "EN";
      toggleBtn.title = lang === "en" ? "切换到中文" : "Switch to English";
    }
    localStorage.setItem("gleam-lang", lang);
  }

  let currentLang = detectLanguage();
  applyLanguage(currentLang);

  const langToggle = document.getElementById("lang-toggle");
  if (langToggle) {
    langToggle.addEventListener("click", () => {
      currentLang = currentLang === "en" ? "zh" : "en";
      applyLanguage(currentLang);
    });
  }
})();
