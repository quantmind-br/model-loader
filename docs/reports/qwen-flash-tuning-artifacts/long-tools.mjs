import fs from 'node:fs';
import http from 'node:http';
const profile=process.argv[2];
if(!profile)throw Error('profile required');
async function chat(messages,extra={}){return new Promise((resolve,reject)=>{const start=Date.now();const req=http.request('http://127.0.0.1:4321/v1/chat/completions',{method:'POST',headers:{'Content-Type':'application/json'}},res=>{const chunks=[];res.on('data',c=>chunks.push(c));res.on('error',reject);res.on('end',()=>{try{const body=JSON.parse(Buffer.concat(chunks));if(res.statusCode>=400)throw Error(JSON.stringify(body));resolve({body,elapsedMs:Date.now()-start});}catch(e){reject(e);}});});req.setTimeout(7200000,()=>req.destroy(Error('timeout')));req.on('error',reject);req.end(JSON.stringify({model:profile,messages,max_tokens:1024,temperature:0,seed:42,stream:false,...extra}));});}
let records='';
for(let i=0;i<6000;i++){records+=`Registro ${i}: produto SKU-${10000+i}, armazém ARM-${i%23}, quantidade ${(i*19)%503}.\n`;if(i===1499)records+='ORDEM PRIORITÁRIA: pedido PED-8427 usa produto SKU-59173.\n';if(i===4499)records+='ROTA ESPECIAL: produto SKU-59173 deve sair do armazém ARM-97.\n';}
const messages=[{role:'system',content:'Consulte os registros para atender ao pedido. Use consultar_disponibilidade com os códigos exatos encontrados; não invente. Após retorno da ferramenta, responda em português usando o lote e a quantidade retornados.'},{role:'user',content:records+'\nConsulte a disponibilidade para o pedido PED-8427.'}];
const tools=[{type:'function',function:{name:'consultar_disponibilidade',description:'Consulta estoque no armazém indicado.',parameters:{type:'object',properties:{sku:{type:'string'},armazem:{type:'string'}},required:['sku','armazem'],additionalProperties:false}}}];
const first=await chat(messages,{tools,tool_choice:'required'});
const msg=first.body.choices?.[0]?.message;
let valid=false,followup=null,error=null;
try{const calls=msg?.tool_calls??[];if(calls.length!==1)throw Error('expected one tool call');const call=calls[0],args=JSON.parse(call.function.arguments);valid=call.function.name==='consultar_disponibilidade'&&args.sku==='SKU-59173'&&args.armazem==='ARM-97';if(valid){followup=await chat([...messages,msg,{role:'tool',tool_call_id:call.id,content:JSON.stringify({quantidade:73,lote:'LTX-492861',status:'disponível'})}],{tools,tool_choice:'none'});}}catch(e){error=String(e);}
const content=followup?.body.choices?.[0]?.message?.content??'';
const result={profile,first,followup,error,toolCorrect:valid,answerCorrect:content.includes('73')&&content.includes('LTX-492861'),promptTokens:first.body.usage?.prompt_tokens};
fs.writeFileSync(`.pi/qwen-flash-tuning/long-tools-${profile}.json`,JSON.stringify(result,null,2));
console.log(JSON.stringify({profile,promptTokens:result.promptTokens,toolCorrect:valid,answerCorrect:result.answerCorrect,error}));
if(!valid||!result.answerCorrect)process.exitCode=1;
