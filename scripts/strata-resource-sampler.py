#!/usr/bin/env python3
"""Read-only host/process/GPU sampler, with guards for Strata calibration."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time


def read(path):
    try:return Path(path).read_text().strip()
    except OSError:return None


def counters(path):
    result={}
    for line in (read(path) or '').splitlines():
        parts=line.replace(':','').split()
        if len(parts)>1:
            try:result[parts[0]]=int(parts[1])
            except ValueError:continue
    return result


def snapshot():
    processes=[]
    for entry in Path('/proc').iterdir():
        if not entry.name.isdigit():continue
        try:cmd=(entry/'cmdline').read_bytes().split(b'\0')
        except OSError:continue
        if not any(b'strata-fork' in c for c in cmd[:3]):continue
        status=counters(entry/'status'); stat=read(entry/'stat')
        if not stat:continue
        fields=stat[stat.rfind(')')+2:].split()
        p={'pid':int(entry.name),'role':'server' if any(b'server.py' in c for c in cmd[:3]) else 'native',
           'status_kib':{k:status[k] for k in ['VmRSS','VmSwap','VmSize','RssAnon','RssFile','RssShmem'] if k in status},
           'smaps_kib':counters(entry/'smaps_rollup'),'io':counters(entry/'io'),
           'minflt':int(fields[7]),'majflt':int(fields[9]),'cgroup':read(entry/'cgroup')}
        group=next((l[3:] for l in (p['cgroup'] or '').splitlines() if l.startswith('0::')),None)
        if group:
            directory=Path('/sys/fs/cgroup')/group.lstrip('/')
            p['cgroup_memory']={k:read(directory/k) for k in ['memory.current','memory.max','memory.swap.current','memory.events','memory.pressure','memory.stat']}
        processes.append(p)
    gpus=[];gpu_processes=[]
    try:
        output=subprocess.check_output(['nvidia-smi','--query-gpu=index,uuid,memory.used,utilization.gpu,power.draw,power.limit,temperature.gpu,clocks.sm,clocks.mem,clocks_event_reasons.active','--format=csv,noheader,nounits'],text=True,timeout=3)
        for line in output.splitlines():
            values=[s.strip() for s in line.split(',')]
            gpus.append(dict(zip(['index','uuid','memory_mib','util_pct','power_w','cap_w','temperature_c','sm_mhz','mem_mhz','clock_event_reasons'],values)))
        output=subprocess.check_output(['nvidia-smi','--query-compute-apps=pid,gpu_uuid,used_memory','--format=csv,noheader,nounits'],text=True,timeout=3)
        gpu_processes=[dict(zip(['pid','uuid','memory_mib'],[s.strip() for s in line.split(',')])) for line in output.splitlines()]
    except (OSError,subprocess.SubprocessError):pass
    vm=counters('/proc/vmstat')
    return {'time':time.time(),'memory_kib':counters('/proc/meminfo'),'processes':processes,'gpus':gpus,'gpu_compute_processes':gpu_processes,
            'pressure':{k:read('/proc/pressure/'+k) for k in ['memory','io','cpu']},
            'vmstat':{k:v for k,v in vm.items() if k.startswith(('pgscan','pgsteal','workingset_refault','pswp','pgmaj','compact_','pgmigrate','allocstall','kswapd','thp_','pageoutrun'))},
            'zram':{p.parent.name:p.read_text().strip() for p in Path('/sys/block').glob('zram*/mm_stat')}}


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--out',required=True);parser.add_argument('--stop-file',required=True)
    parser.add_argument('--guard-file',required=True);a=parser.parse_args()
    baseline=None;strikes=0
    with open(a.out,'w',buffering=1) as log:
        while not Path(a.stop_file).exists():
            s=snapshot();mem=s['memory_kib'];swap=mem['SwapTotal']-mem['SwapFree']
            if baseline is None:baseline=swap
            s['swap_growth_kib']=swap-baseline
            reasons=[]
            if not s['gpus']:reasons.append('GPU telemetry unavailable')
            if mem['MemAvailable']<2*1024**2:reasons.append('MemAvailable below 2 GiB')
            if swap-baseline>2*1024**2:reasons.append('global swap growth above 2 GiB')
            for gpu in s['gpus']:
                if float(gpu['memory_mib'])>23552:reasons.append('GPU '+gpu['index']+' above 23 GiB')
                if float(gpu['temperature_c'])>=85:reasons.append('GPU '+gpu['index']+' at least 85 C')
            strikes=strikes+1 if reasons else 0
            s['guard_reasons']=reasons;s['guard_strikes']=strikes
            log.write(json.dumps(s)+'\n')
            if strikes>=3:
                Path(a.guard_file).write_text(json.dumps({'reasons':reasons,'time':s['time']}));return
            time.sleep(1)


if __name__=='__main__':main()
