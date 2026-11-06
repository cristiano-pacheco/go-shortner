import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


REPORTER = Path(__file__).resolve().parents[2] / 'scripts/report-management-diagnostics.py'


def container(at, total, prior, periods, throttled, memory):
    return {'kind': 'container', 'timestamp': f'2026-01-01T00:00:0{at}+00:00', 'service': 'management', 'id': 'c',
            'state': {'OOMKilled': False}, 'restart_count': 0, 'stats': {
                'read': f'2026-01-01T00:00:0{at}+00:00',
                'preread': f'2026-01-01T00:00:0{at - 1}+00:00',
                'cpu_stats': {'cpu_usage': {'total_usage': total}, 'throttling_data': {
                    'periods': periods, 'throttled_periods': throttled, 'throttled_time': throttled * 100}},
                'precpu_stats': {'cpu_usage': {'total_usage': prior}},
                'memory_stats': {'usage': memory}}}


def prometheus(at, seconds, count):
    names = ('go_sql_wait_duration_seconds_total', 'go_sql_wait_count_total')
    return {'kind': 'prometheus', 'timestamp': f'2026-01-01T00:00:0{at}+00:00', 'data': {
        'status': 'success', 'data': {'result': [
            {'metric': {'__name__': name, 'role': 'management'}, 'value': [at, str(value)]}
            for name, value in zip(names, (seconds, count))]}}}


class DiagnosticReportTest(unittest.TestCase):
    def report(self, samples):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'samples').write_text(''.join(json.dumps(sample) + '\n' for sample in samples))
            (root / 'report').write_text(json.dumps({'operations': {'get': {'window': {
                'start': '2026-01-01T00:00:00+00:00', 'end': '2026-01-01T00:00:04+00:00'}}},
                'dropped_iterations': 3}))
            result = subprocess.run([sys.executable, str(REPORTER), '--samples', str(root / 'samples'),
                                     '--report', str(root / 'report'), '--output', str(root / 'result')],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            return json.loads((root / 'result').read_text())

    def test_cpu_throttling_waits_and_dropped_iterations(self):
        samples = [container(1, 1500000000, 1000000000, 10, 2, 100),
                   container(3, 3500000000, 2500000000, 30, 7, 200),
                   prometheus(1, 2, 10), prometheus(3, 5, 16)]
        report = self.report(samples)
        item = report['containers'][0]
        self.assertEqual(item['cpu_cores_max'], 1)
        self.assertEqual(item['memory_bytes_max'], 200)
        self.assertEqual(item['cpu_periods_delta'], 20)
        self.assertEqual(item['throttled_periods_delta'], 5)
        self.assertEqual(item['throttled_period_fraction'], 0.25)
        waits = report['mean_latency_and_pool_waits'][0]
        self.assertEqual(waits['seconds_delta'], 3)
        self.assertEqual(waits['observations_delta'], 6)
        self.assertEqual(waits['mean_seconds'], 0.5)
        self.assertEqual(report['generator']['dropped_iterations'], 3)
        self.assertTrue(report['warnings'])

    def test_counter_resets_and_out_of_window_are_not_inferred_as_zero(self):
        samples = [container(1, 1500000000, 1000000000, 10, 2, 100),
                   prometheus(1, 5, 10), prometheus(3, 2, 4),
                   container(4, 5000000000, 0, 100, 100, 999)]
        report = self.report(samples)
        self.assertIsNone(report['containers'][0]['throttled_period_fraction'])
        self.assertEqual(report['containers'][0]['memory_bytes_max'], 100)
        self.assertIsNone(report['mean_latency_and_pool_waits'][0]['mean_seconds'])


if __name__ == '__main__':
    unittest.main()
