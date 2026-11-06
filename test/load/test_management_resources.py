import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
RECORDER = ROOT / 'scripts/record-management-resources.py'

# Docker is replaced at the process boundary so these tests exercise the CLI and
# its persisted artifacts without requiring a running Docker daemon.
FAKE_DOCKER = '''#!/usr/bin/env python3
import json
import os
import sys
args = sys.argv[1:]
if args[0] == 'ps':
    service = next(arg.rsplit('=', 1)[1] for arg in args if arg.startswith('label=com.docker.compose.service='))
    if service != os.environ.get('MISSING_SERVICE'):
        print(service)
elif args[0] == 'inspect':
    service = args[1]
    memory = 67108864 if service == os.environ.get('BAD_SERVICE') else 33554432
    print(json.dumps([{
        'Image': 'sha256:image',
        'Config': {'Image': 'example:local', 'Cmd': ['server'], 'Env': ['APP_ENV=load', 'APP_CONFIG_DIR=/app/config', 'SECRET=must-not-be-recorded'] + (['OTEL_TRACES_SAMPLER=always_off'] if service == os.environ.get('BAD_TRACE_SERVICE') else [])},
        'HostConfig': {'NanoCpus': 500000000, 'CpuQuota': 0, 'CpuPeriod': 0,
                       'Memory': memory, 'MemorySwap': 33554432},
        'State': {'OOMKilled': service == 'public'},
        'RestartCount': 2 if service == 'public' else 0,
        'Mounts': [{'Destination': '/app/config/load.yaml', 'RW': False,
                    'Source': os.environ['FAKE_CONFIG']}],
    }]))
else:
    sys.exit(2)
'''


class ManagementResourcesTest(unittest.TestCase):
    def run_recorder(self, **environment):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            docker = directory / 'docker'
            docker.write_text(FAKE_DOCKER, encoding='utf-8')
            docker.chmod(0o755)
            output = directory / 'resources.json'
            config = directory / 'private-must-not-be-recorded.yaml'
            config.write_bytes((ROOT / 'docker/load/management.yaml').read_bytes())
            env = {**os.environ, **environment, 'FAKE_CONFIG': str(config),
                   'PATH': f'{directory}{os.pathsep}{os.environ["PATH"]}'}
            result = subprocess.run([sys.executable, str(RECORDER), '--project', 'fixture',
                                     '--output', str(output)], env=env, capture_output=True, text=True)
            text = output.read_text(encoding='utf-8')
            return result, json.loads(text), text

    def test_limits_images_and_oom_are_recorded_without_secrets(self):
        result, report, text = self.run_recorder()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(report['errors'])
        self.assertEqual(report['services']['management']['nano_cpus'], 500000000)
        self.assertEqual(report['services']['public']['memory_bytes'], 33554432)
        self.assertEqual(report['services']['management']['tracing']['sample_rate'], 0.1)
        self.assertTrue(report['services']['management']['tracing']['enabled'])
        self.assertTrue(report['services']['public']['oom_killed'])
        self.assertEqual(report['services']['public']['restart_count'], 2)
        self.assertEqual(report['services']['proxy']['image_id'], 'sha256:image')
        self.assertNotIn('must-not-be-recorded', text)

    def test_wrong_server_limit_fails_but_preserves_evidence(self):
        result, report, _ = self.run_recorder(BAD_SERVICE='management')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(report['services']['management']['memory_bytes'], 67108864)
        self.assertTrue(any('management' in error for error in report['errors']))

    def test_tracing_override_fails_closed(self):
        result, report, _ = self.run_recorder(BAD_TRACE_SERVICE='management')
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(any('tracing environment' in error for error in report['errors']))

    def test_missing_dependency_fails_but_preserves_other_services(self):
        result, report, _ = self.run_recorder(MISSING_SERVICE='postgres')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('management', report['services'])
        self.assertTrue(any('postgres' in error for error in report['errors']))


if __name__ == '__main__':
    unittest.main()
