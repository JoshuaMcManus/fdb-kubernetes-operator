/*
 * add_pvcs.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// addPVCs provides a reconciliation step for adding new PVCs to a cluster.
type addPVCs struct{}

// reconcile runs the reconciler's work.
func (a addPVCs) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbtypes.FoundationDBCluster) *requeue {
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			continue
		}

		_, idNum, err := podmanager.ParseProcessGroupID(processGroup.ProcessGroupID)
		if err != nil {
			return &requeue{curError: err}
		}

		pvc, err := internal.GetPvc(cluster, processGroup.ProcessClass, idNum)
		if err != nil {
			return &requeue{curError: err}
		}

		if pvc == nil {
			continue
		}
		existingPVC := &corev1.PersistentVolumeClaim{}

		err = r.Get(ctx, client.ObjectKey{Namespace: pvc.Namespace, Name: pvc.Name}, existingPVC)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return &requeue{curError: err}
			}

			owner := internal.BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
			pvc.ObjectMeta.OwnerReferences = owner
			err = r.Create(ctx, pvc)

			if err != nil {
				return &requeue{curError: err}
			}
		}
	}

	return nil
}
