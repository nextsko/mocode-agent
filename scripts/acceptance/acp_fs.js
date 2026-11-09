const { spawn } = require('child_process');
const bin=process.argv[2], cwd=process.argv[3];
const child=spawn(bin,['acp','--yolo'],{cwd,stdio:['pipe','pipe','pipe']});
let buf='';let id=1;const pending=new Map();const updates=[];const writes={};
child.stdout.on('data',d=>{buf+=d.toString();let i;while((i=buf.indexOf('\n'))>=0){const line=buf.slice(0,i).trim();buf=buf.slice(i+1);if(!line)continue;let m;try{m=JSON.parse(line)}catch{continue}
if(m.method==='session/update'){updates.push(m.params);continue}
if(m.method==='fs/read_text_file'){
  const path=m.params.path||'';
  if(path.endsWith('/virtual/hello.txt')) child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result:{content:'HELLO_FS\nsecond line'}})+'\n');
  else child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:m.id,error:{code:-32000,message:'no virtual file '+path}})+'\n');
  continue;
}
if(m.method==='fs/write_text_file'){ writes[m.params.path]=m.params.content; child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result:{}})+'\n'); continue; }
if(m.method==='session/request_permission'){child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result:{outcome:{outcome:'selected',optionId:'allow-once'}}})+'\n');continue}
if(m.id&&pending.has(String(m.id))){const p=pending.get(String(m.id));pending.delete(String(m.id));m.error?p.reject(new Error(JSON.stringify(m.error))):p.resolve(m.result)}}});
child.stderr.on('data',d=>process.stderr.write('[stderr] '+d));
function call(method,params,timeout=180000){const i=id++;return new Promise((res,rej)=>{const t=setTimeout(()=>rej(new Error('timeout '+method)),timeout);pending.set(String(i),{resolve:v=>{clearTimeout(t);res(v)},reject:rej});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:i,method,params})+'\n')})}
(async()=>{
  await call('initialize',{protocolVersion:1,clientCapabilities:{fs:{readTextFile:true,writeTextFile:true},terminal:false}});
  const ns=await call('session/new',{cwd,mcpServers:[]});
  const pr=await call('session/prompt',{sessionId:ns.sessionId,prompt:[{type:'text',text:'请严格用 view 工具读取 /virtual/hello.txt，然后用 write 工具把它的内容原样写入 /virtual/out.txt。完成后只回复 DONE_FS。'}]});
  const text=updates.filter(u=>u.update.sessionUpdate==='agent_message_chunk').map(u=>u.update.content.text).join('');
  const written=writes['/virtual/out.txt']||'';
  console.log('stopReason='+pr.stopReason,'writes='+JSON.stringify(Object.keys(writes)),'writtenHasHello='+written.includes('HELLO_FS'));
  console.log('agentTail='+JSON.stringify(text.slice(-120)));
  child.kill();process.exit(pr.stopReason==='end_turn'&&written.includes('HELLO_FS')?0:1);
})().catch(e=>{console.error('ERR',e.message);child.kill();process.exit(2)});
