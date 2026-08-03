package main

// Payana: the pairing page served at /qr, and the terminal fallback.
//
// Self-contained by necessity — it is served by the bridge on loopback with no
// asset pipeline, and it has to keep working while the machine has no route to
// anything but WhatsApp itself. The QR stays dark-on-white in both themes
// because an inverted QR doesn't scan.
//
// This page, not the agent, owns linking. It presents both methods WhatsApp
// offers, says which one is ready, and tells the truth about whether what is on
// screen can still be scanned. An agent relaying any of that through chat is a
// second copy of the state that goes stale the moment the code rotates.

import (
	"os"

	"github.com/mdp/qrterminal"
)

func writeTerminalQR(code string) {
	qrterminal.GenerateHalfBlock(code, qrterminal.L, os.Stdout)
}

// payanaLogoSVG is the wordmark, inlined because the page is served with no
// asset pipeline and must render with no route to anything but WhatsApp.
const payanaLogoSVG = `<svg class="logo" viewBox="0 0 1616 352" role="img" aria-label="Payana" xmlns="http://www.w3.org/2000/svg">
<path d="M543.067 78.4233C548.071 70.7303 554.942 64.531 563.756 59.8256C572.569 55.1202 582.876 52.7301 594.677 52.7301C608.495 52.7301 620.968 56.1658 632.171 63.0372C643.375 69.9087 652.188 79.7677 658.536 92.6143C665.109 105.461 668.47 120.324 668.47 137.279C668.47 154.233 665.184 169.171 658.536 182.167C652.113 195.014 643.3 204.947 632.171 212.043C620.968 218.914 608.495 222.425 594.677 222.425C583.1 222.425 572.793 220.035 563.756 215.329C554.942 210.624 548.071 204.499 543.067 197.03V298.683H491.755V55.0455H543.067V78.4233ZM616.262 137.204C616.262 124.581 612.677 114.722 605.432 107.627C598.411 100.307 589.748 96.7222 579.366 96.7222C568.984 96.7222 560.469 100.382 553.299 107.627C546.278 114.946 542.768 124.88 542.768 137.503C542.768 150.125 546.278 160.059 553.299 167.378C560.469 174.698 569.208 178.283 579.366 178.283C589.523 178.283 598.262 174.623 605.432 167.378C612.602 159.909 616.262 149.826 616.262 137.204Z" fill="currentColor"/>
<path d="M676.164 137.204C676.164 120.249 679.376 105.386 685.799 92.6144C692.372 79.7678 701.26 69.9835 712.463 63.0373C723.667 56.1659 736.14 52.7302 749.957 52.7302C761.758 52.7302 772.065 55.1203 780.879 59.8257C789.916 64.5311 796.788 70.7304 801.568 78.4234V55.1203H852.879V220.035H801.568V196.732C796.564 204.425 789.543 210.624 780.58 215.329C771.767 220.035 761.459 222.425 749.659 222.425C736.065 222.425 723.667 218.989 712.463 212.118C701.26 205.022 692.372 195.088 685.799 182.242C679.376 169.246 676.164 154.233 676.164 137.279M801.568 137.503C801.568 124.88 797.983 114.947 790.738 107.627C783.717 100.307 775.128 96.7223 764.97 96.7223C754.812 96.7223 746.073 100.382 738.829 107.627C731.808 114.722 728.297 124.581 728.297 137.204C728.297 149.827 731.808 159.835 738.829 167.379C745.999 174.698 754.737 178.283 764.97 178.283C775.202 178.283 783.792 174.623 790.738 167.379C797.908 160.059 801.568 150.125 801.568 137.503Z" fill="currentColor"/>
<path d="M1052 55.046L946.987 298.235H891.792L930.182 214.359L862.065 55.046H919.352L958.116 158.192L996.507 55.046H1052Z" fill="currentColor"/>
<path d="M1051.33 137.204C1051.33 120.249 1054.54 105.386 1060.96 92.6144C1067.54 79.7678 1076.42 69.9835 1087.63 63.0373C1098.83 56.0912 1111.3 52.7302 1125.12 52.7302C1136.92 52.7302 1147.23 55.1203 1156.04 59.8257C1165.08 64.5311 1171.95 70.7304 1176.73 78.4234V55.1203H1228.04V220.035H1176.73V196.732C1171.73 204.425 1164.71 210.624 1155.75 215.329C1146.93 220.035 1136.62 222.425 1124.82 222.425C1111.23 222.425 1098.83 218.989 1087.63 212.118C1076.42 205.022 1067.54 195.088 1060.96 182.242C1054.54 169.246 1051.33 154.233 1051.33 137.279M1176.73 137.503C1176.73 124.88 1173.15 114.947 1165.9 107.627C1158.88 100.307 1150.29 96.7223 1140.13 96.7223C1129.98 96.7223 1121.24 100.382 1114.07 107.627C1107.05 114.722 1103.54 124.581 1103.54 137.204C1103.54 149.827 1107.05 159.835 1114.07 167.379C1121.24 174.698 1129.98 178.283 1140.13 178.283C1150.29 178.283 1158.96 174.623 1165.9 167.379C1173.07 160.059 1176.73 150.125 1176.73 137.503Z" fill="currentColor"/>
<path d="M1358.15 53.2535C1377.72 53.2535 1393.33 59.5274 1404.98 72.15C1416.78 84.5484 1422.68 101.727 1422.68 123.536V219.886H1371.67V130.333C1371.67 119.279 1368.76 110.764 1363.01 104.64C1357.18 98.5154 1349.41 95.4531 1339.63 95.4531C1329.85 95.4531 1322 98.5154 1316.25 104.64C1310.43 110.764 1307.59 119.279 1307.59 130.333V219.886H1256.28V55.0461H1307.59V76.9301C1312.82 69.6105 1319.76 63.9341 1328.58 59.7515C1337.39 55.4195 1347.25 53.2535 1358.3 53.2535" fill="currentColor"/>
<path d="M1439.12 137.204C1439.12 120.249 1442.33 105.386 1448.75 92.6144C1455.32 79.7678 1464.29 69.9835 1475.42 63.0373C1486.62 56.1659 1499.09 52.7302 1512.91 52.7302C1524.71 52.7302 1535.02 55.1203 1543.83 59.8257C1552.87 64.5311 1559.74 70.7304 1564.52 78.4234V55.1203H1615.83V220.035H1564.52V196.732C1559.52 204.425 1552.5 210.624 1543.53 215.329C1534.72 220.035 1524.41 222.425 1512.61 222.425C1499.02 222.425 1486.62 218.989 1475.42 212.118C1464.21 205.022 1455.32 195.088 1448.75 182.242C1442.33 169.246 1439.12 154.233 1439.12 137.279M1564.52 137.503C1564.52 124.88 1560.94 114.947 1553.69 107.627C1546.67 100.307 1538.08 96.7223 1527.92 96.7223C1517.77 96.7223 1509.03 100.382 1501.86 107.627C1494.84 114.722 1491.33 124.581 1491.33 137.204C1491.33 149.827 1494.84 159.835 1501.86 167.379C1509.03 174.698 1517.77 178.283 1527.92 178.283C1538.08 178.283 1546.74 174.623 1553.69 167.379C1560.86 160.059 1564.52 150.125 1564.52 137.503Z" fill="currentColor"/>
<path fill-rule="evenodd" clip-rule="evenodd" d="M401.083 8.44014H169.097C160.209 8.44014 152.367 14.3406 149.977 22.9299L126.674 104.79H81.3371C72.449 104.79 64.6066 110.69 62.2166 119.279L8.29076 308.244C4.63098 320.941 14.1912 333.638 27.4113 333.638H154.309C163.047 333.638 170.815 327.887 173.355 319.522L198.301 237.363H115.171C101.951 237.363 92.3911 224.741 96.0509 211.969L126.599 104.715H211.67C225.04 104.715 234.6 117.636 230.716 130.408L198.301 237.289H343.721C352.46 237.289 360.228 231.538 362.767 223.172L420.129 34.0586C424.012 21.2867 414.452 8.36545 401.083 8.36545V8.44014Z" fill="#0C1DCA"/>
</svg>`

