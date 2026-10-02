#!/usr/bin/env python3
"""Run one managed Strata probe attempt with isolated evidence.

Candidate mode points the candidate config (rebuilt from its prepared backup on
every attempt) at an explicit release binary and a per-attempt engine log,
switches the candidate profile to the matching catalog backend, snapshots
both, captures real thread masks during decode and delegates all inference to
scripts/strata-performance-probe.py (proxy only). Only candidate
profiles/configs created for IDEATION_PERFORMANCE are touched.

--readonly runs an installed canonical profile unchanged and copies the engine
log segment that the attempt appended.
"""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys
import threading
import time

REPO = Path(__file__).resolve().parents[5]
SESSION = Path(__file__).resolve().parents[1]
PROFILES = Path.home() / '.config/model-loader/profiles'
RELEASES = REPO / 'backends/strata-fork/releases'
BACKENDS = {'20261002T100952Z-2eed88f1f51d': 'strata-perf-v6-20261002',
            '20261002T095307Z-13018fd9fe4e': 'strata-perf-v5-20261002',
            '20261001T223017Z-f85ddc896414': 'strata-perf-v4-20261001',
            '20261001T222137Z-3430cc1a87e0': 'strata-perf-v3-20261001',
            '20261001T201850Z-8ae9e4e25528': 'strata-perf-v2-20261001'}


def write_json(path, value):
    tmp = path.with_suffix(path.suffix + '.tmp')
    tmp.write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n')
    tmp.replace(path)


def server_and_children(config_path):
    """PIDs of the server.py that owns this exact config and of its descendants."""
    target = str(Path(config_path).absolute()).encode()
    servers = []
    for entry in Path('/proc').iterdir():
        if not entry.name.isdigit():
            continue
        try:
            argv = (entry / 'cmdline').read_bytes().split(b'\0')
        except OSError:
            continue
        if any(a.endswith(b'/serve/server.py') for a in argv[:3]) and target in argv:
            servers.append(int(entry.name))
    found = set(servers)
    changed = True
    while changed:
        changed = False
        for entry in Path('/proc').iterdir():
            if not entry.name.isdigit() or int(entry.name) in found:
                continue
            try:
                stat = (entry / 'stat').read_text()
            except OSError:
                continue
            ppid = int(stat[stat.rfind(')') + 2:].split()[1])
            if ppid in found:
                found.add(int(entry.name))
                changed = True
    return servers, sorted(found)


def task_masks(pid):
    tasks = []
    for task in sorted((Path('/proc') / str(pid) / 'task').glob('*'), key=lambda p: int(p.name)):
        try:
            status = dict(line.split(':\t', 1) for line in (task / 'status').read_text().splitlines() if ':\t' in line)
            stat = (task / 'stat').read_text()
            fields = stat[stat.rfind(')') + 2:].split()
            tasks.append({'tid': int(task.name), 'comm': (task / 'comm').read_text().strip(),
                          'cpus_allowed_list': status.get('Cpus_allowed_list', '').strip(),
                          'last_cpu': int(fields[36]), 'utime': int(fields[11]), 'stime': int(fields[12]),
                          'voluntary': int(status.get('voluntary_ctxt_switches', '0')),
                          'nonvoluntary': int(status.get('nonvoluntary_ctxt_switches', '0'))})
        except (OSError, ValueError, IndexError):
            continue
    return tasks


def capture_masks(out, config_path, done):
    """After the first measured code request, record every task's allowed CPUs."""
    results = out / 'results.json'
    while not done.is_set():
        try:
            labels = [r['label'] for r in json.loads(results.read_text())]
        except (OSError, ValueError):
            labels = []
        if any(l.startswith(('code-', 'training-')) for l in labels):
            servers, pids = server_and_children(config_path)
            snap = {'time': time.time(), 'after': labels[-1], 'servers': servers, 'processes': {}}
            for pid in pids:
                try:
                    exe = str(Path(f'/proc/{pid}/exe').resolve())
                except OSError:
                    exe = None
                snap['processes'][str(pid)] = {'exe': exe, 'tasks': task_masks(pid)}
            write_json(out / 'thread-masks.json', snap)
            return
        done.wait(0.5)


