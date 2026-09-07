import fs from 'node:fs';
const status=await(await fetch('http://127.0.0.1:4321/_status')).json();
const base=`http://127.0.0.1:${status.loaded_port}`;
const results=[];
for(const [name,extra] of [['before',{}],['force',{ignore_eos:true}],['after',{}],['reset',{ignore_eos:false}]]){
 const r=await fetch(base+'/v1/chat/completions',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({messages:[{role:'user',content:'Responda apenas: OK'}],temperature:0,max_tokens:128,stream:false,...extra})});
 const response=await r.json();results.push({name,response});console.log(name,JSON.stringify(response.choices?.[0]));
}
fs.writeFileSync('.pi/qwen-flash-tuning/repro-ignore-eos.json',JSON.stringify({profile:status.loaded_profile_id,results},null,2));
