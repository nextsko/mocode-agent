const { spawn } = require('child_process');
const bin = process.argv[2], cwd = process.argv[3];
const child = spawn(bin, ['acp'], { cwd, stdio: ['pipe','pipe','pipe'] });
let buf='', id=1; const pending=new Map(); const updates=[]; let permCount=0; let lastPermId=null;
child.stdout.on('data', d => {
  buf += d.toString(); let i;
  while ((i = buf.indexOf('\n')) >= 0) {
    const line = buf.slice(0,i).trim(); buf = buf.slice(i+1); if (!line) continue;
    let m; try { m = JSON.parse(line); } catch { continue; }
    if (m.method === 'session/update') { updates.push(m.params); continue; }
    if (m.method === 'session/request_permission') {
      permCount++; lastPermId = m.id;
      child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result:{outcome:{outcome:'selected',optionId:process.env.PERM_OPTION||'allow-once'}}})+'\n');
      continue;
    }
    if (m.id && pending.has(String(m.id))) {
      const p = pending.get(String(m.id)); pending.delete(String(m.id));
      m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result);
    }
  }
});
child.stderr.on('data', d => process.stderr.write('[stderr] '+d));
function call(method, params, timeout=180000) {
  const i = id++;
  return new Promise((res, rej) => {
    const t = setTimeout(() => { pending.delete(String(i)); rej(new Error('timeout '+method)); }, timeout);
    pending.set(String(i), { resolve: v => { clearTimeout(t); res(v); }, reject: e => { clearTimeout(t); rej(e); } });
    child.stdin.write(JSON.stringify({jsonrpc:'2.0',id:i,method,params})+'\n');
  });
}
function note(method, params) { child.stdin.write(JSON.stringify({jsonrpc:'2.0',method,params})+'\n'); }
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
function check(name, ok, detail='') { results.push({name, ok, detail}); console.log((ok?'PASS':'FAIL')+' '+name+(detail?' :: '+detail:'')); }
(async () => {
  await call('initialize', {protocolVersion:1, clientCapabilities:{fs:{readTextFile:true,writeTextFile:true},terminal:false}});
  const ns = await call('session/new', {cwd, mcpServers: []});
  const sid = ns.sessionId;
  const modes = ns.modes ? ns.modes.availableModes.map(m=>m.id) : [];
  check('session/new', !!sid && !!ns.modes, 'modes='+modes.length);

  // set_mode
  try {
    await call('session/set_mode', {sessionId: sid, modeId: 'plan'});
    check('session/set_mode', updates.some(u=>u.update.sessionUpdate==='current_mode_update' && u.update.currentModeId==='plan'));
  } catch(e) { check('session/set_mode', false, e.message); }

  // set_config_option (same model value)
  try {
    const cfg = (ns.configOptions||[]).find(c=>c.id==='model');
    await call('session/set_config_option', {sessionId: sid, configId: 'model', value: cfg.currentValue});
    check('session/set_config_option', updates.some(u=>u.update.sessionUpdate==='config_option_update'));
  } catch(e) { check('session/set_config_option', false, e.message); }

  // simple prompt
  const p1 = await call('session/prompt', {sessionId: sid, prompt:[{type:'text',text:'只回复两个字：OK'}]});
  check('prompt/end_turn', p1.stopReason==='end_turn', 'stop='+p1.stopReason);

  // cancel
  const before = updates.length;
  const p2 = call('session/prompt', {sessionId: sid, prompt:[{type:'text',text:'请写一篇 3000 字的长文，不要停。'}]}, 30000)
    .catch(e => ({stopReason:'error:'+e.message}));
  await sleep(600);
  note('session/cancel', {sessionId: sid});
  const r2 = await p2;
  check('session/cancel', r2.stopReason==='cancelled', 'stop='+r2.stopReason);

  // tool replay: fresh session, allowed bash, then load
  const ns2 = await call('session/new', {cwd, mcpServers: []});
  await call('session/prompt', {sessionId: ns2.sessionId, prompt:[{type:'text',text:'用 bash 工具运行：printf ACCEPT_OK。然后只回复 DONE。'}]});
  const beforeLoad = updates.length;
  const loaded = await call('session/load', {sessionId: ns2.sessionId, cwd, mcpServers: []});
  const replay = updates.slice(beforeLoad).map(u=>u.update.sessionUpdate);
  check('load text replay', replay.includes('user_message_chunk') && replay.includes('agent_message_chunk'));
  check('load tool replay', replay.includes('tool_call') || replay.includes('tool_call_update'), 'kinds='+JSON.stringify([...new Set(replay)]));

  // resume / close / delete
  try {
    const rs = await call('session/resume', {sessionId: ns2.sessionId, cwd, mcpServers: []});
    check('session/resume', rs.sessionId===ns2.sessionId);
    await call('session/close', {sessionId: ns2.sessionId});
    check('session/close', true);
    await call('session/delete', {sessionId: ns2.sessionId});
    check('session/delete', true);
  } catch(e) { check('resume/close/delete', false, e.message); }

  // reject_always: deny bash once, then same tool must not prompt again
  const ns3 = await call('session/new', {cwd, mcpServers: []});
  process.env.PERM_OPTION = 'reject-always';
  const beforePerm = permCount;
  const beforeTools = updates.filter(u=>u.update.sessionUpdate==='tool_call').length;
  await call('session/prompt', {sessionId: ns3.sessionId, prompt:[{type:'text',text:'用 bash 工具运行：printf DENY_TEST。只运行一次。'}]}).catch(()=>{});
  const afterFirst = permCount;
  const afterTools = updates.filter(u=>u.update.sessionUpdate==='tool_call').length;
  const newTools = updates.slice(-20).filter(u=>u.update.sessionUpdate==='tool_call').map(u=>u.update.name||u.update.title);
  console.log('DEBUG reject first: permDelta='+(afterFirst-beforePerm)+' toolDelta='+(afterTools-beforeTools)+' tools='+JSON.stringify(newTools));
  await call('session/prompt', {sessionId: ns3.sessionId, prompt:[{type:'text',text:'再次用 bash 工具运行：printf DENY_TEST2。'}]}).catch(()=>{});
  const afterSecond = permCount;
  check('reject_always first prompt asked', afterFirst > beforePerm, 'delta='+(afterFirst-beforePerm));
  check('reject_always second prompt silent', afterSecond === afterFirst, 'delta='+(afterSecond-afterFirst));

  // cleanup
  try { await call('session/delete', {sessionId: sid}); } catch {}
  try { await call('session/delete', {sessionId: ns3.sessionId}); } catch {}

  child.kill();
  const failed = results.filter(r=>!r.ok);
  console.log('SUMMARY', results.length-failed.length+'/'+results.length+' passed');
  process.exit(failed.length?1:0);
})().catch(e => { console.error('ERROR', e.stack || e.message); child.kill(); process.exit(2); });
