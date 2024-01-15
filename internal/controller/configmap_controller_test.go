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

package controller_test

import (
	"context"
	"testing"

	"github.com/dpeckett/replikator/internal/controller"
	"github.com/go-logr/logr"
	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestConfigMapReconciler(t *testing.T) {
	ctrl.SetLogger(logr.FromSlogHandler(slogt.New(t).Handler()))

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				controller.AnnotationEnabledKey: "true",
			},
		},
		Data: map[string]string{
			"key":   "test-value",
			"key-2": "another-test-value",
		},
	}

	anotherNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "another-namespace",
		},
	}

	ctx := context.Background()

	t.Run("Should Replicate When Enabled", func(t *testing.T) {
		client := fake.NewClientBuilder().
			WithObjects(cm, anotherNamespace).
			Build()

		r := &controller.ConfigMapReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cm.Name,
				Namespace: cm.Namespace,
			},
		})
		require.NoError(t, err)
		assert.Zero(t, resp)

		var replicatedConfigMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      cm.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedConfigMap)
		require.NoError(t, err)

		assert.Equal(t, cm.Data, replicatedConfigMap.Data)
	})

	t.Run("Should Not Replicate When Not Enabled", func(t *testing.T) {
		unreplicateConfigMap := cm.DeepCopy()
		delete(unreplicateConfigMap.Annotations, controller.AnnotationEnabledKey)

		client := fake.NewClientBuilder().
			WithObjects(unreplicateConfigMap, anotherNamespace).
			Build()

		r := &controller.ConfigMapReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      unreplicateConfigMap.Name,
				Namespace: unreplicateConfigMap.Namespace,
			},
		})
		require.NoError(t, err)
		assert.Zero(t, resp)

		var replicatedConfigMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      unreplicateConfigMap.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedConfigMap)
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("Should Only Replicate Specified Keys", func(t *testing.T) {
		configMapWithKeys := cm.DeepCopy()
		configMapWithKeys.Annotations[controller.AnnotationReplicateKeysKey] = "ca*"

		client := fake.NewClientBuilder().
			WithObjects(configMapWithKeys, anotherNamespace).
			Build()

		r := &controller.ConfigMapReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cm.Name,
				Namespace: cm.Namespace,
			},
		})
		require.NoError(t, err)
		assert.Zero(t, resp)

		var replicatedConfigMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      cm.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedConfigMap)
		require.NoError(t, err)

		assert.Empty(t, replicatedConfigMap.Data[corev1.TLSCertKey])
		assert.Empty(t, replicatedConfigMap.Data[corev1.TLSPrivateKeyKey])
		assert.Equal(t, cm.Data["ca.crt"], replicatedConfigMap.Data["ca.crt"])
	})

	t.Run("Should Only Replicate To Specified Namespaces", func(t *testing.T) {
		thirdNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "third-namespace",
			},
		}

		configMapWithNamespaces := cm.DeepCopy()
		configMapWithNamespaces.Annotations[controller.AnnotationReplicateToKey] = "third-*"

		client := fake.NewClientBuilder().
			WithObjects(configMapWithNamespaces, anotherNamespace, thirdNamespace).
			Build()

		r := &controller.ConfigMapReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cm.Name,
				Namespace: cm.Namespace,
			},
		})
		require.NoError(t, err)
		assert.Zero(t, resp)

		var replicatedConfigMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      cm.Name,
			Namespace: thirdNamespace.Name,
		}, &replicatedConfigMap)
		require.NoError(t, err)

		// Should not be replicated to anotherNamespace
		err = client.Get(ctx, types.NamespacedName{
			Name:      cm.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedConfigMap)
		require.Error(t, err)
	})
}
