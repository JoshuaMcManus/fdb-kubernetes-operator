/*
 * admin_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2020 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

var _ = Describe("admin_client_test", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var client *MockAdminClient

	var err error

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		timeout := time.Second * 5
		Eventually(func() (int64, error) {
			return reloadCluster(k8sClient, cluster)
		}, timeout).ShouldNot(Equal(int64(0)))

		client, err = newMockAdminClientUncast(cluster, k8sClient)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanupCluster(cluster)
	})

	Describe("JSON status", func() {
		var status *fdbtypes.FoundationDBStatus
		JustBeforeEach(func() {
			status, err = client.GetStatus()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			It("should generate the status", func() {
				Expect(status.Cluster.DatabaseConfiguration).To(Equal(fdbtypes.DatabaseConfiguration{
					RedundancyMode: "double",
					StorageEngine:  "ssd-2",
					UsableRegions:  1,
					RoleCounts: fdbtypes.RoleCounts{
						Logs:       3,
						Proxies:    3,
						Resolvers:  1,
						LogRouters: -1,
						RemoteLogs: -1,
					},
				}))

				Expect(status.Cluster.Processes["operator-test-1-storage-1"]).To(Equal(fdbtypes.FoundationDBStatusProcessInfo{
					Address:      "1.1.0.1:4501",
					ProcessClass: "storage",
					CommandLine:  "/usr/bin/fdbserver --class=storage --cluster_file=/var/fdb/data/fdb.cluster --datadir=/var/fdb/data --locality_instance_id=storage-1 --locality_machineid=operator-test-1-storage-1 --locality_zoneid=operator-test-1-storage-1 --logdir=/var/log/fdb-trace-logs --loggroup=operator-test-1 --public_address=:4501 --seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					Excluded:     false,
					Locality: map[string]string{
						"instance_id": "storage-1",
					},
					Version: "6.2.15",
				}))
			})
		})

		Context("with a backup running", func() {
			BeforeEach(func() {
				err = client.StartBackup("blobstore://test@test-service/test-backup")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the backup in the layer status", func() {
				Expect(status.Cluster.Layers.Backup.Tags).To(Equal(map[string]fdbtypes.FoundationDBStatusBackupTag{
					"default": {
						CurrentContainer: "blobstore://test@test-service/test-backup",
						RunningBackup:    true,
						Restorable:       true,
					},
				}))
			})
		})
	})
})