import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from strata_probe_lifecycle import stop_guarded_group


class GuardLifecycle(unittest.TestCase):
    def test_only_exact_config_in_its_own_group_is_stopped(self):
        with tempfile.TemporaryDirectory() as tmp:
            server = Path(tmp) / 'serve/server.py'
            server.parent.mkdir()
            server.write_text('import time\nprint("ready",flush=True)\ntime.sleep(60)\n')
            config = Path(tmp) / 'candidate.json'
            processes = []
            try:
                for cfg, own_group in [(str(config), True), (str(config)+'.other', True), (str(config), False)]:
                    proc = subprocess.Popen([sys.executable, str(server), '--config', cfg],
                                            start_new_session=own_group, stdout=subprocess.PIPE, text=True)
                    processes.append(proc)
                    self.assertEqual(proc.stdout.readline().strip(), 'ready')
                stopped = stop_guarded_group(config)
                self.assertEqual([s['pid'] for s in stopped], [processes[0].pid])
                processes[0].wait(timeout=5)
                self.assertIsNone(processes[1].poll())
                self.assertIsNone(processes[2].poll())
                self.assertEqual(stop_guarded_group(config), [])
            finally:
                for proc in processes:
                    if proc.poll() is None:
                        proc.kill()
                    proc.wait(timeout=5)
                    proc.stdout.close()


if __name__ == '__main__':
    unittest.main()
