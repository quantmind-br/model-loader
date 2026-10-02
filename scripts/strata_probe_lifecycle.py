"""Emergency termination of an exactly identified, managed calibration group.

The normal CLI stop waits for a proxy swap/load to finish. A resource guard
cannot wait for that startup, so it may kill only the server group whose argv
contains the exact prepared config for this probe. It never launches engines.
"""
import os
from pathlib import Path
import signal


def identity(pid):
    try:
        root = Path('/proc') / str(pid)
        argv = (root / 'cmdline').read_bytes().split(b'\0')
        stat = (root / 'stat').read_text()
        fields = stat[stat.rfind(')') + 2:].split()
        return argv, fields[19], os.getpgid(pid), os.getsid(pid)
    except (OSError, IndexError):
        return None


def stop_guarded_group(config_path):
    """Kill a matching Setsid group; return audit identifiers, never argv/env."""
    config = os.fsencode(str(Path(config_path).absolute()))
    stopped = []
    for entry in Path('/proc').iterdir():
        if not entry.name.isdigit():
            continue
        pid = int(entry.name)
        before = identity(pid)
        if before is None:
            continue
        argv, ticks, pgid, sid = before
        if pgid != pid or sid != pid or len(argv) < 3:
            continue
        if not argv[1].endswith(b'/serve/server.py'):
            continue
        if not any(argv[i] == b'--config' and argv[i+1] == config for i in range(len(argv)-1)):
            continue
        # No signal based on a stale PID, reused group, or changed command.
        if identity(pid) != before:
            continue
        try:
            os.killpg(pid, signal.SIGKILL)
        except ProcessLookupError:
            continue
        stopped.append({'pid': pid, 'start_ticks': ticks, 'signal': 'SIGKILL'})
    return stopped
