/* ============================================================
   site.js · reveal observer + nav highlighter + small utilities
   ============================================================ */

(() => {
  // ---- theme init (no-FOUC pattern — run before reveal) ----
  // Persisted under "plivo-theme" (matches STYLE.md §9).
  const THEME_KEY = 'plivo-theme';
  function applyTheme(t) {
    if (t === 'dark') document.documentElement.classList.add('dark');
    else document.documentElement.classList.remove('dark');
  }
  function currentTheme() {
    const stored = localStorage.getItem(THEME_KEY);
    if (stored === 'dark' || stored === 'light') return stored;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  applyTheme(currentTheme());

  // toggle wiring
  document.querySelectorAll('[data-theme-toggle]').forEach(btn => {
    btn.addEventListener('click', () => {
      const next = currentTheme() === 'dark' ? 'light' : 'dark';
      localStorage.setItem(THEME_KEY, next);
      applyTheme(next);
    });
  });
  // sync across tabs
  window.addEventListener('storage', e => {
    if (e.key === THEME_KEY) applyTheme(currentTheme());
  });

  // reveal on scroll
  const io = new IntersectionObserver(
    entries => {
      entries.forEach(e => {
        if (e.isIntersecting) {
          e.target.classList.add('is-in');
          io.unobserve(e.target);
        }
      });
    },
    { threshold: 0.12, rootMargin: '0px 0px -8% 0px' }
  );

  document
    .querySelectorAll('[data-reveal], [data-reveal-stagger]')
    .forEach(el => io.observe(el));

  // active nav based on current path
  const path = location.pathname.split('/').pop() || 'index.html';
  document.querySelectorAll('.nav-links a').forEach(a => {
    const href = a.getAttribute('href');
    if (href === path) a.classList.add('is-active');
  });

  // typing effect for `data-typing` elements
  document.querySelectorAll('[data-typing]').forEach(el => {
    const full = el.textContent;
    el.textContent = '';
    const speed = parseInt(el.dataset.typing || '32', 10);
    let i = 0;
    const tio = new IntersectionObserver(entries => {
      entries.forEach(e => {
        if (!e.isIntersecting) return;
        tio.disconnect();
        const tick = () => {
          if (i >= full.length) return;
          el.textContent = full.slice(0, ++i);
          setTimeout(tick, speed + Math.random() * 20);
        };
        tick();
      });
    });
    tio.observe(el);
  });

  // copy-to-clipboard on .copyable
  document.querySelectorAll('.copyable').forEach(el => {
    el.addEventListener('click', async () => {
      const text = el.dataset.copy || el.textContent;
      try {
        await navigator.clipboard.writeText(text);
        const original = el.dataset.copyLabel || el.getAttribute('data-tip') || '';
        const note = document.createElement('span');
        note.className = 'copied-flash';
        note.textContent = 'copied';
        el.appendChild(note);
        setTimeout(() => note.remove(), 1200);
      } catch (e) {}
    });
  });

  // year stamp
  document.querySelectorAll('[data-year]').forEach(e => {
    e.textContent = new Date().getFullYear();
  });
})();
