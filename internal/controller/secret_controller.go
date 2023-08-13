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
	"time"

	"github.com/gpu-ninja/operator-utils/zaplogr"
	"github.com/gpu-ninja/tls-replicator/internal/constants"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Allow reading of namespaces.
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=secrets/finalizers,verbs=update

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
		if enabledStr, ok := secret.Annotations[constants.AnnotationEnabledKey]; ok && strings.ToLower(enabledStr) == "true" {
			enabled = true
		}
	}

	if !enabled {
		logger.Info("Not enabled")

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&secret, constants.FinalizerName) {
		logger.Info("Adding Finalizer")

		_, err := controllerutil.CreateOrPatch(ctx, r.Client, &secret, func() error {
			controllerutil.AddFinalizer(&secret, constants.FinalizerName)

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

	var existingReplicatedSecrets []corev1.Secret
	for _, namespace := range namespaces.Items {
		if namespace.Name == secret.Namespace {
			continue
		}

		replicatedSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secret.Name,
				Namespace: namespace.Name,
			},
		}

		if err := r.Get(ctx, client.ObjectKeyFromObject(&replicatedSecret), &replicatedSecret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, fmt.Errorf("failed to check for replicated secret: %w", err)
		}

		existingReplicatedSecrets = append(existingReplicatedSecrets, replicatedSecret)
	}

	if !secret.GetDeletionTimestamp().IsZero() {
		logger.Info("Deleting")

		for _, replicatedSecret := range existingReplicatedSecrets {
			if err := r.Delete(ctx, &replicatedSecret); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return ctrl.Result{}, fmt.Errorf("failed to delete replicated secret: %w", err)
			}
		}

		if controllerutil.ContainsFinalizer(&secret, constants.FinalizerName) {
			logger.Info("Removing Finalizer")

			_, err := controllerutil.CreateOrPatch(ctx, r.Client, &secret, func() error {
				controllerutil.RemoveFinalizer(&secret, constants.FinalizerName)

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
		if replicatedKeysAnnotation, ok := secret.Annotations[constants.AnnotationReplicateKeysKey]; ok {
			keyFilters = strings.Split(replicatedKeysAnnotation, ",")
		}
	}

	var namespaceFilters []string
	if secret.Annotations != nil {
		if replicateTo, ok := secret.Annotations[constants.AnnotationReplicateToKey]; ok {
			namespaceFilters = strings.Split(replicateTo, ",")
		}
	}

	var desiredReplicatedSecrets []corev1.Secret
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
			desiredReplicatedSecrets = append(desiredReplicatedSecrets, corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secret.Name,
					Namespace: namespace.Name,
				},
			})
		}
	}

	removedSecrets, addedSecrets := diffSecrets(existingReplicatedSecrets, desiredReplicatedSecrets)

	for _, replicatedSecret := range removedSecrets {
		if err := r.Delete(ctx, &replicatedSecret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, fmt.Errorf("failed to delete replicated secret: %w", err)
		}
	}

	for _, replicatedSecret := range addedSecrets {
		_, err := controllerutil.CreateOrPatch(ctx, r.Client, &replicatedSecret, func() error {
			if secret.Labels != nil {
				if replicatedSecret.Labels == nil {
					replicatedSecret.Labels = map[string]string{}
				}

				for key, value := range secret.Labels {
					replicatedSecret.Labels[key] = value
				}
			}

			replicatedSecret.Type = secret.Type

			if replicatedSecret.Data == nil {
				replicatedSecret.Data = map[string][]byte{}
			}

			// For tls secrets, we need to ensure that the cert and private key are present.
			if secret.Type == corev1.SecretTypeTLS {
				replicatedSecret.Data[corev1.TLSCertKey] = []byte("")
				replicatedSecret.Data[corev1.TLSPrivateKeyKey] = []byte("")
			}

			for key, value := range secret.Data {
				if len(keyFilters) > 0 {
					for _, keyFilter := range keyFilters {
						if ok, err := filepath.Match(keyFilter, key); err != nil {
							return fmt.Errorf("failed to evaluate key filter: %w", err)
						} else if ok {
							replicatedSecret.Data[key] = value
							break
						}
					}
				} else {
					replicatedSecret.Data[key] = value
				}
			}

			return nil
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to replicate secret: %w", err)
		}
	}

	// Periodically revisit the secret to ensure that namespaces have not been added or
	// removed, etc. Should perhaps add a namespace watcher but this is a simple solution
	// for now.
	return ctrl.Result{
		RequeueAfter: 30 * time.Second,
	}, nil
}

func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("secret-controller").
		For(&corev1.Secret{}).
		Owns(&corev1.Namespace{}).
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
