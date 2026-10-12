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
})();
