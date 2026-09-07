import fs from 'node:fs';
import http from 'node:http';
const profile=process.argv[2];
if(!profile) throw Error('profile required');
const results=[];
async function chat(messages,extra={}) {
 const data={model:profile,messages,temperature:0,seed:42,max_tokens:2048,stream:false,...extra};
 return new Promise((resolve,reject)=>{
 const req=http.request('http://127.0.0.1:4321/v1/chat/completions',{method:'POST',headers:{'Content-Type':'application/json'}},res=>{
 const chunks=[];res.on('data',c=>chunks.push(c));res.on('error',reject);res.on('end',()=>{try{const b=JSON.parse(Buffer.concat(chunks));if(res.statusCode>=400)reject(Error(JSON.stringify(b)));else resolve(b);}catch(e){reject(e);}});
 });req.setTimeout(600000,()=>req.destroy(Error('timeout')));req.on('error',reject);req.end(JSON.stringify(data));
 });
}
for(let i=0;i<6;i++) {
 const a=17+i*13,b=29+i*7;
 try {
 const r=await chat([{role:'user',content:`Responda somente JSON com soma de ${a} e ${b} no campo total e a palavra português no campo idioma.`}],{response_format:{type:'json_object'}});
 const text=r.choices[0].message.content;
 let parsed;try{parsed=JSON.parse(text);}catch{}
 results.push({test:'json',i,pass:parsed?.total===a+b&&parsed?.idioma==='português',response:r});
 }catch(e){results.push({test:'json',i,pass:false,error:String(e)});}
 console.log('json',i,results.at(-1).pass);
}
const tools=[{type:'function',function:{name:'consultar_estoque',description:'Consulta estoque real pelo código do produto.',parameters:{type:'object',properties:{sku:{type:'string'}},required:['sku'],additionalProperties:false}}}];
for(let i=0;i<6;i++){
 const sku=`PROD-${731+i}`,nonce=`LOTE-${9163+i*23}`;
 try{
 const messages=[{role:'system',content:'Use a ferramenta para consultar estoque. Não invente resultados. Após recebê-los, informe quantidade e lote em português.'},{role:'user',content:`Qual o estoque de ${sku}?`}];
 const r=await chat(messages,{tools,tool_choice:'required'});
 const msg=r.choices[0].message, calls=msg.tool_calls??[];
 const valid=calls.length===1&&calls[0].function.name==='consultar_estoque'&&JSON.parse(calls[0].function.arguments).sku===sku;
 if(!valid){results.push({test:'tool',i,pass:false,response:r});continue;}
 const r2=await chat([...messages,msg,{role:'tool',tool_call_id:calls[0].id,content:JSON.stringify({sku,quantidade:43+i,lote:nonce})}],{tools,tool_choice:'none'});
 const content=r2.choices[0].message.content??'';
 results.push({test:'tool',i,pass:content.includes(String(43+i))&&content.includes(nonce),response:r,followup:r2});
 }catch(e){results.push({test:'tool',i,pass:false,error:String(e)});}
 console.log('tool',i,results.at(-1).pass);
}
fs.writeFileSync(`.pi/qwen-flash-tuning/quality-${profile}.json`,JSON.stringify({profile,passed:results.filter(r=>r.pass).length,total:results.length,results},null,2));
console.log('passed',results.filter(r=>r.pass).length,'/',results.length);
