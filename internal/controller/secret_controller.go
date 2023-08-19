/* SPDX-License-Identifier: Apache-2.0
 *
 * Copyright 2023 Damian Peckett <damian@pecke.tt>.
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
	"path/filepath"
	"strings"

	"github.com/gpu-ninja/operator-utils/updater"
	"github.com/gpu-ninja/operator-utils/zaplogr"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// Allow reading of namespaces.
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets/finalizers,verbs=update

const (
	// AnnotationEnabledKey is the annotation that enables secret replication.
	AnnotationEnabledKey = "v1alpha1.replikator.gpuninja.com/enabled"
	// AnnotationReplicateToKey is the annotation that specifies the target namespace/s to replicate to.
	// The value of this annotation should be a comma-separated list of values / glob patterns.
	// If this annotation is not present, the secret will be replicated to all namespaces.
	AnnotationReplicateToKey = "v1alpha1.replikator.gpuninja.com/replicate-to"
	// AnnotationReplicateKeysKey is the annotation that specifies the keys to replicate.
	// The value of this annotation should be a comma-separated list of values / glob patterns.
	// If this annotation is not present, all keys will be replicated.
	AnnotationReplicateKeysKey = "v1alpha1.replikator.gpuninja.com/replicate-keys"
	// FinalizerName is the name of the finalizer that will be added to the secret.
	FinalizerName = "replikator.gpu-ninja.com/finalizer"
)

type SecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := zaplogr.FromContext(ctx)

	logger.Info("Reconciling")

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	var enabled bool
	if secret.Annotations != nil {
		if enabledStr, ok := secret.Annotations[AnnotationEnabledKey]; ok && strings.ToLower(enabledStr) == "true" {
			enabled = true
		}
	}

	if !enabled {
		logger.Info("Replication not enabled")

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&secret, FinalizerName) {
		logger.Info("Adding Finalizer")

		_, err := controllerutil.CreateOrPatch(ctx, r.Client, &secret, func() error {
			controllerutil.AddFinalizer(&secret, FinalizerName)

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

	var existingSecrets []corev1.Secret
	for _, namespace := range namespaces.Items {
		if namespace.Name == secret.Namespace {
			continue
		}

		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secret.Name,
				Namespace: namespace.Name,
			},
		}

		if err := r.Get(ctx, client.ObjectKeyFromObject(&secret), &secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, fmt.Errorf("failed to check for replicated secret: %w", err)
		}

		existingSecrets = append(existingSecrets, secret)
	}

	if !secret.GetDeletionTimestamp().IsZero() {
		logger.Info("Deleting")

		for _, secret := range existingSecrets {
			if err := r.Delete(ctx, &secret); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return ctrl.Result{}, fmt.Errorf("failed to delete replicated secret: %w", err)
			}
		}

		if controllerutil.ContainsFinalizer(&secret, FinalizerName) {
			logger.Info("Removing Finalizer")

			_, err := controllerutil.CreateOrPatch(ctx, r.Client, &secret, func() error {
				controllerutil.RemoveFinalizer(&secret, FinalizerName)

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
	if secret.Annotations != nil {
		if replicatedKeysAnnotation, ok := secret.Annotations[AnnotationReplicateKeysKey]; ok {
			keyFilters = strings.Split(replicatedKeysAnnotation, ",")
		}
	}

	template := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   secret.Name,
			Labels: make(map[string]string),
		},
		Type: secret.Type,
		Data: make(map[string][]byte),
	}

	for key, value := range secret.ObjectMeta.Labels {
		template.ObjectMeta.Labels[key] = value
	}

	template.ObjectMeta.Labels["app.kubernetes.io/managed-by"] = "replikator"

	// For tls secrets, we need to ensure that the cert and private key are present.
	if secret.Type == corev1.SecretTypeTLS {
		template.Data[corev1.TLSCertKey] = []byte("")
		template.Data[corev1.TLSPrivateKeyKey] = []byte("")
	}

	for key, value := range secret.Data {
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
	if secret.Annotations != nil {
		if replicateTo, ok := secret.Annotations[AnnotationReplicateToKey]; ok {
			namespaceFilters = strings.Split(replicateTo, ",")
		}
	}

	var desiredSecrets []corev1.Secret
	for _, namespace := range namespaces.Items {
		if namespace.Name == secret.Namespace {
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
			secret := template.DeepCopy()
			secret.ObjectMeta.Namespace = namespace.Name

			desiredSecrets = append(desiredSecrets, *secret)
		}
	}

	removedSecrets, addedSecrets := diffSecrets(existingSecrets, desiredSecrets)

	for _, secret := range removedSecrets {
		if err := r.Delete(ctx, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, fmt.Errorf("failed to delete replicated secret: %w", err)
		}
	}

	for _, secret := range addedSecrets {
		if _, err := updater.CreateOrUpdateFromTemplate(ctx, r.Client, &secret); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to replicate secret: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("secret-controller").
		For(&corev1.Secret{}).
		// Requeue when a namespace is created.
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			logger := zaplogr.FromContext(ctx)

			// Ignore deletions (there's nothing we need to do).
			if !obj.GetDeletionTimestamp().IsZero() {
				return nil
			}

			var secrets corev1.SecretList
			err := r.List(ctx, &secrets)
			if err != nil {
				logger.Error("Failed to list secrets", zap.Error(err))

				return nil
			}

			var reqs []ctrl.Request
			for _, secret := range secrets.Items {
				reqs = append(reqs, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      secret.Name,
						Namespace: secret.Namespace,
					},
				})
			}

			return reqs
		})).
		Complete(r)
}

func diffSecrets(existing, desired []corev1.Secret) (removed, added []corev1.Secret) {
	for _, existingSecret := range existing {
		var found bool
		for _, desiredSecret := range desired {
			if existingSecret.Namespace == desiredSecret.Namespace {
				found = true
				break
			}
		}

		if !found {
			removed = append(removed, existingSecret)
		}
	}

	for _, desiredSecret := range desired {
		var found bool
		for _, existingSecret := range existing {
			if existingSecret.Namespace == desiredSecret.Namespace {
				found = true
				break
			}
		}

		if !found {
			added = append(added, desiredSecret)
		}
	}

	return removed, added
}
