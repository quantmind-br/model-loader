"""Fail-closed identities and serialized inference accounting for one probe attempt."""
import contextlib
import fcntl
import json
import os
from pathlib import Path
import signal
import time
import urllib.error
import urllib.request
from urllib.parse import urlsplit
import uuid

BASE = 'http://127.0.0.1:4321'
_URLOPEN = urllib.request.urlopen


class IntegrityError(RuntimeError):
    pass


def _json(url):
    with _URLOPEN(url, timeout=5) as response:
        return json.load(response)


def _write(path, value):
    temporary = path.with_name(path.name + '.' + uuid.uuid4().hex + '.tmp')
    temporary.write_text(json.dumps(value, indent=2) + '\n')
    temporary.replace(path)


@contextlib.contextmanager
def _lock(out):
    with (out / 'lifecycle.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def _invalid(out, reason):
    path = out / 'lifecycle-invalid.json'
    invalid = json.loads(path.read_text()) if path.exists() else {'time': time.time(), 'reasons': []}
    if reason not in invalid['reasons']:
        invalid['reasons'].append(reason)
    _write(path, invalid)


def mark_invalid(out, reason):
    out = Path(out)
    with _lock(out):
        _invalid(out, reason)


def _process(pid):
    root = Path('/proc') / str(pid)
    stat = (root / 'stat').read_text()
    fields = stat[stat.rfind(')') + 2:].split()
    if fields[0] in ('Z', 'X'):
        raise IntegrityError(f'process {pid} is dead')
    return {'pid': pid, 'start_ticks': int(fields[19]), 'pgid': os.getpgid(pid), 'sid': os.getsid(pid),
            'argv': os.fsdecode((root / 'cmdline').read_bytes()).split('\0')[:-1]}


def _listener(port):
    inodes = set()
    for name in ('tcp', 'tcp6'):
        for line in (Path('/proc/net') / name).read_text().splitlines()[1:]:
            fields = line.split()
            if int(fields[1].split(':')[1], 16) == port and fields[3] == '0A':
                inodes.add(fields[9])
    owners = []
    for entry in Path('/proc').iterdir():
        if not entry.name.isdigit():
            continue
        try:
            descriptors = list((entry / 'fd').iterdir())
        except OSError:
            continue
        links = []
        for fd in descriptors:
            try:
                links.append(os.readlink(fd))
            except OSError:
                continue
        if any('socket:[' + inode + ']' in links for inode in inodes):
            owners.append(int(entry.name))
    if len(owners) != 1:
        raise IntegrityError(f'port {port} has {len(owners)} identifiable listeners')
    return _process(owners[0])


def _native(server_pid, exe):
    children = (Path('/proc') / str(server_pid) / 'task' / str(server_pid) / 'children').read_text().split()
    matches = []
    for pid in children:
        process = _process(int(pid))
        if process['argv'] and Path(process['argv'][0]).resolve() == Path(exe).resolve() and '--serve' in process['argv']:
            matches.append(process)
    if len(matches) != 1:
        raise IntegrityError(f'server {server_pid} has {len(matches)} matching native children')
    return matches[0]


def _metrics(expected):
    metrics = _json(f"http://127.0.0.1:{expected['loaded_port']}/metrics")
    totals = metrics['totals']
    if type(totals['requests']) is not int or totals['requests'] < 0:
        raise IntegrityError('metrics requests is not a nonnegative integer')
    return metrics


def capture_expected(out, profile, launch, config):
    """Capture only the launch result, never a subsequently discovered replacement."""
    out = Path(out)
    with _lock(out):
        try:
            if (out / 'lifecycle-expected.json').exists():
                raise IntegrityError('expected identity already exists')
            if launch['loaded_profile_id'] != profile or not launch['loaded_pid'] or not launch['loaded_port']:
                raise IntegrityError('launch identity does not match requested profile')
            expected = {key: launch[key] for key in ('loaded_profile_id', 'loaded_pid', 'loaded_port')}
            expected['time'] = time.time()
            expected['server'] = _process(expected['loaded_pid'])
            _write(out / 'lifecycle-server.json', expected['server'])
            expected['proxy'] = _listener(4321)
            expected['native'] = _native(expected['loaded_pid'], config['exe'])
            metrics = _metrics(expected)
            if metrics['live']['state'] != 'idle' or metrics['live'].get('queued', 0):
                raise IntegrityError('instance is not idle immediately after launch')
            if metrics['totals']['requests'] != 0:
                raise IntegrityError('inference occurred before launch identity capture')
            expected['metrics_since'] = metrics['totals']['since']
            _write(out / 'lifecycle-accounting.json', {'totals': metrics['totals'], 'active': None})
            _write(out / 'lifecycle-expected.json', expected)
            _validate(out, expected)
        except Exception as exc:
            _invalid(out, 'launch capture failed: ' + str(exc))
            raise IntegrityError(str(exc)) from exc


def _validate(out, expected, accounting=True):
    status = _json(BASE + '/_status')
    for key in ('loaded_profile_id', 'loaded_pid', 'loaded_port'):
        if status.get(key) != expected[key]:
            raise IntegrityError(f'{key} changed: {status.get(key)!r} != {expected[key]!r}')
    if not status.get('running'):
        raise IntegrityError('proxy is not running')
    for role in ('proxy', 'server', 'native'):
        if _process(expected[role]['pid']) != expected[role]:
            raise IntegrityError(role + ' process identity changed')
    metrics = _metrics(expected)
    if metrics['totals']['since'] != expected['metrics_since']:
        raise IntegrityError('metrics epoch changed')
    state = json.loads((out / 'lifecycle-accounting.json').read_text())
    if not accounting:
        return metrics, state
    baseline = state['totals']['requests']
    count = metrics['totals']['requests']
    allowed = (baseline, baseline + 1) if state['active'] else (baseline,)
    if count not in allowed:
        raise IntegrityError(f'unexplained inference total {count}; authorized totals {allowed}')
    if not state['active']:
        if metrics['totals'] != state['totals']:
            raise IntegrityError('metrics totals changed outside authorized inference')
        if metrics['live']['state'] != 'idle' or metrics['live'].get('queued', 0):
            raise IntegrityError('unauthorized inference is active or queued')
    elif metrics['live'].get('queued', 0) > (1 if metrics['live']['state'] == 'idle' and count == baseline else 0):
        raise IntegrityError('another inference request is queued during authorized inference')
    return metrics, state


def validate_identity(out):
    """Validate identities and idle/active accounting; permanently record any failure."""
    out = Path(out)
    with _lock(out):
        if (out / 'lifecycle-invalid.json').exists():
            return False
        try:
            expected = json.loads((out / 'lifecycle-expected.json').read_text())
            _validate(out, expected)
            return True
        except Exception as exc:
            _invalid(out, 'lifecycle observation failed: ' + str(exc))
            return False


def begin_request(out, body, source, allow_rejection=False):
    out = Path(out)
    with _lock(out):
        try:
            if (out / 'lifecycle-invalid.json').exists():
                raise IntegrityError('attempt already contaminated')
            expected = json.loads((out / 'lifecycle-expected.json').read_text())
            metrics, state = _validate(out, expected)
            if state['active']:
                raise IntegrityError('authorized inference requests overlap')
            if body.get('model') != expected['loaded_profile_id']:
                raise IntegrityError('authorized request names a different profile')
            token = uuid.uuid4().hex
            state['active'] = {'id': token, 'source': source, 'started': time.time(), 'before': metrics['totals'],
                               'allow_rejection': allow_rejection}
            _write(out / 'lifecycle-accounting.json', state)
            return token
        except Exception as exc:
            _invalid(out, 'request start failed: ' + str(exc))
            raise IntegrityError(str(exc)) from exc


def finish_request(out, token, status):
    out = Path(out)
    with _lock(out):
        try:
            expected = json.loads((out / 'lifecycle-expected.json').read_text())
            metrics, state = _validate(out, expected, accounting=False)
            active = state['active']
            if not active or active['id'] != token:
                raise IntegrityError('request accounting token changed')
            after = metrics['totals']
            delta = after['requests'] - active['before']['requests']
            # The rejection controls intentionally exercise pre-generation HTTP 400s.
            wanted = 0 if status == 400 and active['allow_rejection'] else 1
            record = {**active, 'finished': time.time(), 'status': status, 'after': after, 'delta': delta}
            with (out / 'inference-accounting.jsonl').open('a') as log:
                log.write(json.dumps(record) + '\n')
            if (out / 'lifecycle-invalid.json').exists():
                raise IntegrityError('attempt was contaminated during authorized inference')
            if status != 200 and not (status == 400 and active['allow_rejection']):
                raise IntegrityError(f'authorized inference returned unexpected HTTP {status}')
            if wanted == 0 and after != active['before']:
                raise IntegrityError('rejected control changed inference totals')
            if delta != wanted:
                raise IntegrityError(f"request {token} returned HTTP {status}: inference delta {delta}, expected {wanted}")
            if metrics['live']['state'] != 'idle' or metrics['live'].get('queued', 0):
                raise IntegrityError('inference remains active after authorized response closed')
            state['totals'] = after
            state['active'] = None
            _write(out / 'lifecycle-accounting.json', state)
        except Exception as exc:
            _invalid(out, 'request completion failed: ' + str(exc))
            raise IntegrityError(str(exc)) from exc


class _Response:
    def __init__(self, response, out, token, started):
        self.response, self.out, self.token, self.inference_started = response, out, token, started
        self.finished = False

    def __getattr__(self, name):
        return getattr(self.response, name)

    def __iter__(self):
        return iter(self.response)

    def __enter__(self):
        return self

    def close(self):
        if not self.finished:
            self.finished = True
            try:
                self.response.close()
            finally:
                finish_request(self.out, self.token, self.response.status)

    def __exit__(self, *args):
        self.close()


def accounted_urlopen(out, request, *args, source='probe', allow_rejection=False, **kwargs):
    """Observe POST generation before send and after response context closes."""
    url = request.full_url if isinstance(request, urllib.request.Request) else str(request)
    parsed = urlsplit(url)
    if parsed.path not in ('/v1/chat/completions', '/v1/messages'):
        return _URLOPEN(request, *args, **kwargs)
    if not isinstance(request, urllib.request.Request) or request.get_method() != 'POST':
        raise IntegrityError('generation must use an observable POST Request')
    if parsed.hostname != '127.0.0.1' or parsed.port != 4321:
        mark_invalid(out, 'authorized generation bypasses the managed proxy')
        raise IntegrityError('generation must use the managed proxy')
    body = json.loads(request.data)
    token = begin_request(out, body, source, allow_rejection)
    started = time.monotonic()
    try:
        response = _URLOPEN(request, *args, **kwargs)
    except urllib.error.HTTPError as exc:
        finish_request(out, token, exc.code)
        raise
    except Exception as exc:
        mark_invalid(out, 'generation transport failed: ' + str(exc))
        raise
    return _Response(response, Path(out), token, started)


def stop_expected_group(out):
    """Emergency kill only the captured server session, never its replacement."""
    out = Path(out)
    expected = (json.loads((out / 'lifecycle-expected.json').read_text())['server']
                if (out / 'lifecycle-expected.json').exists()
                else json.loads((out / 'lifecycle-server.json').read_text()))
    pid = expected['pid']
    try:
        if expected['pgid'] != pid or expected['sid'] != pid or _process(pid) != expected:
            return []
        os.killpg(pid, signal.SIGKILL)
    except (OSError, IntegrityError):
        return []
    return [{'pid': pid, 'start_ticks': expected['start_ticks'], 'signal': 'SIGKILL'}]
