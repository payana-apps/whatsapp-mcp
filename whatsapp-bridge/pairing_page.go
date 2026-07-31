package main

// Payana: the pairing page served at /qr, and the terminal fallback.
//
// Self-contained by necessity — it is served by the bridge on loopback with no
// asset pipeline, and it has to keep working while the machine has no route to
// anything but WhatsApp itself. The QR stays dark-on-white in both themes
// because an inverted QR doesn't scan.

import (
	"os"

	"github.com/mdp/qrterminal"
)

func writeTerminalQR(code string) {
	qrterminal.GenerateHalfBlock(code, qrterminal.L, os.Stdout)
}

const pairingPageHTML = `<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Vincular WhatsApp · Payana</title>
<style>
  :root{ --blue:#0E22F1; --ink:#030730; --card:#fff; --muted:#5b6478; --line:#e6e8f6; --ground:#0E22F1; }
  @media (prefers-color-scheme:dark){ :root{ --ground:#030730; } }
  *{ box-sizing:border-box; }
  body{ margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
        padding:32px 20px; background:var(--ground); color:var(--ink);
        font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; }
  .card{ background:var(--card); border-radius:20px; padding:30px; width:min(420px,100%);
         box-shadow:0 18px 50px rgba(3,7,48,.28); text-align:center; }
  .eyebrow{ text-transform:uppercase; letter-spacing:.14em; font-size:11px; font-weight:700;
            color:var(--blue); margin:0 0 4px; }
  h1{ font-size:21px; margin:0 0 18px; text-wrap:balance; }
  .qrwrap{ background:#fff; border:1px solid var(--line); border-radius:14px; padding:14px;
           display:inline-block; min-height:200px; min-width:200px; }
  .qrwrap img{ display:block; width:min(280px,72vw); height:auto; image-rendering:pixelated; }
  .code{ font-family:ui-monospace,SFMono-Regular,Menlo,monospace; font-size:30px; font-weight:700;
         letter-spacing:.16em; margin:6px 0 2px; color:var(--ink); }
  .codebox{ border:1px dashed var(--blue); border-radius:14px; padding:14px; margin:0 0 18px; }
  .codebox p{ margin:0; font-size:13px; color:var(--muted); }
  ol{ text-align:left; margin:20px 0 0; padding:0; list-style:none; counter-reset:s;
      display:flex; flex-direction:column; gap:10px; }
  ol li{ counter-increment:s; position:relative; padding-left:34px; font-size:14px; line-height:1.45; }
  ol li::before{ content:counter(s); position:absolute; left:0; top:-1px; width:23px; height:23px;
    border-radius:50%; background:var(--blue); color:#fff; font-size:12px; font-weight:700;
    display:flex; align-items:center; justify-content:center; }
  .status{ margin:18px 0 0; font-size:12px; color:var(--muted); line-height:1.5;
           display:flex; align-items:center; justify-content:center; gap:7px; }
  .dot{ width:8px; height:8px; border-radius:50%; background:var(--blue); flex:none; }
  .ok .dot{ background:#12A150; }
  .done h1{ margin-bottom:6px; }
  .done p{ margin:0; font-size:14px; color:var(--muted); }
  .check{ font-size:44px; line-height:1; margin:0 0 10px; }
  @media (prefers-reduced-motion:no-preference){
    .dot{ animation:pulse 1.8s ease-in-out infinite; }
    @keyframes pulse{ 50%{ opacity:.35; } }
  }
</style>
</head>
<body>
<div class="card" id="card">
  <p class="eyebrow">WhatsApp · Bridge local</p>
  <h1>Vincula tu dispositivo</h1>
  <div id="codebox" class="codebox" hidden>
    <p>Escribe este código en tu teléfono</p>
    <div class="code" id="paircode"></div>
    <p>Ajustes → Dispositivos vinculados → Vincular con número de teléfono</p>
  </div>
  <div class="qrwrap"><img id="qr" alt="Código QR de vinculación de WhatsApp"></div>
  <ol>
    <li>Abre <b>WhatsApp</b> en tu teléfono.</li>
    <li>Ve a <b>Ajustes → Dispositivos vinculados</b>.</li>
    <li>Toca <b>Vincular un dispositivo</b> y escanea.</li>
  </ol>
  <p class="status" id="status"><span class="dot"></span><span id="statustext">Esperando el primer código…</span></p>
</div>
<script>
(function(){
  var token = new URLSearchParams(location.search).get('t') || '';
  var qr = document.getElementById('qr');
  var statusText = document.getElementById('statustext');
  var status = document.getElementById('status');
  var codebox = document.getElementById('codebox');
  var paircode = document.getElementById('paircode');
  var shownAt = -1;

  function linked(){
    document.getElementById('card').className = 'card done';
    document.getElementById('card').innerHTML =
      '<p class="check">✓</p><h1>Listo, quedó vinculado</h1>' +
      '<p>Ya puedes cerrar esta página. La sesión queda guardada, así que no vas a ' +
      'tener que volver a escanear.</p>';
  }

  function tick(){
    fetch('/qr/status?t=' + encodeURIComponent(token), {cache:'no-store'})
      .then(function(r){ return r.ok ? r.json() : Promise.reject(r.status); })
      .then(function(s){
        if (s.linked) { linked(); return; }

        if (s.pair_code) {
          paircode.textContent = s.pair_code.replace(/(.{4})(?=.)/g, '$1-');
          codebox.hidden = false;
        }

        // age_ms drops back to ~0 on every rotation: that is the signal to pull
        // a fresh PNG, instead of refetching an unchanged image every second.
        if (s.has_code && s.age_ms < shownAt) { shownAt = -1; }
        if (s.has_code && shownAt < 0) {
          qr.src = '/qr.png?t=' + encodeURIComponent(token) + '&v=' + Date.now();
          shownAt = 0;
        }
        if (s.has_code) {
          shownAt = s.age_ms;
          var left = Math.max(0, Math.round((s.ttl_ms - s.age_ms)/1000));
          status.className = 'status';
          statusText.textContent = 'Código válido · se renueva en ' + left + ' s';
        } else {
          statusText.textContent = 'Pidiendo un código nuevo…';
        }
      })
      .catch(function(){ statusText.textContent = 'Sin conexión con el bridge — ¿sigue corriendo?'; });
  }

  tick();
  setInterval(tick, 1000);
})();
</script>
</body>
</html>
`
