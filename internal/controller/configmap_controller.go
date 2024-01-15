/* SPDX-License-Identifier: Apache-2.0
 *
 * Copyright 2024 Damian Peckett <damian@pecke.tt>.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/gpu-ninja/operator-utils/updater"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update

type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := slog.New(logr.ToSlogHandler(log.FromContext(ctx)))

	logger.Info("Reconciling")

	var cm corev1.ConfigMap
	if err := r.Get(ctx, req.NamespacedName, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	var enabled bool
	if cm.Annotations != nil {
		if enabledStr, ok := cm.Annotations[AnnotationEnabledKey]; ok && strings.ToLower(enabledStr) == "true" {
			enabled = true
		}
	}

	if !enabled {
		logger.Info("Replication not enabled")

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&cm, FinalizerName) {
		logger.Info("Adding Finalizer")

		_, err := controllerutil.CreateOrPatch(ctx, r.Client, &cm, func() error {
			controllerutil.AddFinalizer(&cm, FinalizerName)

			return nil
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
	}

	var namespaces corev1.NamespaceList
	if err := r.List(ctx, &namespaces); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var existingConfigMaps []*corev1.ConfigMap
	for _, namespace := range namespaces.Items {
		if namespace.Name == cm.Namespace {
			continue
		}

		cm := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cm.Name,
				Namespace: namespace.Name,
			},
		}

		if err := r.Get(ctx, client.ObjectKeyFromObject(&cm), &cm); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, fmt.Errorf("failed to check for replicated configmap: %w", err)
		}

		existingConfigMaps = append(existingConfigMaps, &cm)
	}

	if !cm.GetDeletionTimestamp().IsZero() {
		logger.Info("Deleting")

		for _, cm := range existingConfigMaps {
			if err := r.Delete(ctx, cm); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return ctrl.Result{}, fmt.Errorf("failed to delete replicated configmap: %w", err)
			}
		}

		if controllerutil.ContainsFinalizer(&cm, FinalizerName) {
			logger.Info("Removing Finalizer")

			_, err := controllerutil.CreateOrPatch(ctx, r.Client, &cm, func() error {
				controllerutil.RemoveFinalizer(&cm, FinalizerName)

				return nil
			})
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}

		return ctrl.Result{}, nil
	}

	logger.Info("Creating or updating")

	var keyFilters []string
	if cm.Annotations != nil {
		if replicatedKeysAnnotation, ok := cm.Annotations[AnnotationReplicateKeysKey]; ok {
			keyFilters = strings.Split(replicatedKeysAnnotation, ",")
		}
	}

	template := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cm.Name,
			Labels: make(map[string]string),
		},
		Data: make(map[string]string),
	}

	for key, value := range cm.ObjectMeta.Labels {
		template.ObjectMeta.Labels[key] = value
	}

	template.ObjectMeta.Labels["app.kubernetes.io/managed-by"] = "replikator"

	for key, value := range cm.Data {
		if len(keyFilters) > 0 {
			for _, keyFilter := range keyFilters {
				if ok, err := filepath.Match(keyFilter, key); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to evaluate key filter: %w", err)
				} else if ok {
					template.Data[key] = value
					break
				}
			}
		} else {
			template.Data[key] = value
		}
	}

	var namespaceFilters []string
	if replicateTo, ok := cm.Annotations[AnnotationReplicateToKey]; ok {
		namespaceFilters = strings.Split(replicateTo, ",")
	}

	var desiredConfigMaps []*corev1.ConfigMap
	for _, namespace := range namespaces.Items {
		if namespace.Name == cm.Namespace {
			continue
		}

		var replicate bool
		if len(namespaceFilters) > 0 {
			for _, filter := range namespaceFilters {
				if ok, err := filepath.Match(filter, namespace.Name); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to evaluate namespace filter: %w", err)
				} else if ok {
					replicate = true
					break
				}
			}
		} else {
			replicate = true
		}

		if replicate {
			cm := template.DeepCopy()
			cm.ObjectMeta.Namespace = namespace.Name

			desiredConfigMaps = append(desiredConfigMaps, cm)
		}
	}

	removedConfigMaps, addedConfigMaps := diffObjects(existingConfigMaps, desiredConfigMaps)

	for _, cm := range removedConfigMaps {
		if err := r.Delete(ctx, cm); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, fmt.Errorf("failed to delete replicated configmap: %w", err)
		}
	}

	for _, cm := range addedConfigMaps {
		if _, err := updater.CreateOrUpdateFromTemplate(ctx, r.Client, cm); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to replicate configmap: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("configmap-controller").
		For(&corev1.ConfigMap{}).
		// Requeue when a namespace is created.
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			logger := slog.New(logr.ToSlogHandler(log.FromContext(ctx)))

			// Ignore deletions (there's nothing we need to do).
			if !obj.GetDeletionTimestamp().IsZero() {
				return nil
			}

			var configmaps corev1.ConfigMapList
			if err := r.List(ctx, &configmaps); err != nil {
				logger.Error("Failed to list configmaps", "error", err)

				return nil
			}

			var reqs []ctrl.Request
			for _, cm := range configmaps.Items {
				reqs = append(reqs, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      cm.Name,
						Namespace: cm.Namespace,
					},
				})
			}

			return reqs
		})).
		Complete(r)
}

func diffObjects[T metav1.Object](existingObjects, desiredObjects []T) (removedObjects, addedObjects []T) {
	for _, existingObject := range existingObjects {
		var found bool
		for _, desiredObject := range desiredObjects {
			if desiredObject.GetNamespace() == existingObject.GetNamespace() {
				found = true
				break
			}
		}

		if !found {
			removedObjects = append(removedObjects, existingObject)
		}
	}

	for _, desiredObject := range desiredObjects {
		var found bool
		for _, existingObject := range existingObjects {
			if desiredObject.GetNamespace() == existingObject.GetNamespace() {
				found = true
				break
			}
		}

		if !found {
			addedObjects = append(addedObjects, desiredObject)
		}
	}

	return
}
