#!/usr/bin/env python3
"""Managed Strata probe: proxy-only inference, per-request transcripts and guards."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import threading
import time
import urllib.request
import urllib.error
from strata_probe_lifecycle import stop_guarded_group
from strata_probe_integrity import accounted_urlopen, capture_expected, mark_invalid, stop_expected_group, validate_identity

BASE='http://127.0.0.1:4321'
ACTIVE_OUT=None


def post(path,body,timeout=1800,allow_rejection=False):
    req=urllib.request.Request(BASE+path,data=json.dumps(body).encode(),headers={'Content-Type':'application/json'})
    return accounted_urlopen(ACTIVE_OUT,req,timeout=timeout,allow_rejection=allow_rejection) if ACTIVE_OUT is not None else urllib.request.urlopen(req,timeout=timeout)


def request(profile,text,maximum=256,**extra):
    body={'model':profile,'messages':[{'role':'user','content':text}],'max_tokens':maximum,'stream':True,'reasoning_effort':'none',**extra}
    start=time.monotonic();first=None;chunks=[];content='';reasoning='';last={}
    with post('/v1/chat/completions',body) as response:
        start=getattr(response,'inference_started',start)
        for raw in response:
            if not raw.startswith(b'data: '):continue
            if raw.strip()==b'data: [DONE]':break
            chunk=json.loads(raw[6:]);chunks.append(chunk)
            if chunk.get('error'):raise RuntimeError(chunk['error'])
            delta=chunk.get('choices',[{}])[0].get('delta',{})
            if first is None and any(delta.get(k) for k in ['content','reasoning_content','tool_calls']):first=time.monotonic()
            content+=delta.get('content','');reasoning+=delta.get('reasoning_content','')
            if 'usage' in chunk:last=chunk
        finished=time.monotonic()
    if not last or not last.get('timings'):
        raise RuntimeError('stream ended without complete usage/timings')
    return {'time':time.time(),'request':body,'content':content,'reasoning':reasoning,'ttft_s':None if first is None else first-start,
            'elapsed_s':finished-start,'usage':last.get('usage'),'timings':last.get('timings'),
            'finish':last.get('choices',[{}])[0].get('finish_reason'),'chunks':chunks}


def cooldown(limit=65,timeout=900):
    """Passive cooling: wait until every GPU reads below `limit` C; returns the samples."""
    cool=[];deadline=time.monotonic()+timeout
    while True:
        temperatures=[int(v) for v in subprocess.check_output(['nvidia-smi','--query-gpu=temperature.gpu','--format=csv,noheader,nounits'],text=True).split()]
        cool.append({'time':time.time(),'temperatures_c':temperatures})
        if max(temperatures)<limit:return cool
        if time.monotonic()>deadline:raise RuntimeError(f'GPUs did not cool below {limit} C')
        time.sleep(5)


def main():
    ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('profile');ap.add_argument('--out',required=True)
    ap.add_argument('--fills',action='store_true');ap.add_argument('--reps',type=int,default=3)
    ap.add_argument('--controls',action='store_true');ap.add_argument('--tuning',action='store_true')
    ap.add_argument('--training',action='store_true');ap.add_argument('--linger',type=int,default=0)
    ap.add_argument('--wait-harness',action='store_true');ap.add_argument('--harness-timeout',type=int,default=3600);a=ap.parse_args()
    if a.linger<0 or a.harness_timeout<=0:ap.error('observation and harness waits must be bounded and nonnegative')
    out=Path(a.out);out.mkdir(parents=True,exist_ok=True)
    stop=out/'sampler.stop';guard=out/'guard.json'
    if any((out/name).exists() for name in ('sampler.stop','guard.json','lifecycle-expected.json','lifecycle-server.json','lifecycle-invalid.json','harness-done.json','probe-requests-done.json')):raise SystemExit('use a fresh output directory')
    profile_path=Path.home()/'.config/model-loader/profiles'/(a.profile+'.json')
    config_path=json.loads(profile_path.read_text())['args']['config']
    with post('/_admin/unload', {}):
        pass
    # Keep the same power/thermal policy; start each arm after passive cooling.
    (out/'cooldown.json').write_text(json.dumps(cooldown(),indent=2)+'\n')
    sampler=subprocess.Popen([sys.executable,str(Path(__file__).with_name('strata-resource-sampler.py')),'--out',str(out/'resources.jsonl'),'--stop-file',str(stop),'--guard-file',str(guard),'--integrity-out',str(out)])
    ended=threading.Event()
    def stop_sampler():
        stop.touch()
        try:sampler.wait(timeout=20)
        except subprocess.TimeoutExpired:
            sampler.kill();sampler.wait()
            mark_invalid(out,'resource sampler failed to stop before unload')
    def watch():
        while not ended.wait(.25):
            if (out/'lifecycle-invalid.json').exists() and not guard.exists():
                guard.write_text(json.dumps({'time':time.time(),'reasons':['lifecycle or inference accounting contamination']}))
            if sampler.poll() is not None and not guard.exists():
                guard.write_text(json.dumps({'time':time.time(),'reasons':['resource sampler exited unexpectedly']}))
            if guard.exists():
                stop_sampler()
                captured=(out/'lifecycle-server.json').exists()
                stopped=(stop_expected_group(out) if captured else
                         ([] if (out/'lifecycle-invalid.json').exists() else stop_guarded_group(config_path)))
                (out/'guard-stop.json').write_text(json.dumps(stopped,indent=2)+'\n')
                if not captured and not (out/'lifecycle-invalid.json').exists():
                    subprocess.run(['model-loader','instance','stop',a.profile,'--force'],capture_output=True,timeout=60)
                return
    watchdog=threading.Thread(target=watch,daemon=True);watchdog.start()
    results=[];requests_complete=False
    def run(label,text,maximum=256,**extra):
        if guard.exists():raise RuntimeError('resource guard triggered')
        result=request(a.profile,text,maximum,**extra);result['label']=label;results.append(result)
        (out/'results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
        print(label,json.dumps({k:result[k] for k in ['ttft_s','elapsed_s','timings','finish']},ensure_ascii=False),flush=True)
        return result
    try:
        launch=subprocess.run(['model-loader','instance','start',a.profile,'--json'],capture_output=True,text=True,timeout=420)
        (out/'launch.json').write_text(launch.stdout);(out/'launch.stderr').write_text(launch.stderr)
        if launch.returncode:raise RuntimeError('launch failed: '+launch.stderr)
        capture_expected(out,a.profile,json.loads(launch.stdout),json.loads(Path(config_path).read_text()))
        global ACTIVE_OUT
        ACTIVE_OUT=out
        if a.training:
            for i,text in enumerate([
                'Implement a Python topological sort with cycle detection.',
                'Write a Rust CLI that counts UTF-8 words from stdin.',
                'Explique o isolamento de transações e deadlocks em português.',
                'Create a JavaScript promise queue with bounded concurrency.',
                'Explain why TCP retransmission can hurt tail latency.',
                'Design a normalized SQL schema for inventory and orders.',
            ]):run('training-'+str(i),text,512)
            requests_complete=True
            return
        run('warmup','Escreva uma função Python para busca binária e explique os casos de borda.',192)
        for i in range(a.reps):
            run('code-'+str(i),'Write a Python LRU cache with get and put operations, explain its complexity and provide three tests.',384)
        run('portuguese','Explique em português, com exemplos, a diferença entre concorrência e paralelismo.',384)
        run('exact-copy','Repita exatamente a linha a seguir, sem comentários: /tmp/a b/ação.txt && echo "ok"',96,temperature=0)
        run('json-text','Responda somente com o JSON solicitado: {"status":"ok","count":3}',96,temperature=0)
        if a.tuning:
            text='Write a Python LRU cache with get and put operations, explain its complexity and provide three tests.'
            for knob,values in [('spec_min_p',[.7,.5,.9,.9,.5,.7,.7,.5,.9]),('pcie_frac',[.28,0,.6,.6,0,.28,.28,0,.6])]:
                for i,value in enumerate(values):run(f'tune-{knob}-{value}-{i}',text,384,strata_tune={knob:value})
        if a.controls:
            harness=Path(__file__).with_name('strata_harness_runner.py')
            for mode,flags in [('integrity',['--stream','--continuation']),('auto',[])]:
                strict=subprocess.run([sys.executable,str(harness),'--out',str(out),'--','--model',a.profile,'--mode',mode,*flags],capture_output=True,text=True,timeout=180)
                (out/('strict-harness-'+mode+'.json')).write_text(json.dumps({'returncode':strict.returncode,'stdout':strict.stdout,'stderr':strict.stderr},indent=2)+'\n')
            for i in range(2):run('seed-zero-'+str(i),'Name three sorting algorithms.',128,seed=0,temperature=.7)
            run('stop-unicode','Repeat exactly: ação FIM segredo',64,temperature=0,stop=[' FIM'])
            tool={'type':'function','function':{'name':'execute','description':'Execute the exact supplied shell command.',
                  'parameters':{'type':'object','properties':{'command':{'type':'string'}},'required':['command']}}}
            command='cat "/tmp/a b/ação.txt" && printf "%s\\n" "ok"'
            result=run('auto-tool','Call execute with this exact command, preserving all bytes: '+command,256,tools=[tool],temperature=0)
            calls={}
            for chunk in result['chunks']:
                for call in chunk.get('choices',[{}])[0].get('delta',{}).get('tool_calls',[]):
                    obj=calls.setdefault(call['index'],{'id':'','type':'function','function':{'name':'','arguments':''}})
                    obj['id']=call.get('id',obj['id'])
                    for key in ('name','arguments'):obj['function'][key]+=call.get('function',{}).get(key,'')
            if calls:
                run('tool-continuation','',256,tools=[tool],messages=[{'role':'user','content':'Call execute with this exact command: '+command},
                    {'role':'assistant','content':None,'tool_calls':list(calls.values())},
                    *[{'role':'tool','tool_call_id':v['id'],'content':'Success. Exit code 0. The task is complete; do not execute again.'} for v in calls.values()]])
            run('no-tool-control','Responda somente: bom dia. Não precisa executar comandos.',64,tools=[tool],temperature=0)
            rejects=[]
            for control in [{'tool_choice':'required','tools':[tool]}, {'parallel_tool_calls':False,'tools':[tool]},
                            {'response_format':{'type':'json_object'}}, {'seed':True}, {'top_k':0}]:
                try:
                    with post('/v1/chat/completions',{'model':a.profile,'messages':[{'role':'user','content':'hi'}],**control},allow_rejection=True) as response:
                        rejects.append({'control':control,'status':response.status,'body':response.read().decode()})
                except urllib.error.HTTPError as error:rejects.append({'control':control,'status':error.code,'body':error.read().decode()})
            (out/'rejections.json').write_text(json.dumps(rejects,indent=2)+'\n')
            # A -> B -> A exercises the existing split prefix checkpoints.
            for label,prefix in [('a1','ALFA'),('b','BETA'),('a2','ALFA')]:
                run('conversation-'+label,prefix+'\n'+('Registro de exemplo sem novidade.\n'*3000)+'\nRepita o identificador inicial.',64,temperature=0)
        if a.fills:
            sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'backends/strata-fork/tools'))
            from strata_tokenizer import Tokenizer
            profile=json.loads((Path.home()/'.config/model-loader/profiles'/(a.profile+'.json')).read_text())
            cfg=json.loads(Path(profile['args']['config']).read_text());v=json.loads((Path(cfg['tokenizer'])/'vocab.json').read_text())
            tokens=[None]*len(v)
            for token,index in v.items():tokens[index]=token
            directory=Path(cfg['tokenizer'])
            tok=Tokenizer(tokens,(directory/'merges.txt').read_text().split('\n'),json.loads((directory/'token_type.json').read_text()))
            ctx=profile['args']['max-context'];unit=' Registro comum: índice 17, estado normal, nenhuma alteração.\n'
            fill_cooling=[]
            unit_n=len(tok.encode(unit))
            for fraction in ([.05,.25,.5,.9,.989] if ctx >= 1000000 else [.05,.25,.5,.9]):
                n=max(1,int(ctx*fraction)-500)//unit_n
                text=f'Sessão de avaliação {fraction}:\nIdentificador secreto: LARANJA-7391.\n'+unit*n+'\nQual foi o identificador secreto inicial? Responda apenas com ele.'
                # every fresh fill starts from the same passive-cooling threshold, not from the previous fill's heat
                fill_cooling.append({'fill':fraction,'samples':cooldown()})
                (out/'cooldown-fills.json').write_text(json.dumps(fill_cooling,indent=2)+'\n')
                run(f'fresh-{fraction}',text,64,temperature=0)
                run(f'cached-{fraction}',text,64,temperature=0)
        # No regular probe inference follows this sentinel, even when --after names
        # a cached fill which is not the last fill for other context sizes.
        (out/'probe-requests-done.json').write_text(json.dumps({'time':time.time()})+'\n')
        observation_end=time.monotonic()+a.linger
        harness_deadline=time.monotonic()+a.harness_timeout
        while time.monotonic()<observation_end or a.wait_harness:
            if guard.exists():raise RuntimeError('resource guard triggered during observation')
            if a.wait_harness:
                sentinel=out/'harness-done.json'
                if sentinel.exists():
                    try:
                        terminal=json.loads(sentinel.read_text())
                        if type(terminal['returncode']) is not int:raise ValueError('returncode is not an integer')
                    except (OSError,ValueError,KeyError) as exc:
                        mark_invalid(out,'invalid harness terminal sentinel: '+str(exc));raise RuntimeError('invalid harness sentinel') from exc
                    a.wait_harness=False
                elif time.monotonic()>=harness_deadline:
                    mark_invalid(out,'promotion harness terminal sentinel timeout')
                    raise RuntimeError('promotion harness did not finish within its bounded wait')
            time.sleep(1)
        if not validate_identity(out):raise RuntimeError('attempt lifecycle contaminated')
        requests_complete=True
    finally:
        # Freeze all observers BEFORE an intentional unload. A replacement or
        # failed observation during the measured window still remains invalid.
        ended.set();watchdog.join();stop_sampler()
        if (out/'lifecycle-expected.json').exists() and not guard.exists():validate_identity(out)
        ACTIVE_OUT=None
        try:
            if guard.exists() or (out/'lifecycle-invalid.json').exists():
                if (out/'lifecycle-server.json').exists():
                    (out/'guard-stop.json').write_text(json.dumps(stop_expected_group(out),indent=2)+'\n')
            else:
                subprocess.run(['model-loader','instance','stop',a.profile,'--force'],capture_output=True,timeout=60)
        finally:
            (out/'run-status.json').write_text(json.dumps({'requests_complete':requests_complete,'guard_triggered':guard.exists(),
                'lifecycle_invalid':(out/'lifecycle-invalid.json').exists()},indent=2)+'\n')


if __name__=='__main__':main()
