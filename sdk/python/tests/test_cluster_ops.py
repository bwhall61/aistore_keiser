#
# Copyright (c) 2018-2022, NVIDIA CORPORATION. All rights reserved.
#

# Default provider is AIS, so all Cloud-related tests are skipped.

import unittest

from aistore.client.api import Client
from . import CLUSTER_ENDPOINT


class TestClusterOps(unittest.TestCase):  # pylint: disable=unused-variable
    def setUp(self) -> None:
        self.client = Client(CLUSTER_ENDPOINT)

    def test_cluster_map(self):
        smap = self.client.get_cluster_info()

        self.assertIsNotNone(smap)
        self.assertIsNotNone(smap.proxy_si)
        self.assertNotEqual(len(smap.pmap), 0)
        self.assertNotEqual(len(smap.tmap), 0)
        self.assertNotEqual(smap.version, 0)
        self.assertIsNot(smap.uuid, "")
