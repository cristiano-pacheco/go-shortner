import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


COLLECTOR = Path(__file__).resolve().parents[2] / 'scripts/collect-management-diagnostics.py'


class ManagementDiagnosticsTest(unittest.TestCase):
    def test_unsupported_docker_endpoint_preserves_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {**os.environ, 'DOCKER_HOST': 'tcp://127.0.0.1:2375'}
            result = subprocess.run([sys.executable, str(COLLECTOR), '--project', 'fixture',
                                     '--output', directory], env=env, text=True, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            lines = [json.loads(line) for line in (Path(directory) / 'samples.jsonl').read_text().splitlines()]
            self.assertEqual(lines[0]['kind'], 'collector_start')
            self.assertEqual(lines[-1]['kind'], 'collector_error')
            self.assertIn('Unix socket', lines[-1]['error'])
            self.assertTrue(lines[-1]['timestamp'])

    def test_plan_mode_records_diagnostic_mode_and_plans(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            docker = directory / 'docker'
            docker.write_text('''#!/usr/bin/env python3
import sys
if sys.argv[1] == 'ps':
    print('fixture-postgres')
elif sys.argv[1] == 'exec':
    if 'EXPLAIN' in sys.argv[-1]:
        print('[{"Plan": {"Node Type": "Limit"}}]')
    else:
        print('1000')
else:
    sys.exit(2)
''', encoding='utf-8')
            docker.chmod(0o755)
            output = directory / 'results'
            env = {**os.environ, 'PATH': f'{directory}{os.pathsep}{os.environ["PATH"]}'}
            result = subprocess.run([sys.executable, str(COLLECTOR), '--project', 'fixture',
                                     '--output', str(output), '--query-plans'],
                                    env=env, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            lines = [json.loads(line) for line in (output / 'samples.jsonl').read_text().splitlines()]
            self.assertEqual([line['kind'] for line in lines], ['collector_start', 'query_plans', 'collector_stop'])
            self.assertEqual(lines[0]['mode'], 'query_plans')
            self.assertFalse(lines[1]['analyze'])
            self.assertEqual(lines[1]['visible_count'], 1000)
            self.assertEqual(lines[1]['deep_offset'], 900)
            self.assertIn('Limit', lines[1]['data'])


if __name__ == '__main__':
    unittest.main()
