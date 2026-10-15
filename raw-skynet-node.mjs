// Raw endpoint ke ../skynet — Node version (lebih cocok untuk agent lain)
// Usage: node raw-skynet-node.mjs "prompt kamu" [model]
import fs from 'fs';
const SKYNET_DIR = new URL('../skynet', import.meta.url).pathname.replace(/\/$/,'');
const API_KEY = process.env.COMMAND_CODE_API_KEY || "user_4xEMfXTxtzVAhTZDd7uFkQ6fjCZ8k2mwU7AMAW9eCKE9p2jxMJxHFgJ3rFMXYqyngr9RGCsqmaxV7bpr78Yzu7vf";
const MODEL = process.argv[3] || "poolside/laguna-s-2.1-free";
const MSG = process.argv[2] || "jelaskan struktur project skynet ini, fokus ke internal/agent";
const TEMPLATE = "/tmp/body.json";

function uuid(){ return crypto.randomUUID(); }
function randHex(n){ return [...crypto.getRandomValues(new Uint8Array(n))].map(b=>b.toString(16).padStart(2,'0')).join(''); }

const uuid1 = uuid(), trace=randHex(16), span=randHex(8);
const template = JSON.parse(fs.readFileSync(TEMPLATE,'utf8'));
// patch 100% mirip CLI
template.config.workingDir = SKYNET_DIR;
template.config.date = new Date().toISOString().slice(0,10);
template.threadId = uuid1;
template.params.model = MODEL;
template.params.messages = [{role:"user", content:[{type:"text", text:MSG}]}];
template.params.max_tokens = 8000;
template.params.stream = true;
// keep tools+system resmi

console.log(`→ ${SKYNET_DIR} → https://api.commandcode.ai/alpha/generate [${MODEL}]`);

const res = await fetch("https://api.commandcode.ai/alpha/generate", {
  method:"POST",
  headers:{
    "Content-Type":"application/json",
    "Authorization":`Bearer ${API_KEY}`,
    "User-Agent":"cli",
    "x-command-code-version":"1.28.4",
    "x-cli-environment":"production",
    "x-project-slug": SKYNET_DIR.replace(/\//g,'-').toLowerCase().replace(/^-/,''),
    "x-taste-learning":"true",
    "x-co-flag":"false",
    "x-session-id": uuid1,
    "traceparent": `00-${trace}-${span}-01`,
    "Accept":"*/*","Accept-Language":"*","Sec-Fetch-Mode":"cors"
  },
  body: JSON.stringify(template)
});
if(!res.ok){ console.error("HTTP",res.status, await res.text()); process.exit(1); }
const dec = new TextDecoder(); let buf="";
for await(const chunk of res.body){
  buf+=dec.decode(chunk,{stream:true});
  let lines=buf.split("\n"); buf=lines.pop();
  for(const l of lines){ if(!l.trim()) continue; try{ const j=JSON.parse(l); if(j.type==='text-delta') process.stdout.write(j.text); if(j.type==='finish-step') console.log("\n\n[usage]",j.usage); }catch{} }
}
console.log("\n— done —");