def run_probe(a, out, config_path, meta):
    done = threading.Event()
    watcher = threading.Thread(target=capture_masks, args=(out, config_path, done), daemon=True)
    watcher.start()
    with open(out / 'probe.log', 'w') as log:
        rc = subprocess.run([sys.executable, str(REPO / 'scripts/strata-performance-probe.py'), a.profile,
                             '--out', str(out), *a.probe], stdout=log, stderr=subprocess.STDOUT, cwd=REPO).returncode
    done.set()
    watcher.join(timeout=5)
    try:
        launch = json.loads((out / 'launch.json').read_text())
        log_path = launch.get('loaded_log_path') or launch.get('log_path')
        if log_path and Path(log_path).exists():
            shutil.copy2(log_path, out / 'instance.log')
    except (OSError, ValueError):
        pass
    meta.update(returncode=rc, finished=time.time())
    write_json(out / 'attempt.json', meta)
    print(json.dumps({'label': a.label, 'returncode': rc}))
    return rc


def run_readonly(a, out, profile_path, profile, config_path):
    cfg = json.loads(config_path.read_text())
    shutil.copy2(config_path, out / 'config.json')
    shutil.copy2(profile_path, out / 'profile.json')
    engine_log = Path(cfg['log']) if cfg.get('log') else None
    offset = engine_log.stat().st_size if engine_log and engine_log.exists() else 0
    meta = {'profile': a.profile, 'readonly': True, 'backend': profile['launch'].get('backendId'), 'exe': cfg['exe'],
            'engine_log': str(engine_log), 'engine_log_offset': offset, 'probe_flags': a.probe, 'started': time.time()}
    write_json(out / 'attempt.json', meta)
    rc = run_probe(a, out, config_path, meta)
    if engine_log and engine_log.exists():
        with open(engine_log, 'rb') as src, open(out / 'engine.log', 'wb') as dst:
            src.seek(offset)
            shutil.copyfileobj(src, dst)
    return rc


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('profile')
    ap.add_argument('label', help='new attempt directory under claude-session/runs')
    ap.add_argument('--release', default='20261002T100952Z-2eed88f1f51d')
    ap.add_argument('--exe', help='explicit exe (e.g. an nsys wrapper); default <release>/strata')
    ap.add_argument('--set-arg', action='append', default=[], metavar='FLAG=VALUE',
                    help='replace the value after an existing native flag')
    ap.add_argument('--env', action='append', default=[], metavar='KEY=VALUE', help='config env for the engine')
    ap.add_argument('--readonly', action='store_true',
                    help='run an installed canonical profile unchanged; copy the engine log segment it appends')
    ap.add_argument('--probe', nargs=argparse.REMAINDER, default=[], help='flags passed to the probe')
    a = ap.parse_args()
    if a.readonly and (a.exe or a.set_arg or a.env):
        raise SystemExit('--readonly takes no --exe/--set-arg/--env')

    out = SESSION / 'runs' / a.label
    if out.exists():
        raise SystemExit(f'{out} exists; use a fresh attempt label')
    out.mkdir(parents=True)
    profile_path = PROFILES / (a.profile + '.json')
    profile = json.loads(profile_path.read_text())
    config_path = Path(profile['args']['config'])
    if a.readonly:
        sys.exit(run_readonly(a, out, profile_path, profile, config_path))
    if '-candidate' not in profile['id']:
        raise SystemExit('refusing to modify a non-candidate profile')
    backups = SESSION / 'config-backups'
    backups.mkdir(exist_ok=True)
    for src in (profile_path, config_path):
        dst = backups / (src.name + ('.profile' if src == profile_path else '.config') + '.orig.json')
        if not dst.exists():
            shutil.copy2(src, dst)

    # every attempt starts from the prepared config, so --set-arg/--env never leak into later attempts
    cfg = json.loads((backups / (config_path.name + '.config.orig.json')).read_text())
    cfg['exe'] = a.exe or str(RELEASES / a.release / 'strata')
    cfg['log'] = str(out / 'engine.log')
    for item in a.set_arg:
        flag, value = item.split('=', 1)
        index = cfg['args'].index(flag)
        cfg['args'][index + 1] = value
    if a.env:
        cfg['env'] = dict(cfg.get('env') or {}, **dict(e.split('=', 1) for e in a.env))
    write_json(config_path, cfg)
    backend = BACKENDS[a.release]
    if profile['launch'].get('backendId') != backend:
        subprocess.run(['model-loader', 'profile', 'edit', a.profile, '--backend', backend], check=True,
                       capture_output=True, text=True)
    shutil.copy2(config_path, out / 'config.json')
    shutil.copy2(profile_path, out / 'profile.json')
    meta = {'profile': a.profile, 'release': a.release, 'backend': backend, 'exe': cfg['exe'],
            'probe_flags': a.probe, 'set_arg': a.set_arg, 'env': a.env, 'started': time.time()}
    write_json(out / 'attempt.json', meta)
    sys.exit(run_probe(a, out, config_path, meta))


if __name__ == '__main__':
    main()
