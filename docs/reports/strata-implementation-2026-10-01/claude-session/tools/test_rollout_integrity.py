"""Behavior boundaries for harness completion and contaminated attempt aggregation."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


def load_module(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class RolloutIntegrity(unittest.TestCase):
    def test_quality_failure_records_terminal_failure_without_skipping_auto(self):
        harness = load_module('harness_window')
        with tempfile.TemporaryDirectory() as root:
            out = Path(root) / 'attempt'
            out.mkdir()
            (out / 'results.json').write_text('[{"label":"cached-0.9"}]')
            for name in ('lifecycle-expected.json', 'probe-requests-done.json'):
                (out / name).write_text('{}')
            failure = subprocess.CompletedProcess([], 7, 'failed', '')
            with patch.object(harness, 'RUNS', Path(root)), patch.object(sys, 'argv',
                    ['harness_window', 'profile', 'attempt', '--after', 'cached-0.9']), \
                    patch.object(harness, 'validate_identity', return_value=True), \
                    patch.object(harness.subprocess, 'run', side_effect=[failure, subprocess.CompletedProcess([], 0, 'passed', '')]) as run:
                self.assertEqual(harness.main(), 7)
                self.assertEqual(run.call_count, 2)
            self.assertEqual(json.loads((out / 'harness-done.json').read_text())['returncode'], 7)
            self.assertFalse(json.loads((out / 'harness-done.json').read_text())['success'])
            self.assertEqual(json.loads((out / 'promotion-harness-auto.json').read_text())['returncode'], 0)

    def test_contaminated_attempt_is_excluded_from_all_medians(self):
        compare = load_module('compare')
        def summary(label):
            return {'run': label, 'run_status': {'requests_complete': True}, 'guard': None,
                    'attempt': {'probe_flags': []}, 'requests': [], 'req': {},
                    'code_tps': 10 if label == 'clean' else 1000,
                    'peak_gpu_mib': {'0': 20 if label == 'clean' else 2000},
                    'lifecycle_invalid': None if label == 'clean' else {'reasons': ['foreign request']}}
        stdout = io.StringIO()
        with patch.object(sys, 'argv', ['compare', 'V7=clean,dirty']), \
                patch.object(compare, 'load', side_effect=summary), contextlib.redirect_stdout(stdout):
            compare.main()
        arm = json.loads(stdout.getvalue())['V7']
        self.assertEqual(arm['complete'], 1)
        self.assertEqual(arm['code_tps'][0], 10)
        self.assertEqual(arm['peak_gpu0_mib'][0], 20)
        self.assertEqual(arm['lifecycle_invalid'][0][1]['reasons'], ['foreign request'])


if __name__ == '__main__':
    unittest.main()
