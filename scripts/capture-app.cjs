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
 await send('Emulation.clearDeviceMetricsOverride');
 await fs.writeFile(path.join(output,`visual-stage${ready.stage}.json`),JSON.stringify(metrics,null,2));
 await evaluate('globalThis.__asimiAcceptanceContinue=true');socket.close();
 console.log(JSON.stringify(metrics));
 if(metrics.some(x=>x.noHorizontalOverflow===false))process.exitCode=1;
})().catch(e=>{console.error(e);process.exitCode=1});
