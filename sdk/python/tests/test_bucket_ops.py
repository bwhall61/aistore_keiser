#
# Copyright (c) 2018-2022, NVIDIA CORPORATION. All rights reserved.
#

# Default provider is AIS, so all Cloud-related tests are skipped.

import random
import string
import unittest
from aistore.client.errors import ErrBckNotFound
import tempfile

from aistore.client.api import Client
import requests
from . import CLUSTER_ENDPOINT, REMOTE_BUCKET


class TestObjectOps(unittest.TestCase):  # pylint: disable=unused-variable
    def setUp(self) -> None:
        letters = string.ascii_lowercase
        self.bck_name = ''.join(random.choice(letters) for _ in range(10))

        self.client = Client(CLUSTER_ENDPOINT)
        self.buckets = []

    def tearDown(self) -> None:
        # Try to destroy all temporary buckets if there are left.
        for bck_name in self.buckets:
            try:
                self.client.destroy_bucket(bck_name)
            except ErrBckNotFound:
                pass

    def test_bucket(self):
        res = self.client.list_buckets()
        count = len(res)
        self.create_bucket(self.bck_name)
        res = self.client.list_buckets()
        count_new = len(res)
        self.assertEqual(count + 1, count_new)

    def create_bucket(self, bck_name):
        self.buckets.append(bck_name)
        self.client.create_bucket(bck_name)

    def test_head_bucket(self):
        self.create_bucket(self.bck_name)
        self.client.head_bucket(self.bck_name)
        self.client.destroy_bucket(self.bck_name)
        try:
            self.client.head_bucket(self.bck_name)
        except requests.exceptions.HTTPError as e:
            self.assertEqual(e.response.status_code, 404)

    @unittest.skipIf(REMOTE_BUCKET == "" or REMOTE_BUCKET.startswith("ais:"), "Remote bucket is not set")
    def test_evict_bucket(self):
        obj_name = "evict_obj"
        parts = REMOTE_BUCKET.split("://")  # must be in the format '<provider>://<bck>'
        self.assertTrue(len(parts) > 1)
        provider, self.bck_name = parts[0], parts[1]
        content = "test".encode("utf-8")
        with tempfile.NamedTemporaryFile() as f:
            f.write(content)
            f.flush()
            self.client.put_object(self.bck_name, obj_name, f.name, provider=provider)

        objects = self.client.list_objects(self.bck_name, provider=provider, props="name,cached", prefix=obj_name)
        self.assertTrue(len(objects) > 0)
        for obj in objects:
            if obj.name == obj_name:
                self.assertTrue(obj.is_ok())
                self.assertTrue(obj.is_cached())

        self.client.evict_bucket(self.bck_name, provider=provider)
        objects = self.client.list_objects(self.bck_name, provider=provider, props="name,cached", prefix=obj_name)
        self.assertTrue(len(objects) > 0)
        for obj in objects:
            if obj.name == obj_name:
                self.assertTrue(obj.is_ok())
                self.assertFalse(obj.is_cached())
        self.client.delete_object(self.bck_name, obj_name, provider=provider)

    def test_copy_bucket(self):
        from_bck = self.bck_name + 'from'
        to_bck = self.bck_name + 'to'
        self.create_bucket(from_bck)
        self.create_bucket(to_bck)

        xact_id = self.client.copy_bucket(from_bck, to_bck)
        self.assertNotEqual(xact_id, "")
        self.client.wait_for_xaction_finished(xact_id=xact_id)


if __name__ == '__main__':
    unittest.main()
