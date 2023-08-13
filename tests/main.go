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

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	red   = color.New(color.FgRed).SprintFunc()
	green = color.New(color.FgGreen).SprintFunc()
)

func main() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}

	pwd, err := os.Getwd()
	if err != nil {
		logger.Fatal(red("Failed to get current working directory"), zap.Error(err))
	}

	logger.Info("Building operator image")

	buildContextPath := filepath.Clean(filepath.Join(pwd, ".."))

	imageName := "ghcr.io/gpu-ninja/tls-replicator:latest-dev"
	if err := buildOperatorImage(buildContextPath, "Dockerfile", imageName); err != nil {
		logger.Fatal(red("Failed to build operator image"), zap.Error(err))
	}

	logger.Info("Creating k3d cluster")

	clusterName := "tls-replicator-test"
	if err := createK3dCluster(clusterName); err != nil {
		logger.Fatal(red("Failed to create k3d cluster"), zap.Error(err))
	}
	defer func() {
		logger.Info("Deleting k3d cluster")

		if err := deleteK3dCluster(clusterName); err != nil {
			logger.Fatal(red("Failed to delete k3d cluster"), zap.Error(err))
		}
	}()

	logger.Info("Loading operator image into k3d cluster")

	if err := loadOperatorImage(clusterName, imageName); err != nil {
		logger.Fatal(red("Failed to load operator image"), zap.Error(err))
	}

	logger.Info("Installing cert-manager and operator")

	certManagerVersion := "v1.12.0"
	if err := installCertManager(certManagerVersion); err != nil {
		logger.Fatal(red("Failed to install cert-manager"), zap.Error(err))
	}

	overrideYAMLPath := filepath.Join(pwd, "config/dev.yaml")
	if err := installOperator(overrideYAMLPath, filepath.Join(pwd, "../config")); err != nil {
		logger.Fatal(red("Failed to install operator"), zap.Error(err))
	}

	logger.Info("Creating example resources")

	if err := createExampleResources(filepath.Join(pwd, "../examples")); err != nil {
		logger.Fatal(red("Failed to create example resources"), zap.Error(err))
	}

	kubeconfig := filepath.Join(clientcmd.RecommendedConfigDir, "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Fatal(red("Failed to build kubeconfig"), zap.Error(err))
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Fatal(red("Failed to create kubernetes clientset"), zap.Error(err))
	}

	logger.Info("Waiting for cluster-tls to be replicated")

	ctx := context.Background()
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		secret, err := clientset.CoreV1().Secrets("default").Get(ctx, "cluster-tls", metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			}

			logger.Info("Not yet replicated")

			return false, nil
		}

		valid := len(secret.Data["ca.crt"]) > 0 && len(secret.Data[corev1.TLSCertKey]) == 0 && len(secret.Data[corev1.TLSPrivateKeyKey]) == 0
		if !valid {
			return false, fmt.Errorf("cluster-tls secret is not valid")
		}

		return true, nil
	})
	if err != nil {
		logger.Fatal(red("Failed to wait for cluster-tls secret to be replicated"), zap.Error(err))
	}

	logger.Info("Creating additional test namespace")

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tls-replicator-test",
		},
	}

	if _, err := clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
		logger.Fatal(red("Failed to create test namespace"), zap.Error(err))
	}

	logger.Info("Checking that cluster-tls secret is replicated to new namespace")

	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := clientset.CoreV1().Secrets("tls-replicator-test").Get(ctx, "cluster-tls", metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			}

			logger.Info("Not yet replicated")

			return false, nil
		}

		return true, nil
	})
	if err != nil {
		logger.Fatal(red("Failed to wait for cluster-tls secret to be replicated"), zap.Error(err))
	}

	logger.Info(green("Successfully replicated cluster-tls secret"))
}

func buildOperatorImage(buildContextPath, relDockerfilePath, image string) error {
	cmd := exec.Command("docker", "build", "-t", image, "-f",
		filepath.Join(buildContextPath, relDockerfilePath), buildContextPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func createK3dCluster(clusterName string) error {
	cmd := exec.Command("k3d", "cluster", "create", clusterName, "--wait")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func deleteK3dCluster(clusterName string) error {
	cmd := exec.Command("k3d", "cluster", "delete", clusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func loadOperatorImage(clusterName, imageName string) error {
	cmd := exec.Command("k3d", "image", "import", "-c", clusterName, imageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func installCertManager(certManagerVersion string) error {
	cmd := exec.Command("kapp", "deploy", "-y", "-a", "cert-manager", "-f",
		fmt.Sprintf("https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml", certManagerVersion))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func installOperator(overrideYAMLPath, configDir string) error {
	cmd := exec.Command("ytt", "-f", overrideYAMLPath, "-f", configDir)
	patchedYAML, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	cmd = exec.Command("kapp", "deploy", "-y", "-a", "tls-replicator", "-f", "-")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = bytes.NewReader(patchedYAML)

	return cmd.Run()
}

func createExampleResources(examplesDir string) error {
	cmd := exec.Command("kapp", "deploy", "-y", "-a", "tls-replicator-examples", "-f", examplesDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
