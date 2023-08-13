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

package controller_test

import (
	"context"
	"testing"

	"github.com/gpu-ninja/operator-utils/zaplogr"
	"github.com/gpu-ninja/tls-replicator/internal/constants"
	"github.com/gpu-ninja/tls-replicator/internal/controller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestSecretReconciler(t *testing.T) {
	ctrl.SetLogger(zaplogr.New(zaptest.NewLogger(t)))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				constants.AnnotationEnabledKey: "true",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": []byte("test-crt"),
			"tls.key": []byte("test-key"),
			"ca.crt":  []byte("test-ca"),
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
			WithObjects(secret, anotherNamespace).
			Build()

		r := &controller.SecretReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      secret.Name,
				Namespace: secret.Namespace,
			},
		})
		require.NoError(t, err)
		assert.NotZero(t, resp.RequeueAfter)

		var replicatedSecret corev1.Secret
		err = client.Get(ctx, types.NamespacedName{
			Name:      secret.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedSecret)
		require.NoError(t, err)

		assert.Equal(t, secret.Data, replicatedSecret.Data)
	})

	t.Run("Should Not Replicate When Not Enabled", func(t *testing.T) {
		unreplicateSecret := secret.DeepCopy()
		delete(unreplicateSecret.Annotations, constants.AnnotationEnabledKey)

		client := fake.NewClientBuilder().
			WithObjects(unreplicateSecret, anotherNamespace).
			Build()

		r := &controller.SecretReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      unreplicateSecret.Name,
				Namespace: unreplicateSecret.Namespace,
			},
		})
		require.NoError(t, err)
		assert.Zero(t, resp)

		var replicatedSecret corev1.Secret
		err = client.Get(ctx, types.NamespacedName{
			Name:      unreplicateSecret.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedSecret)
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("Should Only Replicate Specified Keys", func(t *testing.T) {
		secretWithKeys := secret.DeepCopy()
		secretWithKeys.Annotations[constants.AnnotationReplicatedKeysKey] = "ca*"

		client := fake.NewClientBuilder().
			WithObjects(secretWithKeys, anotherNamespace).
			Build()

		r := &controller.SecretReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      secret.Name,
				Namespace: secret.Namespace,
			},
		})
		require.NoError(t, err)
		assert.NotZero(t, resp.RequeueAfter)

		var replicatedSecret corev1.Secret
		err = client.Get(ctx, types.NamespacedName{
			Name:      secret.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedSecret)
		require.NoError(t, err)

		assert.Empty(t, replicatedSecret.Data[corev1.TLSCertKey])
		assert.Empty(t, replicatedSecret.Data[corev1.TLSPrivateKeyKey])
		assert.Equal(t, secret.Data["ca.crt"], replicatedSecret.Data["ca.crt"])
	})

	t.Run("Should Only Replicate To Specified Namespaces", func(t *testing.T) {
		thirdNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "third-namespace",
			},
		}

		secretWithNamespaces := secret.DeepCopy()
		secretWithNamespaces.Annotations[constants.AnnotationReplicateToKey] = "third-*"

		client := fake.NewClientBuilder().
			WithObjects(secretWithNamespaces, anotherNamespace, thirdNamespace).
			Build()

		r := &controller.SecretReconciler{
			Client: client,
			Scheme: scheme.Scheme,
		}

		resp, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      secret.Name,
				Namespace: secret.Namespace,
			},
		})
		require.NoError(t, err)
		assert.NotZero(t, resp.RequeueAfter)

		var replicatedSecret corev1.Secret
		err = client.Get(ctx, types.NamespacedName{
			Name:      secret.Name,
			Namespace: thirdNamespace.Name,
		}, &replicatedSecret)
		require.NoError(t, err)

		// Should not be replicated to anotherNamespace
		err = client.Get(ctx, types.NamespacedName{
			Name:      secret.Name,
			Namespace: anotherNamespace.Name,
		}, &replicatedSecret)
		require.Error(t, err)
	})
}
