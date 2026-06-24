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
      #baby-exit.holding .ring{stroke-dashoffset:0;transition:stroke-dashoffset .7s linear}
      #baby-exit-hint{position:fixed;top:calc(env(safe-area-inset-top) + 64px);left:14px;z-index:9999;
        background:rgba(20,18,30,.85);color:#fff;padding:7px 14px;border-radius:18px;font-size:13px;
        opacity:0;pointer-events:none;transition:opacity .2s;white-space:nowrap}
      #baby-exit-hint.show{opacity:1}
      @media (min-width:768px){
        #baby-exit{width:60px;height:60px;top:calc(env(safe-area-inset-top) + 12px);left:14px}
        #baby-exit svg{width:60px;height:60px}
        #baby-exit span{font-size:28px}
      }
    `;
    document.head.appendChild(style);

    const hint = document.createElement('div');
    hint.id = 'baby-exit-hint';
    hint.textContent = '双击返回上一页 · 长按回首页';
    document.body.appendChild(hint);
    const flashHint = () => {
      hint.classList.add('show');
      clearTimeout(hint._t);
      hint._t = setTimeout(() => hint.classList.remove('show'), 1600);
    };

    // 统一返回手势：双击 → 上一页(游戏列表)，长按 → 首页，单击仅提示
    let taps = 0, tapT = null, holdT = null, held = false;
    const down = (e) => {
      e.preventDefault(); e.stopPropagation();
      held = false;
      btn.classList.add('holding');
      babyVibrate(15);
      holdT = setTimeout(() => { held = true; babyVibrate([20,40,20]); location.href = '/'; }, 700);
    };
    const up = (e) => {
      if (e) e.preventDefault();
      clearTimeout(holdT);
      btn.classList.remove('holding');
      if (held) return;
      taps++;
      if (taps >= 2) { clearTimeout(tapT); taps = 0; babyVibrate(20); location.href = '/#games'; }
      else { tapT = setTimeout(() => { taps = 0; flashHint(); }, 350); }
    };
    const leave = () => { clearTimeout(holdT); btn.classList.remove('holding'); };
    btn.addEventListener('touchstart', down, { passive: false });
    btn.addEventListener('touchend', up);
    btn.addEventListener('touchmove', leave);
    btn.addEventListener('mousedown', down);
    btn.addEventListener('mouseup', up);
    btn.addEventListener('mouseleave', leave);
  };

  // ── 阻止下拉刷新 / 双指缩放 / 长按菜单等系统手势 ──
  document.addEventListener('gesturestart', e => e.preventDefault());
  document.addEventListener('contextmenu', e => e.preventDefault());
})();
