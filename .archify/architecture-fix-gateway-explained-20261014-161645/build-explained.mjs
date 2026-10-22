import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';

const directory=path.dirname(fileURLToPath(import.meta.url));
const model=JSON.parse(fs.readFileSync(path.join(directory,'candidate.json'),'utf8'));
const descriptions=JSON.parse(fs.readFileSync(path.join(directory,'explanations.json'),'utf8'));
const native=fs.readFileSync(path.join(directory,'fix-gateway-diagram.html'),'utf8');
const revisedSummary=path.join(directory,'review-2','fix-gateway-diagram.finalize-summary.json');
const summary=JSON.parse(fs.readFileSync(fs.existsSync(revisedSummary)?revisedSummary:path.join(directory,'fix-gateway-diagram.finalize-summary.json'),'utf8'));
if(!summary.ok||Object.values(summary.gates).some(x=>x!=='pass')) throw new Error('The native diagram must pass finalize before packaging.');
if(createHash('sha256').update(native).digest('hex')!==summary.artifact.sha256) throw new Error('The receipt does not match the diagram being embedded.');
const ids=model.components.map(x=>x.id);
if(ids.length!==descriptions.length||ids.some(id=>descriptions.filter(x=>x.id===id).length!==1)) throw new Error('Each diagram node needs exactly one explanation.');
const data=descriptions.map((description,index)=>({...description,index:index+1,sources:model.components.find(x=>x.id===description.id).sources}));
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const safeJSON=value=>JSON.stringify(value).replace(/</g,'\\u003c');
const options=data.map(x=>'<option value="'+esc(x.id)+'">'+String(x.index).padStart(2,'0')+' · '+esc(x.label)+'</option>').join('');
const cards=data.map(x=>'<details class="reference-card" id="explanation-'+esc(x.id)+'"><summary>'+String(x.index).padStart(2,'0')+' · '+esc(x.label)+'</summary><p>'+esc(x.what)+'</p><p><strong>Получает:</strong> '+esc(x.input)+'</p><p><strong>Передаёт:</strong> '+esc(x.output)+'</p><p class="reference-note">'+esc(x.important)+'</p></details>').join('');
const html=`<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FIX_GateWay — схема с объяснением каждого блока</title>
<style>
:root{color-scheme:light;--ink:#16243a;--muted:#526279;--line:#dce4ed;--blue:#195fb8;--surface:#fff;--background:#f3f6fa}
*{box-sizing:border-box} [hidden]{display:none!important}
body{margin:0;background:var(--background);color:var(--ink);font:16px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif}
header{max-width:1800px;margin:auto;padding:26px 28px 16px}
.kicker{font-size:12px;letter-spacing:.12em;font-weight:750;color:var(--blue)}
h1{font-size:clamp(24px,3vw,36px);line-height:1.15;letter-spacing:-.03em;margin:8px 0}
.lead{margin:8px 0 18px;color:var(--muted);max-width:950px}
.toolbar{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
select,button{font:inherit;border:1px solid var(--line);border-radius:9px;padding:9px 13px;background:#fff;color:var(--ink)}
select{min-width:300px;max-width:100%}
button,a,summary{cursor:pointer} button:hover,button:focus-visible{border-color:var(--blue)}
button:focus-visible,select:focus-visible,a:focus-visible,summary:focus-visible{outline:3px solid #86b9fa;outline-offset:3px}
.toolbar a{font-size:14px;color:var(--blue)}
.overview{margin:18px 0 0;padding:13px 16px;background:#eaf2fe;border:1px solid #d4e4fa;border-radius:10px;font-size:14px}
main{max-width:1800px;margin:0 auto;padding:0 14px 30px}
iframe{display:block;width:100%;border:1px solid var(--line);border-radius:16px;background:#fff;height:1100px}
.drawer{position:fixed;right:24px;top:24px;width:min(420px,calc(100vw - 32px));max-height:calc(100vh - 48px);overflow:auto;background:#fff;border:1px solid #cbd9ea;border-radius:16px;box-shadow:0 18px 60px #18324b38;z-index:50;padding:23px}
.drawer.left{right:auto;left:24px}
.drawer-top{display:flex;justify-content:space-between;align-items:start;gap:12px}
.drawer-top button{padding:5px 11px;flex:none} .drawer h2{font-size:22px;line-height:1.3;margin:6px 0}
.role{font-size:12px;font-weight:650;color:var(--muted);margin:0 0 14px}
.drawer p{margin:10px 0}.io{margin:16px 0;padding:13px 15px;background:#f4f7fb;border-radius:10px}
.io dt{font-size:11px;font-weight:750;letter-spacing:.08em;text-transform:uppercase;color:#6b7890}.io dd{margin:3px 0 13px;font-size:14px}.io dd:last-child{margin-bottom:0}
.important{font-size:14px;border-left:3px solid #6e96cd;padding-left:12px;color:#384e69}
.sources{margin-top:16px;padding-top:13px;border-top:1px solid var(--line);font-size:12px}
.sources a{display:block;color:var(--blue);overflow-wrap:anywhere;margin:5px 0}
.drawer-tip{font-size:12px;color:var(--muted);margin:15px 0 0!important}
.reference{padding:25px 14px 0;max-width:1200px;margin:auto}
.reference h2{font-size:22px}.reference-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;align-items:start}
.reference-card{background:#fff;border:1px solid var(--line);border-radius:11px;padding:13px 16px}
.reference-card summary{font-weight:650}.reference-card p{font-size:14px}.reference-note{color:var(--muted)}
.audit{padding:18px;margin:24px 0;background:#fff;border:1px solid var(--line);border-radius:12px;font-size:14px}
.audit summary{font-weight:700}.audit li{margin:7px 0}.version{font-size:12px;color:var(--muted)}
@media(max-width:700px){header{padding:20px 16px 14px}.reference-grid{grid-template-columns:1fr}.drawer{right:16px;top:16px;max-height:calc(100vh - 32px)}.drawer.left{left:16px}iframe{border-radius:10px}select{min-width:0;width:100%}}
@media print{.toolbar,.drawer{display:none!important}.reference-card{break-inside:avoid}.reference-card p{display:block}.reference-card{content-visibility:visible}}
</style></head>
<body>
<header>
<div class="kicker">FIX_GATEWAY · ARCHIFY</div>
<h1>Как проходит торговая заявка</h1>
<p class="lead">Нажмите любой блок на схеме — появится объяснение простыми словами. В карточке указано, что блок получает, что делает и что передаёт дальше.</p>
<div class="toolbar"><select id="node-select" aria-label="Выбрать блок для объяснения"><option value="">Объяснение блока…</option>${options}</select><button id="close-detail" type="button">Закрыть объяснение</button><a href="fix-gateway-diagram.html" target="_blank" rel="noopener">Открыть обычную схему Archify ↗</a><a href="#reference">Все пояснения ниже</a></div>
<p class="overview"><strong>Главный путь:</strong> брокер → проверка заявки → сохранение в БД → очередь отправки → FIX → ARQA → QUIK.<br><strong>Обратный путь:</strong> FIX-отчёт → запись на диск → обновление заявки и сделки → события клиентам.</p>
</header>
<main>
<iframe id="diagram" title="Интерактивная архитектура FIX_GateWay" allow="clipboard-write; fullscreen"></iframe>
<section class="reference" id="reference" aria-labelledby="reference-title"><h2 id="reference-title">Что означает каждый блок</h2><p>Зелёные блоки — части одного Go-процесса Gateway. Постоянные хранилища и внешние участники выделены отдельно.</p><div class="reference-grid">${cards}</div>
<details class="audit" id="audit"><summary>Что исправлено и сверено с кодом</summary>
<ul><li>У всех 13 блоков есть краткая роль на рисунке и подробное объяснение по нажатию.</li><li>Подписи стрелок переведены на понятный русский язык. Заявка и очередь сохраняются вместе; уведомление worker идёт после успешного сохранения.</li><li>Worker фиксирует попытку до SendOrder. Успешный возврат этого вызова не означает принятие в QUIK.</li><li>Go FIX-адаптер отделён от внешнего ARQA FIX Service. Возврат результата QUIK → ARQA отмечен как контракт внешнего контура.</li><li>Inbox — локальные файлы с синхронизацией на диск. Hub — рассылка в памяти без воспроизведения пропущенных событий.</li><li>События появляются после записи состояния. Admin UI перечитывает REST после переподключения; broker-app использует REST-опрос.</li><li>Показанные пути сверены с закоммиченным кодом и актуальными изменениями main.go/manager.go: эти изменения не меняют показанный путь заявок и исполнений.</li></ul>
<p class="version">Источники SRC закреплены за коммитом ${esc(model.meta.repository.revision.slice(0,7))}. Незакоммиченные правки справочников, market data и настроек не выдаются за содержимое этого коммита. Внутренняя обработка и живые подключения ARQA/QUIK не проверялись.</p>
</details></section>
</main>
<aside class="drawer" id="detail" hidden aria-labelledby="detail-title" aria-live="polite">
<div class="drawer-top"><div><div class="kicker" id="detail-number"></div><h2 id="detail-title"></h2></div><button id="drawer-close" type="button" aria-label="Закрыть объяснение">×</button></div>
<p class="role" id="detail-role"></p><p id="detail-what"></p><dl class="io"><dt>Получает</dt><dd id="detail-input"></dd><dt>Передаёт</dt><dd id="detail-output"></dd></dl><p class="important" id="detail-important"></p><div class="sources" id="detail-sources"></div><p class="drawer-tip">Нажмите другой блок, чтобы сменить объяснение. Esc или × закрывают карточку.</p>
</aside>
<script type="application/json" id="explanation-data">${safeJSON(data)}</script>
<script type="application/json" id="native-reader">${safeJSON(native)}</script>
<script>
const entries=JSON.parse(document.getElementById('explanation-data').textContent);
const byId=new Map(entries.map(x=>[x.id,x]));
const frame=document.getElementById('diagram');
const detail=document.getElementById('detail');
const picker=document.getElementById('node-select');
const repository=${safeJSON(model.meta.repository)};
let lastActivationSide='right';
function closeDetail(resetNative=true){
 detail.hidden=true;detail.dataset.nodeId='';picker.value='';
 if(resetNative&&frame.contentDocument){frame.contentDocument.dispatchEvent(new frame.contentWindow.KeyboardEvent('keydown',{key:'Escape',bubbles:true}));}
}
function showDetail(id,side){
 const entry=byId.get(id);if(!entry)return;
 detail.dataset.nodeId=id;detail.hidden=false;
 detail.classList.toggle('left',side==='left');
 picker.value=id;
 for(const [target,value] of Object.entries({'detail-number':'БЛОК '+String(entry.index).padStart(2,'0')+' / '+entries.length,'detail-title':entry.label,'detail-role':entry.role,'detail-what':entry.what,'detail-input':entry.input,'detail-output':entry.output,'detail-important':entry.important})){document.getElementById(target).textContent=value;}
 const sources=document.getElementById('detail-sources');sources.replaceChildren();
 const heading=document.createElement('strong');heading.textContent='Проверенные источники';sources.append(heading);
 for(const source of entry.sources){
  const anchor=document.createElement('a');
  anchor.href=repository.url.replace(/\\.git$/,'')+'/blob/'+repository.revision+'/'+source.path.split('/').map(encodeURIComponent).join('/')+'#L'+source.line+(source.end_line?'-L'+source.end_line:'');
  anchor.textContent=source.label+' · '+source.path+':'+source.line;
  anchor.target='_blank';anchor.rel='noopener noreferrer';sources.append(anchor);
 }
 detail.scrollTop=0;
}
function activateNode(id){
 const doc=frame.contentDocument;if(!doc)return;
 const node=[...doc.querySelectorAll('svg [data-node-id]')].find(x=>x.getAttribute('data-node-id')===id);
 if(node){node.dispatchEvent(new frame.contentWindow.MouseEvent('click',{bubbles:true}));}
 else showDetail(id,'right');
}
picker.addEventListener('change',()=>picker.value?activateNode(picker.value):closeDetail());
document.getElementById('drawer-close').addEventListener('click',closeDetail);
document.getElementById('close-detail').addEventListener('click',closeDetail);
document.addEventListener('keydown',event=>{if(event.key==='Escape')closeDetail();});
frame.addEventListener('load',()=>{
 const doc=frame.contentDocument;if(!doc)return;
 doc.documentElement.setAttribute('data-reading-link-template',location.href.split('#')[0]+'#reader={state}');
 doc.addEventListener('click',event=>{
  const node=event.target.closest?.('svg [data-node-id]');if(!node)return;
  const id=node.getAttribute('data-node-id');if(!byId.has(id))return;
  lastActivationSide=node.getBoundingClientRect().left>frame.clientWidth/2?'left':'right';
  showDetail(id,lastActivationSide);
 });
 doc.addEventListener('keydown',event=>{
  if(event.key==='Escape'){closeDetail(false);return;}
  if(event.key!=='Enter'&&event.key!==' ')return;
  const node=event.target.closest?.('svg [data-node-id]');
  if(node)showDetail(node.getAttribute('data-node-id'),'right');
 });
 let resizing=false;
 const fit=()=>{
  if(resizing)return;resizing=true;
  requestAnimationFrame(()=>{
   const height=Math.max(1000,doc.documentElement.scrollHeight,doc.body.scrollHeight);
   if(Math.abs(frame.clientHeight-height)>2)frame.style.height=height+'px';
   resizing=false;
  });
 };
 new ResizeObserver(fit).observe(doc.body);fit();
 const state=new URLSearchParams(location.hash.slice(1)).get('reader');
 if(state)frame.contentWindow.location.hash=state;
});
frame.srcdoc=JSON.parse(document.getElementById('native-reader').textContent);
</script></body></html>`;
const target=path.join(directory,'fix-gateway-explained.html');
fs.writeFileSync(target,html,'utf8');
console.log(JSON.stringify({output:target,explanations:data.length,nativeFinalized:true,bytes:Buffer.byteLength(html)}));

