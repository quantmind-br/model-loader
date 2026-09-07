import fs from 'node:fs';
import crypto from 'node:crypto';
const dir = '.pi/qwen-flash-tuning';
const status = await (await fetch('http://127.0.0.1:4321/_status')).json();
const base = `http://127.0.0.1:${status.loaded_port}`;
async function post(path, data) {
  const http = await import('node:http');
  return new Promise((resolve, reject) => {
    const req = http.request(base + path, {method:'POST', headers:{'Content-Type':'application/json'}}, res => {
      const chunks = [];
      res.on('data', c => chunks.push(c));
      res.on('error', reject);
      res.on('end', () => {
        try {
          const body = JSON.parse(Buffer.concat(chunks).toString());
          if (res.statusCode >= 400) reject(new Error(JSON.stringify(body)));
          else resolve(body);
        } catch (e) { reject(e); }
      });
    });
    req.setTimeout(10800000, () => req.destroy(new Error('Three-hour response timeout')));
    req.on('error', reject);
    req.end(JSON.stringify(data));
  });
}
// Deterministic unique records: no repeated corpus prefix or cache-hit timing claims.
let corpus = '';
for (let i=0; i<22000; i++) {
  const digest = crypto.createHash('sha256').update(`record-${i}`).digest('hex').slice(0,16);
  corpus += `Registro ${i}: lote ${digest}; quantidade ${(i*37)%997}; estado arquivado.\n`;
}
const {tokens} = await post('/tokenize', {content:corpus, add_special:false});
const facts = ['CHAVE_ALFA=laranja-7421','CHAVE_BETA=tucano-5836','CHAVE_GAMA=violeta-9164'];
const promptTokens = [];
const instructions = 'Leia os registros. Encontre CHAVE_ALFA, CHAVE_BETA e CHAVE_GAMA. Responda somente um objeto JSON com essas tres chaves e seus valores exatos.\n';
promptTokens.push(...(await post('/tokenize',{content:instructions,add_special:true})).tokens);
const budget = 244000;
for(let part=0; part<4; part++) {
  // Push in bounded chunks to avoid argument-count limits.
  for(const token of tokens.slice(part*61000,(part+1)*61000)) promptTokens.push(token);
  if(part<3) promptTokens.push(...(await post('/tokenize',{content:`\nDADO CONFIRMADO: ${facts[part]}\n`,add_special:false})).tokens);
}
promptTokens.push(...(await post('/tokenize',{content:'\nAgora forneça somente o JSON solicitado, sem explicações.\n',add_special:false})).tokens);
if(promptTokens.length < 240000 || promptTokens.length > 250000) throw new Error(`Unexpected length ${promptTokens.length}, source ${tokens.length}`);
fs.writeFileSync(`${dir}/near-full-request-meta.json`,JSON.stringify({profile:status.loaded_profile_id,promptTokens:promptTokens.length,facts,format:'raw completion token ids; not a chat/tool evaluation'},null,2));
const start=Date.now();
const response=await post('/completion',{prompt:promptTokens,n_predict:512,temperature:0,seed:42,cache_prompt:false,stream:false});
const output={elapsedMs:Date.now()-start,profile:status.loaded_profile_id,promptTokens:promptTokens.length,response,exactValuesPresent:facts.every(f=>response.content?.includes(f.split('=')[1]))};
fs.writeFileSync(`${dir}/near-full-result.json`,JSON.stringify(output,null,2));
console.log(JSON.stringify({elapsedMs:output.elapsedMs,promptTokens:output.promptTokens,timings:response.timings,content:response.content,exactValuesPresent:output.exactValuesPresent}));
