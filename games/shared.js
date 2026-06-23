// babynas 游戏共享模块：防误触退出 + 听力保护 + 震动
// 所有小游戏 <script src="/games/shared.js"></script> 引入

(function () {
  // ── 听力保护：全局音量上限，避免婴幼儿近距离听损 ──
  let _AC = null, _master = null;
  window.babyAudio = function () {
    if (!_AC) {
      _AC = new (window.AudioContext || window.webkitAudioContext)();
      _master = _AC.createGain();
      _master.gain.value = 0.55; // 硬上限，单个音再响也不过载
      // 软限幅器，防止多音叠加爆音刺耳
      const comp = _AC.createDynamicsCompressor();
      comp.threshold.value = -18; comp.ratio.value = 12; comp.attack.value = 0.003;
      _master.connect(comp); comp.connect(_AC.destination);
    }
    if (_AC.state === 'suspended') _AC.resume();
    return { ac: _AC, out: _master };
  };

  window.babyVibrate = function (ms) {
    if (navigator.vibrate) navigator.vibrate(ms);
  };

  // ── 防误触退出：长按 1.2s 才回首页，婴幼儿点一下无效 ──
  window.babyExitButton = function () {
    const btn = document.createElement('button');
    btn.id = 'baby-exit';
    btn.innerHTML = '<svg viewBox="0 0 44 44"><circle class="ring" cx="22" cy="22" r="19"/></svg><span>🏠</span>';
    document.body.appendChild(btn);

    const style = document.createElement('style');
    style.textContent = `
      #baby-exit{position:fixed;top:calc(env(safe-area-inset-top) + 8px);left:10px;z-index:9999;
        width:48px;height:48px;border:none;border-radius:50%;background:rgba(255,255,255,.45);
        backdrop-filter:blur(8px);-webkit-backdrop-filter:blur(8px);cursor:pointer;padding:0;
        display:flex;align-items:center;justify-content:center;box-shadow:0 2px 8px rgba(0,0,0,.2)}
      #baby-exit span{font-size:22px;position:relative;z-index:2}
      #baby-exit svg{position:absolute;inset:0;width:48px;height:48px;transform:rotate(-90deg)}
      #baby-exit .ring{fill:none;stroke:#FF6B6B;stroke-width:4;stroke-linecap:round;
        stroke-dasharray:120;stroke-dashoffset:120;transition:stroke-dashoffset .05s linear}
      #baby-exit.holding .ring{stroke-dashoffset:0;transition:stroke-dashoffset 1.2s linear}
    `;
    document.head.appendChild(style);

    let timer = null;
    const start = (e) => {
      e.preventDefault(); e.stopPropagation();
      btn.classList.add('holding');
      babyVibrate(15);
      timer = setTimeout(() => { babyVibrate([20, 40, 20]); location.href = '/'; }, 1200);
    };
    const cancel = () => { clearTimeout(timer); btn.classList.remove('holding'); };
    btn.addEventListener('touchstart', start, { passive: false });
    btn.addEventListener('touchend', cancel);
    btn.addEventListener('touchmove', cancel);
    btn.addEventListener('mousedown', start);
    btn.addEventListener('mouseup', cancel);
    btn.addEventListener('mouseleave', cancel);
  };

  // ── 阻止下拉刷新 / 双指缩放 / 长按菜单等系统手势 ──
  document.addEventListener('gesturestart', e => e.preventDefault());
  document.addEventListener('contextmenu', e => e.preventDefault());
})();