const pairingPageHTML = `<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Vincular WhatsApp · Payana</title>
<style>
  /* Payana Brand Kit (Design System v1.8) design tokens: color-primary,
     color-text-muted-accessible, color-error, color-success. --warnbg is a
     light tint of color-error — the kit defines no explicit token for it. */
  :root{ --blue:#0c1dca; --ink:#030730; --card:#fff; --muted:#6b6d7a; --line:#e6e8f6;
         --ground:#0c1dca; --warn:#df6b10; --warnbg:#fbeee3; --ok:#00ab59; }
  @media (prefers-color-scheme:dark){ :root{ --ground:#030730; } }
  *{ box-sizing:border-box; }
  body{ margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
        padding:32px 20px; background:var(--ground); color:var(--ink);
        font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; }
  .card{ background:var(--card); border-radius:20px; padding:28px 30px 30px; width:min(430px,100%);
         box-shadow:0 18px 50px rgba(3,7,48,.28); text-align:center; }
  .logo{ display:block; height:22px; width:auto; margin:0 auto 18px; color:var(--ink); }
  .eyebrow{ text-transform:uppercase; letter-spacing:.14em; font-size:11px; font-weight:700;
            color:var(--blue); margin:0 0 4px; }
  h1{ font-size:21px; margin:0 0 4px; text-wrap:balance; }
  .lede{ font-size:13.5px; color:var(--muted); margin:0 0 18px; line-height:1.5; }

  .tabs{ display:flex; gap:6px; background:#f2f3fb; border-radius:12px; padding:4px; margin:0 0 18px; }
  .tabs button{ flex:1; border:0; background:none; border-radius:9px; padding:9px 6px; cursor:pointer;
     font:inherit; font-size:13px; font-weight:600; color:var(--muted); }
  .tabs button[aria-selected="true"]{ background:var(--card); color:var(--ink);
     box-shadow:0 1px 4px rgba(3,7,48,.14); }
  .tabs button .rdy{ display:inline-block; width:6px; height:6px; border-radius:50%;
     background:var(--ok); margin-left:5px; vertical-align:middle; }

  .qrwrap{ background:#fff; border:1px solid var(--line); border-radius:14px; padding:14px;
           display:inline-block; min-height:210px; min-width:210px; }
  .qrwrap img{ display:block; width:min(280px,72vw); height:auto; image-rendering:pixelated; }
  /* flex overrides the inline-block above, so text-align on the card no longer
     centres it — it needs its own auto margins. */
  .qrwrap.spent{ display:flex; align-items:center; justify-content:center; text-align:center;
           width:min(308px,78vw); margin-inline:auto; color:var(--muted); font-size:13.5px;
           line-height:1.5; padding:22px; }
  .qrwrap.spent img{ display:none; }

  .code{ font-family:ui-monospace,SFMono-Regular,Menlo,monospace; font-size:30px; font-weight:700;
         letter-spacing:.16em; margin:6px 0 2px; color:var(--ink); }
  .codebox{ border:1px dashed var(--blue); border-radius:14px; padding:16px 14px; }
  .codebox p{ margin:0; font-size:13px; color:var(--muted); }

  form{ display:flex; gap:8px; margin:4px 0 0; }
  input{ flex:1; min-width:0; font:inherit; font-size:15px; padding:11px 13px; color:var(--ink);
     border:1px solid var(--line); border-radius:11px; background:#fff; }
  input:focus{ outline:2px solid var(--blue); outline-offset:-1px; }
  /* radius-sm (6px) — the kit's button radius, not a pill. */
  .btn{ font:inherit; font-size:14px; font-weight:600; border:0; cursor:pointer; border-radius:6px;
     padding:11px 16px; background:var(--blue); color:#fff; }
  .btn[disabled]{ opacity:.45; cursor:default; }
  .btn.ghost{ background:#f2f3fb; color:var(--ink); }
  .hint{ font-size:12.5px; color:var(--muted); margin:10px 0 0; line-height:1.5; }
  .err{ font-size:12.5px; color:var(--warn); background:var(--warnbg); border-radius:10px;
        padding:9px 11px; margin:10px 0 0; line-height:1.45; text-align:left; }

  ol{ text-align:left; margin:18px 0 0; padding:0; list-style:none; counter-reset:s;
      display:flex; flex-direction:column; gap:9px; }
  ol li{ counter-increment:s; position:relative; padding-left:32px; font-size:13.5px; line-height:1.45; }
  ol li::before{ content:counter(s); position:absolute; left:0; top:-1px; width:22px; height:22px;
    border-radius:50%; background:var(--blue); color:#fff; font-size:11.5px; font-weight:700;
    display:flex; align-items:center; justify-content:center; }

  .status{ margin:16px 0 0; font-size:12.5px; color:var(--muted); line-height:1.5;
           display:flex; align-items:center; justify-content:center; gap:7px; }
  .dot{ width:8px; height:8px; border-radius:50%; background:var(--blue); flex:none; }
  .status.warn{ color:var(--warn); } .status.warn .dot{ background:var(--warn); }
  .status.ok{ color:var(--ok); } .status.ok .dot{ background:var(--ok); }
  .done p{ margin:0; font-size:14px; color:var(--muted); }
  .check{ font-size:44px; line-height:1; margin:0 0 10px; }
  [hidden]{ display:none !important; }
  @media (prefers-reduced-motion:no-preference){
    .dot{ animation:pulse 1.8s ease-in-out infinite; }
    @keyframes pulse{ 50%{ opacity:.35; } }
  }
</style>
</head>
<body>
<div class="card" id="card">
  ` + payanaLogoSVG + `
  <p class="eyebrow">WhatsApp · Bridge local</p>
  <h1>Vincula tu dispositivo</h1>
  <p class="lede">Elige el método que prefieras. Los dos vinculan la misma cuenta;
     el código por número no necesita cámara.</p>

  <div class="tabs" role="tablist">
    <button id="tabqr" role="tab" aria-selected="true" aria-controls="panelqr">Escanear QR</button>
    <button id="tabphone" role="tab" aria-selected="false" aria-controls="panelphone">Código por número<span class="rdy" id="rdy" hidden></span></button>
  </div>

  <section id="panelqr" role="tabpanel" aria-labelledby="tabqr">
    <div class="qrwrap" id="qrwrap"><img id="qr" alt="Código QR de vinculación de WhatsApp"><span id="qrmsg" hidden></span></div>
    <p id="qrretry" hidden><button class="btn" id="btnretry">Generar un código nuevo</button></p>
    <ol>
      <li>Abre <b>WhatsApp</b> en tu teléfono.</li>
      <li>Ve a <b>Ajustes → Dispositivos vinculados</b>.</li>
      <li>Toca <b>Vincular un dispositivo</b> y escanea el código de arriba.</li>
    </ol>
  </section>

  <section id="panelphone" role="tabpanel" aria-labelledby="tabphone" hidden>
    <div id="codebox" class="codebox" hidden>
      <p>Escribe este código en tu teléfono</p>
      <div class="code" id="paircode"></div>
    </div>
    <form id="phoneform">
      <input id="phone" type="tel" inputmode="numeric" autocomplete="tel"
             placeholder="57 300 123 4567" aria-label="Tu número con indicativo de país">
      <button class="btn" id="btnphone" type="submit">Pedir código</button>
    </form>
    <p class="hint" id="phonehint">Tu número con indicativo de país. WhatsApp te devuelve un
       código de 8 caracteres para escribir en el teléfono.</p>
    <p class="err" id="phoneerr" hidden></p>
    <ol>
      <li>Abre <b>WhatsApp</b> en tu teléfono.</li>
      <li>Ve a <b>Ajustes → Dispositivos vinculados</b>.</li>
      <li>Toca <b>Vincular con número de teléfono</b> y escribe el código.</li>
    </ol>
  </section>

  <p class="status" id="status"><span class="dot"></span><span id="statustext">Esperando el primer código…</span></p>
</div>
<script>
(function(){
  var token = new URLSearchParams(location.search).get('t') || '';
  var qs = function(id){ return document.getElementById(id); };
  var qr = qs('qr'), qrwrap = qs('qrwrap'), qrmsg = qs('qrmsg'), qrretry = qs('qrretry');
  var statusText = qs('statustext'), status = qs('status');
  var codebox = qs('codebox'), paircode = qs('paircode');
  var phoneErr = qs('phoneerr'), phoneForm = qs('phoneform'), btnPhone = qs('btnphone');
  var tabQR = qs('tabqr'), tabPhone = qs('tabphone');
  var panelQR = qs('panelqr'), panelPhone = qs('panelphone'), rdy = qs('rdy');
  var shownAt = -1, stopped = false, pickedTab = false;

  function auth(path){ return path + (path.indexOf('?') < 0 ? '?' : '&') + 't=' + encodeURIComponent(token); }

  function selectTab(which){
    var phone = which === 'phone';
    tabPhone.setAttribute('aria-selected', phone ? 'true' : 'false');
    tabQR.setAttribute('aria-selected', phone ? 'false' : 'true');
    panelPhone.hidden = !phone;
    panelQR.hidden = phone;
  }
  tabQR.onclick = function(){ pickedTab = true; selectTab('qr'); };
  tabPhone.onclick = function(){ pickedTab = true; selectTab('phone'); };

  function finish(html){
    stopped = true;
    var card = qs('card');
    card.className = 'card done';
    card.innerHTML = html;
  }
  function linked(){
    finish('<p class="check">✓</p><h1>Listo, quedó vinculado</h1>' +
      '<p>Ya puedes cerrar esta página. La sesión queda guardada, así que no vas a ' +
      'tener que volver a escanear.</p>');
  }
  function exhausted(){
    finish('<h1>Se agotaron los códigos</h1>' +
      '<p>El bridge dejó de pedirle códigos nuevos a WhatsApp para no ganarse un ' +
      'bloqueo temporal. Reinícialo cuando tengas el teléfono a mano y vuelve a ' +
      'abrir esta página.</p>');
  }

  // The QR is only ever shown while the bridge holds a live code. An expired
  // one is replaced outright — a stale QR on screen is what sends people to
  // check their wifi when the phone says "Check your connection".
  function showSpent(msg){
    qrwrap.className = 'qrwrap spent';
    qrmsg.hidden = false;
    qrmsg.textContent = msg;
    qr.removeAttribute('src');
    shownAt = -1;
  }
  function showQR(){
    qrwrap.className = 'qrwrap';
    qrmsg.hidden = true;
  }

  qs('btnretry').onclick = function(){
    var b = this;
    b.disabled = true;
    b.textContent = 'Pidiendo…';
    fetch(auth('/qr/retry'), {method:'POST', cache:'no-store'})
      .then(function(r){ return r.json(); })
      .then(function(res){
        if (res && res.error) { showSpent(res.error); }
        b.textContent = 'Generar un código nuevo';
        b.disabled = false;
      })
      .catch(function(){ b.textContent = 'Generar un código nuevo'; b.disabled = false; });
  };

  phoneForm.onsubmit = function(e){
    e.preventDefault();
    phoneErr.hidden = true;
    btnPhone.disabled = true;
    btnPhone.textContent = 'Pidiendo…';
    fetch(auth('/qr/pair-phone') + '&phone=' + encodeURIComponent(qs('phone').value), {
      method:'POST', cache:'no-store'
    })
      .then(function(r){ return r.json(); })
      .then(function(res){
        if (res && res.error) { phoneErr.hidden = false; phoneErr.textContent = res.error; }
      })
      .catch(function(){
        phoneErr.hidden = false;
        phoneErr.textContent = 'No se pudo hablar con el bridge — ¿sigue corriendo?';
      })
      .then(function(){
        btnPhone.disabled = false;
        btnPhone.textContent = codebox.hidden ? 'Pedir código' : 'Pedir otro código';
      });
  };

  function tick(){
    if (stopped) { return; }
    fetch(auth('/qr/status'), {cache:'no-store'})
      .then(function(r){ return r.ok ? r.json() : Promise.reject(r.status); })
      .then(function(s){
        if (s.linked) { linked(); return; }
        if (s.exhausted) { exhausted(); return; }

        // Shown verbatim: WhatsApp already returns it grouped (ABCD-EFGH), and
        // re-grouping it here mangles the separator it came with.
        if (s.pair_code) {
          paircode.textContent = s.pair_code;
          codebox.hidden = false;
          // The form stays: this code lives about three minutes, and asking for
          // the next one from here beats restarting the bridge to get one.
          btnPhone.textContent = 'Pedir otro código';
          qs('phonehint').textContent = 'El código dura unos 3 minutos. Si se vence, pide otro aquí o escanea el QR.';
          rdy.hidden = false;
          // A code that just arrived is the thing to act on, so surface it —
          // but never yank the user out of a tab they chose themselves.
          if (!pickedTab) { selectTab('phone'); }
        } else {
          codebox.hidden = true;
          rdy.hidden = true;
        }
        if (s.pair_error) { phoneErr.hidden = false; phoneErr.textContent = s.pair_error; }
        btnPhone.disabled = !s.can_pair_phone;

        // age_ms drops back to ~0 on every rotation: that is the signal to pull
        // a fresh PNG, instead of refetching an unchanged image every second.
        if (s.has_code && s.age_ms < shownAt) { shownAt = -1; }
        if (s.has_code && shownAt < 0) {
          qr.src = auth('/qr.png') + '&v=' + Date.now();
          shownAt = 0;
          showQR();
        }

        // The countdown belongs to the QR. Showing it under the 8-character code
        // would be a lie about a code that lives minutes, not seconds.
        if (!panelPhone.hidden && s.pair_code) {
          status.className = 'status ok';
          statusText.textContent = 'Código listo · escríbelo en el teléfono';
          if (s.has_code) { shownAt = s.age_ms; }
          qrretry.hidden = true;
        } else if (s.has_code) {
          shownAt = s.age_ms;
          var left = Math.max(0, Math.round((s.ttl_ms - s.age_ms)/1000));
          status.className = 'status';
          statusText.textContent = 'Código válido · se renueva en ' + left + ' s';
          qrretry.hidden = true;
        } else if (s.retry_in_ms > 0) {
          var secs = Math.ceil(s.retry_in_ms/1000);
          showSpent('El código venció. El bridge está pidiendo uno nuevo.');
          status.className = 'status warn';
          statusText.textContent = 'Código nuevo en ' + secs + ' s — o pídelo ahora';
          qrretry.hidden = false;
        } else if (s.expired) {
          // Mid-round rotation: the next code is seconds away. No button here —
          // a press during a round is only consumed at the NEXT backoff, where
          // it cancels a wait that exists to keep WhatsApp from throttling the
          // account. The button belongs to the between-rounds branch above.
          showSpent('El código venció. Llega uno nuevo en un momento…');
          status.className = 'status warn';
          statusText.textContent = 'Renovando el código…';
          qrretry.hidden = true;
        } else {
          status.className = 'status';
          statusText.textContent = 'Pidiendo un código nuevo…';
          qrretry.hidden = true;
        }
      })
      .catch(function(){
        status.className = 'status warn';
        statusText.textContent = 'Sin conexión con el bridge — ¿sigue corriendo?';
      });
  }

  tick();
  setInterval(tick, 1000);
})();
</script>
</body>
</html>
`
