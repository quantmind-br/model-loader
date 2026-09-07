import assert from 'node:assert/strict';
import fs from 'node:fs';
const status=await(await fetch('http://127.0.0.1:4321/_status')).json();
assert.equal(status.loaded_profile_id,'qwen3.8-flash-next-iq4kt-ik-fifo-ub1024-256k');
const base=`http://127.0.0.1:${status.loaded_port}`;
async function post(path,data){const r=await fetch(base+path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});assert.equal(r.status,200);return r.json();}
const results=[];
const tokenized=await post('/tokenize',{content:'OK',add_special:false});
assert.ok(tokenized.tokens.length>0);
const bias=Object.fromEntries(tokenized.tokens.map(t=>[String(t),-100]));
for(const [name,extra,finish] of [['normal',{},'stop'],['force',{ignore_eos:true},'length'],['after',{},'stop'],['explicit-reset',{ignore_eos:false},'stop'],['biased',{logit_bias:bias},null],['after-bias',{},'stop'],['empty-bias',{logit_bias:{}},'stop']]){
 const response=await post('/v1/chat/completions',{messages:[{role:'user',content:'Responda apenas: OK'}],temperature:0,max_tokens:64,stream:false,...extra});
 const choice=response.choices[0];results.push({name,response});
 if(finish)assert.equal(choice.finish_reason,finish,name);
 if(name==='after-bias'||name==='empty-bias')assert.match(choice.message.content,/OK/);
 console.log(name,choice.finish_reason);
}
fs.writeFileSync('.pi/qwen-flash-tuning/eos-regression-result.json',JSON.stringify({profile:status.loaded_profile_id,results,passed:true},null,2));
