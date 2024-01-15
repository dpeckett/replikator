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

package main_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestOperator(t *testing.T) {
	t.Log("Creating example resources")

	rootDir := os.Getenv("ROOT_DIR")

	require.NoError(t, createExampleResources(filepath.Join(rootDir, "examples")))

	kubeconfig := filepath.Join(clientcmd.RecommendedConfigDir, "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)

	clientset, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)

	t.Log("Waiting for root-ca-tls secret to be replicated")

	ctx := context.Background()
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		secret, err := clientset.CoreV1().Secrets("default").Get(ctx, "root-ca-tls", metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			}

			t.Log("Not yet replicated")

			return false, nil
		}

		valid := len(secret.Data["ca.crt"]) > 0 && len(secret.Data[corev1.TLSCertKey]) == 0 && len(secret.Data[corev1.TLSPrivateKeyKey]) == 0
		if !valid {
			return false, fmt.Errorf("root-ca-tls secret is not valid")
		}

		return true, nil
	})
	require.NoError(t, err, "failed to wait for root-ca-tls secret to be replicated")

	t.Log("Creating additional test namespace")

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "replikator-test",
		},
	}

	_, err = clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	require.NoError(t, err, "failed to create replikator-test namespace")

	t.Log("Checking that root-ca-tls secret is replicated to new namespace")

	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := clientset.CoreV1().Secrets("replikator-test").Get(ctx, "root-ca-tls", metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			}

			t.Log("Not yet replicated")

			return false, nil
		}

		return true, nil
	})
	require.NoError(t, err, "failed to wait for root-ca-tls secret to be replicated")
}

func createExampleResources(examplesDir string) error {
	cmd := exec.Command("kapp", "deploy", "-y", "-a", "ldap-operator-examples", "-f", examplesDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
