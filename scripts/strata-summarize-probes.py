#!/usr/bin/env python3
"""Summarize retained managed Strata probes without conflating fresh/cached work."""
import argparse,json,statistics
from pathlib import Path

def summarize(path):
 results=json.loads((path/'results.json').read_text()) if (path/'results.json').exists() else []
 samples=[json.loads(line) for line in (path/'resources.jsonl').read_text().splitlines()] if (path/'resources.jsonl').exists() else []
 code=[r['timings']['predicted_per_second'] for r in results if r['label'].startswith('code-') and r.get('timings')]
 summary={'run':path.name,'run_status':json.loads((path/'run-status.json').read_text()) if (path/'run-status.json').exists() else None,
          'guard':json.loads((path/'guard.json').read_text()) if (path/'guard.json').exists() else None,
          'code_tps':{'median':statistics.median(code),'min':min(code),'max':max(code)} if code else None,
          'peak_gpu_mib':{},'process_peak_swap_kib':{},'retrieval':[],'checks':{}}
 if samples:
  summary['min_available_gib']=min(s['memory_kib']['MemAvailable'] for s in samples)/1024**2
  summary['max_swap_growth_gib']=max(0,max(s['swap_growth_kib'] for s in samples))/1024**2
  summary['peak_simultaneous_gpu_mib']=max(sum(float(g['memory_mib']) for g in s['gpus']) for s in samples)
  summary['peak_temperature_c']=max(float(g['temperature_c']) for s in samples for g in s['gpus'])
  summary['global_pswpout_pages']=samples[-1]['vmstat'].get('pswpout',0)-samples[0]['vmstat'].get('pswpout',0)
  for s in samples:
   for g in s['gpus']:summary['peak_gpu_mib'][g['index']]=max(summary['peak_gpu_mib'].get(g['index'],0),float(g['memory_mib']))
   for p in s['processes']:
    k=str(p['pid'])+':'+p['role'];summary['process_peak_swap_kib'][k]=max(summary['process_peak_swap_kib'].get(k,0),p['status_kib'].get('VmSwap',0))
 for r in results:
  t=r.get('timings') or {};label=r['label']
  if label.startswith(('fresh-','cached-')):
   summary['retrieval'].append({'label':label,'prompt_n':t.get('prompt_n'),'cache_n':t.get('cache_n'),
     'ttft_s':r['ttft_s'],'exact':r['content'].strip()=='LARANJA-7391'})
  if label=='exact-copy':summary['checks']['exact_copy']=r['content'].strip()=='/tmp/a b/ação.txt && echo "ok"'
  if label=='json-text':
   try:summary['checks']['json_text']=json.loads(r['content'])=={'status':'ok','count':3}
   except ValueError:summary['checks']['json_text']=False
  if label=='auto-tool':
   calls=[c for chunk in r['chunks'] for c in chunk.get('choices',[{}])[0].get('delta',{}).get('tool_calls',[])]
   args=''.join(c.get('function',{}).get('arguments','') for c in calls)
   try:summary['checks']['tool_bytes']=json.loads(args).get('command')=='cat "/tmp/a b/ação.txt" && printf "%s\\n" "ok"'
   except ValueError:summary['checks']['tool_bytes']=False
  if label=='tool-continuation':summary['checks']['continuation_no_repeat']=r['finish']=='stop'
  if label=='no-tool-control':summary['checks']['no_tool_control']=r['finish']=='stop'
  if label=='stop-unicode':summary['checks']['stop_unicode']=r['content']=='ação'
  if label=='seed-zero-1':summary['checks']['seed_zero_repeat']=r['content']==next(x['content'] for x in results if x['label']=='seed-zero-0')
 if results:summary['engine_info']=(results[-1].get('timings') or {}).get('engine_info')
 return summary

def main():
 ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('directory',type=Path);a=ap.parse_args()
 data=[]
 for p in sorted(a.directory.iterdir()):
  if p.is_dir() and (p/'resources.jsonl').exists():
   s=summarize(p);(p/'summary.json').write_text(json.dumps(s,indent=2)+'\n');data.append(s)
 (a.directory/'summaries.json').write_text(json.dumps(data,indent=2)+'\n')
 for s in data:print(s['run'],s['code_tps'],s.get('peak_gpu_mib'),round(s.get('max_swap_growth_gib',0),3),'GUARD' if s['guard'] else '',s['checks'])
if __name__=='__main__':main()
