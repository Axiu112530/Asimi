// Captures only the app's own WebView render via loopback CDP, never the desktop.
const fs=require('node:fs/promises');
const path=require('node:path');
const port=Number(process.argv[2]);const output=process.argv[3];
const pause=ms=>new Promise(r=>setTimeout(r,ms));
(async()=>{
 let target;
 for(let attempt=0;attempt<80;attempt++){try{const list=await fetch(`http://127.0.0.1:${port}/json/list`).then(r=>r.json());target=list.find(x=>x.type==='page'&&x.url.includes('acceptance=1'));if(target)break}catch{}await pause(150)}
 if(!target)throw new Error('App WebView debugging target not found');
 const socket=new WebSocket(target.webSocketDebuggerUrl);await new Promise((r,j)=>{socket.onopen=r;socket.onerror=j});
 let id=0;const pending=new Map();socket.onmessage=e=>{const v=JSON.parse(e.data);const p=pending.get(v.id);if(p){pending.delete(v.id);v.error?p.reject(v.error):p.resolve(v.result)}};
 const send=(method,params={})=>new Promise((resolve,reject)=>{const key=++id;pending.set(key,{resolve,reject});socket.send(JSON.stringify({id:key,method,params}))});
 const evaluate=async expression=>(await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true})).result?.value;
 let ready;for(let attempt=0;attempt<180;attempt++){ready=await evaluate('globalThis.__asimiAcceptanceReady');if(ready)break;await pause(150)}
 if(!ready)throw new Error('Acceptance did not reach visual review');
 await fs.mkdir(output,{recursive:true});
 const metrics=[];
 for(const [label,width,height] of [['desktop',1380,820],['compact',1000,700],['minimum',900,620]]){
  await send('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile:false});await pause(200);
  const dimensions=await evaluate('({width:innerWidth,height:innerHeight,scrollWidth:document.documentElement.scrollWidth,language:document.documentElement.lang})');
  metrics.push({label,...dimensions,noHorizontalOverflow:dimensions.scrollWidth<=width});
  const shot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(path.join(output,`stage${ready.stage}-${label}.png`),Buffer.from(shot.data,'base64'));
 }
 await send('Emulation.setDeviceMetricsOverride',{width:1380,height:820,deviceScaleFactor:1,mobile:false});
 await evaluate(`(async()=>{const {Call}=await import('/wails/runtime.js');const s=await Call.ByName('main.DesktopService.Call','GetSettings','{}');return Call.ByName('main.DesktopService.Call','UpdateSettings',JSON.stringify({...s.data,theme:'dark'}))})()`);
 await pause(250);
 const dark=await evaluate("document.documentElement.dataset.theme==='dark'");
 if(!dark)throw new Error('Dark theme did not update from settings event');
 const darkShot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(path.join(output,`stage${ready.stage}-dark.png`),Buffer.from(darkShot.data,'base64'));
 await evaluate(`(async()=>{const {Call}=await import('/wails/runtime.js');const s=await Call.ByName('main.DesktopService.Call','GetSettings','{}');return Call.ByName('main.DesktopService.Call','UpdateSettings',JSON.stringify({...s.data,theme:'light'}))})()`);
 metrics.push({label:'darkTheme',applied:dark});
 if (ready.stage === 15) {
  for (const [page, selector] of [
    ['home', '[data-testid="product-home"]'],
    ['goals', '[data-testid="unified-page"]'],
    ['connections', '[data-testid="connections-page"]'],
    ['settings', '.settings-page'],
    ['models', '.preset-provider-page'],
    ['usage', '[data-testid="ai-usage"]'],
    ['computer', '[data-testid="computer-page"]'],
    ['skills', '[data-testid="cognition-skills"]'],
    ['memory', '[data-testid="cognition-memory"]'],
    ['learning', '[data-testid="cognition-learning"]'],
    ['growth', '[data-testid="cognition-growth"]'],
  ]) {
   await evaluate(`document.querySelector('[data-testid="nav-${page}"]').click()`);
   await pause(200);
   if (page === 'settings') {
    await evaluate("document.querySelector('[data-testid=preset-advanced]').open=true");
    await pause(180);
    await evaluate("document.querySelector('.preset-provenance').open=true");
   }
   for (const [label, width, height] of [['wide',1380,820],['compact',1000,700],['narrow',720,620]]) {
    await send('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile:false});
    await pause(150);
    const dimensions = await evaluate(`(() => {
     const root = document.querySelector(${JSON.stringify(selector)});
     if (!root) return null;
     const workspace = document.querySelector('.workspace');
     const main = root.closest('.page-scroll') || root.closest('.product-page') || root;
     const scroller = root.closest('.cognition-skills-route') || main;
     const checkPairs = [...root.querySelectorAll('.cognition-check')].map(row => {
       const box = row.querySelector('input').getBoundingClientRect();
       const text = row.querySelector('span').getBoundingClientRect();
       return { centerDelta: Math.abs((box.top + box.bottom - text.top - text.bottom) / 2), boxHeight: box.height };
     });
     const toolbar = root.querySelector('.cognition-toolbar');
     const select = toolbar?.querySelector('select')?.getBoundingClientRect();
     const button = toolbar?.querySelector('button')?.getBoundingClientRect();
     const toolbarAligned = !select || !button || (Math.abs(select.bottom - button.bottom) <= 1 &&
       Math.abs(select.height - button.height) <= 1);
     const controlsPresent = root.dataset.testid !== 'cognition-memory' || (checkPairs.length === 4 && select && button);
     const alignment = !!controlsPresent && toolbarAligned && checkPairs.every(row => row.centerDelta <= 1 && row.boxHeight <= 18);
     return { width: innerWidth, pageWidth: main.clientWidth, pageScrollWidth: main.scrollWidth,
       alignment, toolbarAligned, checkPairs,
       scrollContainerFits: scroller.scrollWidth <= scroller.clientWidth + 1,
       shellFits: workspace.scrollWidth <= workspace.clientWidth + 1 };
    })()`);
    if (!dimensions) throw new Error(`Page ${page} did not render`);
    metrics.push({label:`${page}-${label}`,...dimensions,
      noHorizontalOverflow:dimensions.shellFits && dimensions.scrollContainerFits && dimensions.pageScrollWidth<=dimensions.pageWidth+1});
    if (page === 'settings' && label === 'narrow') {
     await evaluate("document.querySelector('[data-testid=preset-limit-requests]').scrollIntoView({block:'center'})");
    }
    const shot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});
    await fs.writeFile(path.join(output,`stage${ready.stage}-${page}-${label}.png`),Buffer.from(shot.data,'base64'));
   }
  }
 }
 if (ready.stage === 15) {
  await evaluate(`(async()=>{const {Call}=await import('/wails/runtime.js');const s=await Call.ByName('main.DesktopService.Call','GetSettings','{}');return Call.ByName('main.DesktopService.Call','UpdateSettings',JSON.stringify({...s.data,interfaceMode:'standard'}))})()`);
  await pause(180);
  for (const page of ['goals','connections','computer','learning','growth']) {
   await evaluate(`document.querySelector('[data-testid="nav-${page}"]').click()`);
   await pause(180);
   const ordinary = await evaluate(`!document.querySelector('.computer-capture-details') && !document.querySelector('[data-testid=cognition-learning-open-tasks]') && !document.querySelector('[data-testid=nav-developer]') && !document.querySelector('[data-testid=nav-playground]') && !!document.querySelector('[data-testid=nav-goals]') && !!document.querySelector('[data-testid=nav-connections]')`);
   if (!ordinary) throw new Error('Standard view hides ordinary navigation or exposes capture diagnostics');
   metrics.push({label:'standard-'+page,ordinaryFeaturesAvailable:ordinary});
   for (const [label,width,height] of [['wide',1380,820],['compact',1000,700],['narrow',720,620]]) {
    await send('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile:false});
    await pause(130);
    const dimensions=await evaluate(`(() => {
      const root=document.querySelector('[data-testid="${page==='goals'?'unified-page':page==='connections'?'connections-page':page==='computer'?'computer-page':page==='learning'?'cognition-learning':'cognition-growth'}"]');
      const workspace=document.querySelector('.workspace');
      return root&&workspace?{width:innerWidth,pageWidth:root.clientWidth,pageScrollWidth:root.scrollWidth,shellFits:workspace.scrollWidth<=workspace.clientWidth+1}:null;
    })()`);
    if(!dimensions)throw new Error('Standard '+page+' did not render');
    metrics.push({label:`standard-${page}-${label}`,...dimensions,noHorizontalOverflow:dimensions.shellFits&&dimensions.pageScrollWidth<=dimensions.pageWidth+1});
    const shot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});
    await fs.writeFile(path.join(output,`stage15-standard-${page}-${label}.png`),Buffer.from(shot.data,'base64'));
   }
  }
  await evaluate(`(async()=>{const {Call}=await import('/wails/runtime.js');const s=await Call.ByName('main.DesktopService.Call','GetSettings','{}');return Call.ByName('main.DesktopService.Call','UpdateSettings',JSON.stringify({...s.data,interfaceMode:'developer'}))})()`);
 }
 await send('Emulation.clearDeviceMetricsOverride');
 await fs.writeFile(path.join(output,`visual-stage${ready.stage}.json`),JSON.stringify(metrics,null,2));
 await evaluate('globalThis.__asimiAcceptanceContinue=true');socket.close();
 console.log(JSON.stringify(metrics));
 if(metrics.some(x=>x.noHorizontalOverflow===false || x.alignment===false))process.exitCode=1;
})().catch(e=>{console.error(e);process.exitCode=1});
