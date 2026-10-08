import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

import yaml

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('update_gitops', ROOT / 'scripts/update_gitops.py')
update = importlib.util.module_from_spec(spec)
spec.loader.exec_module(update)


class UpdateTests(unittest.TestCase):
    def setUp(self):
        self.current = {'app': 'transform', 'database': {'enabled': False},
                        'routes': [{'path': '/api', 'workload': 'api', 'timeouts': {'request': '75s'}}],
                        'quota': {'pods': '4'}, 'externalDomains': ['api.cohere.com'],
                        'workloads': {'api': {'secretEnv': {'COHERE_API_KEY': {
                            'name': 'transform-api', 'key': 'cohere-api-key'}}}},
                        'image': {'repository': 'ghcr.io/yinlerens/translate', 'digest': 'sha256:' + 'c' * 64}}
        self.image = {'repository': 'ghcr.io/yinlerens/translate', 'digest': 'sha256:' + 'a' * 64,
                      'source_sha': 'b' * 40}

    def test_missing_registration_never_seals_or_creates_resources(self):
        with patch.object(update, 'seal_model_key') as seal:
            with self.assertRaisesRegex(RuntimeError, '不会创建新应用'):
                update.release_proposal(None, self.image, 'model-secret')
            seal.assert_not_called()

    def test_image_update_preserves_dependencies_routes_and_existing_secret(self):
        before = copy.deepcopy(self.current)
        with patch.object(update, 'seal_model_key') as seal:
            files = update.release_proposal(self.current, self.image)
            seal.assert_not_called()
        self.assertEqual(set(files), {update.VALUES})
        result = yaml.safe_load(files[update.VALUES])
        for field in ['database', 'routes', 'quota', 'externalDomains', 'workloads']:
            self.assertEqual(result[field], before[field])
        self.assertEqual(result['image']['digest'], self.image['digest'])
        self.assertEqual(result['releaseVersion'], self.image['source_sha'])
        self.assertEqual(self.current, before)

    def test_key_rotation_on_same_image_rolls_pod_and_only_publishes_ciphertext(self):
        self.image['digest'] = self.current['image']['digest']
        with patch.object(update, 'seal_model_key', return_value='encrypted-only') as seal:
            files = update.release_proposal(self.current, self.image, ' model-secret ')
            seal.assert_called_once_with('model-secret')
        self.assertEqual(set(files), {update.VALUES, update.SECRET})
        result = yaml.safe_load(files[update.VALUES])
        self.assertIn('foundation.makima.sbs/rollout-revision', result['workloads']['api']['annotations'])
        self.assertNotIn('model-secret', ''.join(files.values()))

    def test_wrong_identity_mutable_digest_and_secret_reference_are_rejected(self):
        for field, value in [('repository', 'ghcr.io/other/service'), ('digest', 'latest'),
                             ('source_sha', 'main')]:
            with self.assertRaises(RuntimeError):
                update.release_proposal(self.current, dict(self.image, **{field: value}))
        with self.assertRaises(RuntimeError):
            update.release_proposal(dict(self.current, app='acceptance'), self.image)
        self.current['workloads']['api']['secretEnv']['COHERE_API_KEY']['name'] = 'other-secret'
        with patch.object(update, 'seal_model_key') as seal:
            with self.assertRaisesRegex(RuntimeError, 'Secret'):
                update.release_proposal(self.current, self.image, 'model-secret')
            seal.assert_not_called()


if __name__ == '__main__':
    unittest.main()
