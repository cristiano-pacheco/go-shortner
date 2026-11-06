import datetime as dt
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


REPORTER = Path(__file__).resolve().parents[2] / 'scripts/report-management-load.py'
EPOCH = dt.datetime(2026, 1, 1, tzinfo=dt.timezone.utc)


def point(metric, at, value, **tags):
    when = EPOCH + dt.timedelta(seconds=at)
    return {'type': 'Point', 'metric': metric, 'data': {
        'time': when.isoformat(), 'value': value, 'tags': tags}}


def marker(operation, offset=0, duration=2):
    start = int((EPOCH + dt.timedelta(seconds=offset)).timestamp() * 1000)
    return point('management_window_start', offset, 1, phase='measurement',
                 operation=operation, start_ms=str(start), end_ms=str(start + duration * 1000))


def request(operation, started, completed, failed=0, check=1, latency=10):
    tags = {'phase': 'measurement', 'operation': operation}
    return [point('management_requests_started', started, 1, **tags),
            point('http_reqs', completed, 1, **tags),
            point('http_req_duration', completed, latency, **tags),
            point('http_req_failed', completed, failed, **tags),
            point('checks', completed, check, **tags)]


class ManagementReportTest(unittest.TestCase):
    def report(self, events, operation, duration=2):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'events.json'
            path.write_text(''.join(json.dumps(event) + '\n' for event in events), encoding='utf-8')
            result = subprocess.run([sys.executable, str(REPORTER), str(path), '--operation', operation,
                                     '--duration-seconds', str(duration)], text=True, capture_output=True)
            return result, json.loads(result.stdout) if result.stdout else None

    def test_window_rates_peak_and_late_completions(self):
        events = [marker('get')]
        for index in range(2001):
            events.extend(request('get', 0.1 if index < 1501 else 1.1,
                                  0.2 if index < 1500 else (1.2 if index < 2000 else 2.0)))
        result, report = self.report(events, 'get')
        self.assertEqual(result.returncode, 0, result.stderr)
        operation = report['operations']['get']
        self.assertEqual(operation['offered_rate_per_second'], 1010)
        self.assertEqual(operation['mean_completed_rate_per_second'], 1000)
        self.assertEqual(operation['peak']['rate_per_second'], 1500)
        self.assertEqual(operation['peak']['duration_seconds'], 1)
        self.assertEqual(operation['completed_outside_window'], 1)
        self.assertEqual(operation['incomplete_at_window_end'], 1)
        self.assertEqual(operation['never_completed'], 0)
        self.assertFalse(report['campaign_conditions'])

    def test_late_failures_checks_and_drops_invalidate(self):
        events = [marker('get'), *request('get', 0.1, 2.0, failed=1, check=0),
                  point('dropped_iterations', 2.1, 1, scenario='get')]
        result, report = self.report(events, 'get')
        self.assertEqual(result.returncode, 1)
        operation = report['operations']['get']
        self.assertEqual(operation['http_failures'], 1)
        self.assertEqual(operation['failed_checks'], 1)
        self.assertEqual(report['dropped_iterations'], 1)
        self.assertFalse(report['valid'])

    def test_mixed_accepts_distinct_windows_and_one_request_per_iteration(self):
        events = []
        for index, operation in enumerate(('create', 'get', 'list', 'update', 'delete')):
            events.append(marker(operation, offset=index))
            events.extend(request(operation, index + 0.1, index + 0.2))
        result, report = self.report(events, 'mixed')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(report['diagnostic_only'])
        self.assertEqual(report['operations']['delete']['requests_per_iteration'], 1)
        self.assertEqual(report['operations']['delete']['offered_rate_per_second'], 200)
        self.assertNotEqual(report['operations']['create']['window'], report['operations']['delete']['window'])

    def test_incomplete_failed_latency_and_boundary_are_reported(self):
        events = [marker('get'), *request('get', 0.1, 1.9, latency=101),
                  point('management_requests_started', 1.9, 1, phase='measurement', operation='get'),
                  point('management_requests_started', 2.0, 1, phase='measurement', operation='get')]
        result, report = self.report(events, 'get')
        self.assertEqual(result.returncode, 1)
        operation = report['operations']['get']
        self.assertEqual(operation['never_completed'], 1)
        self.assertEqual(operation['started_outside_window'], 1)
        self.assertEqual(operation['p99_ms'], 101)
        self.assertIn('p99 exceeds 100 ms or is unavailable', operation['issues'])

    def test_missing_or_invalid_markers_fail_closed(self):
        for events in ([*request('get', 0.1, 0.2)], [marker('get'), marker('get')], [marker('get', duration=3)]):
            with self.subTest(events=events[:1]):
                result, report = self.report(events, 'get')
                self.assertEqual(result.returncode, 2)
                self.assertIsNone(report)


if __name__ == '__main__':
    unittest.main()
