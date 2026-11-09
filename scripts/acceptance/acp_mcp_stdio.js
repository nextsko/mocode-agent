const { spawn } = require('child_process');
const bin=process.argv[2], cwd=process.argv[3], server=process.argv[4];
const child=spawn(bin,['acp','--yolo'],{cwd,stdio:['pipe','pipe','pipe']});
let buf='';let id=1;const pending=new Map();const updates=[];
child.stdout.on('data',d=>{buf+=d.toString();let i;while((i=buf.indexOf('\n'))>=0){const line=buf.slice(0,i).trim();buf=buf.slice(i+1);if(!line)continue;let m;try{m=JSON.parse(line)}catch{continue}
if(m.method==='session/update'){updates.push(m.params);continue}
if(m.id&&pending.has(String(m.id))){const p=pending.get(String(m.id));pending.delete(String(m.id));m.error?p.reject(new Error(JSON.stringify(m.error))):p.resolve(m.result)}}});
child.stderr.on('data',d=>process.stderr.write('[stderr] '+d));
function call(method,params,timeout=180000){const i=id++;return new Promise((res,rej)=>{const t=setTimeout(()=>rej(new Error('timeout '+method)),timeout);pending.set(String(i),{resolve:v=>{clearTimeout(t);res(v)},reject:rej});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:i,method,params})+'\n')})}
(async()=>{
  await call('initialize',{protocolVersion:1,clientCapabilities:{fs:{readTextFile:false,writeTextFile:false},terminal:false}});
  const ns=await call('session/new',{cwd,mcpServers:[{name:'echo',command:process.execPath,args:[server],env:[]}]});
  console.log('session='+ns.sessionId);
  const pr=await call('session/prompt',{sessionId:ns.sessionId,prompt:[{type:'text',text:'请调用 echo_tool 工具，参数 text=HELLO，然后把工具返回的内容原样告诉我。'}]});
  const text=updates.filter(u=>u.update.sessionUpdate==='agent_message_chunk').map(u=>u.update.content.text).join('');
  const tools=updates.filter(u=>u.update.sessionUpdate==='tool_call'||u.update.sessionUpdate==='tool_call_update');
  console.log('stopReason='+pr.stopReason,'toolUpdates='+tools.length,'hasECHO='+/ECHO:HELLO/.test(text));
  console.log('text='+JSON.stringify(text.slice(0,200)));
  child.kill();process.exit(pr.stopReason==='end_turn'&&/ECHO:HELLO/.test(text)?0:1);
})().catch(e=>{console.error('ERR',e.message);child.kill();process.exit(2)});
