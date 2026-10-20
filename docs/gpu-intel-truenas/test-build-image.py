#!/usr/bin/env python3
"""Hermetic Git/build-workflow regressions; Docker is the external boundary."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('build-image.sh')
DOCKERFILE = SCRIPT.with_name('Dockerfile')


class BuildImageTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.repo = self.root / 'repo'
        self.repo.mkdir()
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.calls = self.root / 'docker.jsonl'
        self.env = dict(os.environ, PATH=f'{self.bin}:{os.environ["PATH"]}', DOCKER_CALLS=str(self.calls))
        docs = self.repo / 'docs/gpu-intel-truenas'
        docs.mkdir(parents=True)
        shutil.copy(SCRIPT, docs)
        shutil.copy(DOCKERFILE, docs)
        (self.repo / '.gitignore').write_text('build/\n')
        (self.repo / 'go.mod').write_text('module example.test/agent\n\ngo 1.27.2\n')
        (self.repo / 'source.txt').write_text('fresh committed source\n')
        self.git('init', '-q')
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'fixture')
        (self.repo / 'build').mkdir()
        (self.repo / 'build/beszel-agent-linux-amd64').write_bytes(b'stale unrelated executable')
        docker = self.bin / 'docker'
        docker.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
with open(os.environ['DOCKER_CALLS'], 'a') as f:
    f.write(json.dumps(args) + '\\n')
if args[0] == 'run':
    if os.environ.get('FAIL_COMPILE'):
        sys.exit(42)
    volumes = [args[i+1] for i, a in enumerate(args[:-1]) if a in ('-v', '--volume')]
    source = next(pathlib.Path(v.rsplit(':', 2)[0]) for v in volumes if ':/src:ro' in v)
    output = next(pathlib.Path(v.rsplit(':', 1)[0]) for v in volumes if v.endswith(':/output'))
    (output / 'agent').write_bytes(b'compiled:' + (source / 'source.txt').read_bytes())
elif args[0] != 'build':
    sys.exit('unexpected Docker operation')
''')
        docker.chmod(0o755)

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.repo, text=True).strip()

    def run_build(self):
        return subprocess.run(['sh', 'docs/gpu-intel-truenas/build-image.sh'], cwd=self.repo,
                              env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

    def test_rebuilds_stale_binary_and_records_exact_provenance(self):
        result = self.run_build()
        self.assertEqual(result.returncode, 0, result.stdout)
        binary = (self.repo / 'build/beszel-agent-linux-amd64').read_bytes()
        self.assertEqual(binary, b'compiled:fresh committed source\n')
        revision = self.git('rev-parse', 'HEAD')
        manifest = json.loads((self.repo / f'build/beszel-intel-lab-{revision[:12]}.provenance.json').read_text())
        self.assertEqual(manifest['source_sha'], revision)
        self.assertEqual(manifest['binary_sha256'], hashlib.sha256(binary).hexdigest())
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual([c[0] for c in calls], ['run', 'build'])
        self.assertIn(f'org.opencontainers.image.revision={revision}', calls[-1])
        self.assertIn(f'dev.beszel.agent.binary.sha256={manifest["binary_sha256"]}', calls[-1])
        self.assertIn(manifest['go_image'], calls[0])
        self.assertIn('@sha256:', manifest['go_image'])

    def test_compile_failure_cannot_publish_stale_binary(self):
        self.env['FAIL_COMPILE'] = '1'
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual((self.repo / 'build/beszel-agent-linux-amd64').read_bytes(),
                         b'stale unrelated executable')
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()]
        self.assertEqual([c[0] for c in calls], ['run'])
        self.assertEqual(list((self.repo / 'build').glob('*.provenance.json')), [])

    def test_rejects_modified_staged_and_untracked_source_before_docker(self):
        for kind in ('modified', 'staged', 'untracked'):
            with self.subTest(kind=kind):
                if kind == 'untracked':
                    (self.repo / 'unexpected.go').write_text('package agent\n')
                else:
                    (self.repo / 'source.txt').write_text('uncommitted change\n')
                    if kind == 'staged':
                        self.git('add', 'source.txt')
                result = self.run_build()
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn('dirty', result.stdout.lower())
                self.assertFalse(self.calls.exists())
                self.git('reset', '--hard', '-q', 'HEAD')
                (self.repo / 'unexpected.go').unlink(missing_ok=True)


if __name__ == '__main__':
    unittest.main()
