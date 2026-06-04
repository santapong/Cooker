/* Cosmic Cooker — drifting starfield (shared) */
(function () {
  const canvas = document.getElementById('starfield');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  let stars = [], w = 0, h = 0, dpr = Math.min(window.devicePixelRatio || 1, 2);

  function resize() {
    w = canvas.clientWidth; h = canvas.clientHeight;
    canvas.width = w * dpr; canvas.height = h * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const count = Math.round((w * h) / 7000);
    stars = Array.from({ length: count }, () => ({
      x: Math.random() * w, y: Math.random() * h,
      r: Math.random() * 1.3 + 0.25,
      o: Math.random() * 0.6 + 0.18,
      tw: Math.random() * 0.02 + 0.004,
      ph: Math.random() * Math.PI * 2,
      vy: Math.random() * 0.06 + 0.01,
      hue: Math.random() < 0.18 ? '#FFB454' : Math.random() < 0.3 ? '#33E1F2' : '#FFFFFF',
    }));
  }
  function frame(t) {
    ctx.clearRect(0, 0, w, h);
    for (const s of stars) {
      s.y += s.vy; if (s.y > h + 2) s.y = -2;
      const op = s.o * (0.6 + 0.4 * Math.sin(s.ph + t * s.tw));
      ctx.globalAlpha = Math.max(0, op);
      ctx.fillStyle = s.hue;
      ctx.shadowColor = s.hue; ctx.shadowBlur = s.r * 3;
      ctx.beginPath(); ctx.arc(s.x, s.y, s.r, 0, Math.PI * 2); ctx.fill();
    }
    ctx.globalAlpha = 1; ctx.shadowBlur = 0;
    requestAnimationFrame(frame);
  }
  resize();
  window.addEventListener('resize', resize);
  requestAnimationFrame(frame);
})();
