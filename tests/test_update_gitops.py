import copy
import importlib.util
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('update_gitops', ROOT / 'scripts/update_gitops.py')
update = importlib.util.module_from_spec(spec)
spec.loader.exec_module(update)


class UpdateTests(unittest.TestCase):
    def setUp(self):
        image = {'repository': 'ghcr.io/yinlerens/translate', 'digest': 'sha256:' + 'c' * 64}
        self.current = {'app': 'transform', 'releaseVersion': 'd' * 40,
                        'database': {'enabled': False}, 'image': image,
                        'routes': [{'path': '/api', 'workload': 'api', 'timeouts': {'request': '75s'}}],
                        'quota': {'pods': '6'}, 'externalDomains': ['api.cohere.com'],
                        'mesh': {'enabled': True, 'canary': {'enabled': True,
                            'stableWorkload': 'api', 'candidateWorkload': 'api-canary', 'weight': 5}},
                        'workloads': {'api': {'image': image, 'releaseVersion': 'd' * 40},
                            'api-canary': {'image': image, 'releaseVersion': 'd' * 40,
                                'nodeSelector': {'foundation.makima.sbs/site': 'remote'},
                                'secretEnv': {'COHERE_API_KEY': {'name': 'transform-api', 'key': 'cohere-api-key'}}}}}
        self.image = {'repository': 'ghcr.io/yinlerens/translate', 'digest': 'sha256:' + 'a' * 64,
                      'source_sha': 'b' * 40}

    def test_missing_registration_never_creates_application(self):
        with self.assertRaisesRegex(RuntimeError, '不会创建新应用'):
            update.release_proposal(None, self.image)

    def test_only_candidate_image_and_release_change(self):
        before = copy.deepcopy(self.current)
        files = update.release_proposal(self.current, self.image)
        self.assertEqual(set(files), {update.VALUES})
        result = yaml.safe_load(files[update.VALUES])
        candidate = result['workloads']['api-canary']
        self.assertEqual(candidate['image']['digest'], self.image['digest'])
        self.assertEqual(candidate['releaseVersion'], self.image['source_sha'])
        candidate['image'] = before['workloads']['api-canary']['image']
        candidate['releaseVersion'] = before['workloads']['api-canary']['releaseVersion']
        self.assertEqual(result, before)
        self.assertEqual(self.current, before)

    def test_invalid_identity_and_image_rejected(self):
        for field, value in [('repository', 'ghcr.io/other/service'), ('digest', 'latest'), ('source_sha', 'main')]:
            with self.assertRaises(RuntimeError):
                update.release_proposal(self.current, dict(self.image, **{field: value}))
        with self.assertRaises(RuntimeError):
            update.release_proposal(dict(self.current, app='acceptance'), self.image)

    def test_no_canary_no_remote_and_unpinned_stable_fail_closed(self):
        for target, field in [('mesh', 'canary'), ('workloads', 'api')]:
            value = copy.deepcopy(self.current)
            value[target].pop(field)
            with self.assertRaises(RuntimeError):
                update.release_proposal(value, self.image)
        value = copy.deepcopy(self.current)
        value['workloads']['api-canary']['nodeSelector'] = {}
        with self.assertRaises(RuntimeError):
            update.release_proposal(value, self.image)

    def test_shared_secret_is_not_rotated_by_canary_release(self):
        with self.assertRaisesRegex(RuntimeError, '不轮换共享模型密钥'):
            update.release_proposal(self.current, self.image, 'model-secret')


if __name__ == '__main__':
    unittest.main()
