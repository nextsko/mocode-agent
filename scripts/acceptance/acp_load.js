const { spawn } = require('child_process');
const bin=process.argv[2], cwd=process.argv[3];
const child=spawn(bin,['acp','--yolo'],{cwd,stdio:['pipe','pipe','pipe']});
let buf='';let id=1;const pending=new Map();const updates=[];
child.stdout.on('data',d=>{buf+=d.toString();let i;while((i=buf.indexOf('\n'))>=0){const line=buf.slice(0,i).trim();buf=buf.slice(i+1);if(!line)continue;let m;try{m=JSON.parse(line)}catch{continue}
if(m.method==='session/update'){updates.push(m.params);continue}
if(m.method==='session/request_permission'){child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result:{outcome:{outcome:'selected',optionId:'allow-once'}}})+'\n');continue}
if(m.id&&pending.has(String(m.id))){const p=pending.get(String(m.id));pending.delete(String(m.id));m.error?p.reject(new Error(JSON.stringify(m.error))):p.resolve(m.result)}}});
child.stderr.on('data',d=>process.stderr.write('[stderr] '+d));
function call(method,params,timeout=180000){const i=id++;return new Promise((res,rej)=>{const t=setTimeout(()=>rej(new Error('timeout '+method)),timeout);pending.set(String(i),{resolve:v=>{clearTimeout(t);res(v)},reject:rej});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:i,method,params})+'\n')})}
(async()=>{
  await call('initialize',{protocolVersion:1,clientCapabilities:{fs:{readTextFile:false,writeTextFile:false},terminal:false}});
  const ns=await call('session/new',{cwd,mcpServers:[]});
  await call('session/prompt',{sessionId:ns.sessionId,prompt:[{type:'text',text:'只回复：第二回合'}]});
  const before=updates.length;
  const loaded=await call('session/load',{sessionId:ns.sessionId,cwd,mcpServers:[]});
  const replay=updates.slice(before);
  const kinds=replay.map(u=>u.update.sessionUpdate);
  console.log('loadSession='+(loaded.sessionId===ns.sessionId),'replayed='+replay.length,'kinds='+JSON.stringify([...new Set(kinds)]));
  console.log('hasUser='+kinds.includes('user_message_chunk'),'hasAgent='+kinds.includes('agent_message_chunk'));
  child.kill();process.exit(loaded.sessionId===ns.sessionId&&kinds.includes('user_message_chunk')&&kinds.includes('agent_message_chunk')?0:1);
})().catch(e=>{console.error('ERR',e.message);child.kill();process.exit(2)});
