/*
 * backup_controller.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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
	ctx "context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbadminclient"

	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"golang.org/x/net/context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FoundationDBBackupReconciler reconciles a FoundationDBCluster object
type FoundationDBBackupReconciler struct {
	client.Client
	Recorder               record.EventRecorder
	Log                    logr.Logger
	InSimulation           bool
	DatabaseClientProvider DatabaseClientProvider
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs the reconciliation logic.
func (r *FoundationDBBackupReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	backup := &fdbtypes.FoundationDBBackup{}

	err := r.Get(ctx, request.NamespacedName, backup)

	originalGeneration := backup.ObjectMeta.Generation

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	backupLog := log.WithValues("namespace", backup.Namespace, "backup", backup.Name)

	subReconcilers := []backupSubReconciler{
		updateBackupStatus{},
		updateBackupAgents{},
		startBackup{},
		stopBackup{},
		toggleBackupPaused{},
		modifyBackup{},
		updateBackupStatus{},
	}

	for _, subReconciler := range subReconcilers {
		requeue := subReconciler.reconcile(r, ctx, backup)
		if requeue == nil {
			continue
		}

		return processRequeue(requeue, subReconciler, backup, r.Recorder, backupLog)
	}

	if backup.Status.Generations.Reconciled < originalGeneration {
		backupLog.Info("Backup was not fully reconciled by reconciliation process")
		return ctrl.Result{Requeue: true}, nil
	}

	backupLog.Info("Reconciliation complete")

	return ctrl.Result{}, nil
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBBackupReconciler) getDatabaseClientProvider() DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}
	panic("Backup reconciler does not have a DatabaseClientProvider defined")
}

// adminClientForBackup provides an admin client for a backup reconciler.
func (r *FoundationDBBackupReconciler) adminClientForBackup(context ctx.Context, backup *fdbtypes.FoundationDBBackup) (fdbadminclient.AdminClient, error) {
	cluster := &fdbtypes.FoundationDBCluster{}
	err := r.Get(context, types.NamespacedName{Namespace: backup.ObjectMeta.Namespace, Name: backup.Spec.ClusterName}, cluster)
	if err != nil {
		return nil, err
	}

	adminClient, err := r.getDatabaseClientProvider().GetAdminClient(cluster, r)
	if err != nil {
		return nil, err
	}

	adminClient.SetKnobs(backup.Spec.CustomParameters.GetKnobsForCLI())

	return adminClient, nil
}

// SetupWithManager prepares a reconciler for use.
func (r *FoundationDBBackupReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrentReconciles int, selector metav1.LabelSelector) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &appsv1.Deployment{}, "metadata.name", func(o client.Object) []string {
		return []string{o.(*appsv1.Deployment).Name}
	})
	if err != nil {
		return err
	}
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbtypes.FoundationDBBackup{}).
		Owns(&appsv1.Deployment{}).
		// Only react on generation changes or annotation changes and only watch
		// resources with the provided label selector.
		WithEventFilter(
			predicate.And(
				labelSelectorPredicate,
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicate.AnnotationChangedPredicate{},
				),
			)).
		Complete(r)
}

// backupSubReconciler describes a class that does part of the work of
// reconciliation for a backup.
type backupSubReconciler interface {
	/**
	reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a requeue object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a requeue object with a
	`Message` field.
	*/
	reconcile(r *FoundationDBBackupReconciler, context ctx.Context, backup *fdbtypes.FoundationDBBackup) *requeue
}
