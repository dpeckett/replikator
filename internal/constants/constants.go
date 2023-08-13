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

package constants

const (
	// AnnotationEnabledKey is the annotation that enables the TLS replicator.
	AnnotationEnabledKey = "v1alpha1.tls-replicator.gpuninja.com/enabled"
	// AnnotationReplicateToKey is the annotation that specifies the target namespace/s to replicate to.
	// The value of this annotation should be a comma-separated list of values / glob patterns.
	// If this annotation is not present, the secret will be replicated to all namespaces.
	AnnotationReplicateToKey = "v1alpha1.tls-replicator.gpuninja.com/replicate-to"
	// AnnotationReplicatedKeysKey is the annotation that specifies the keys to replicate.
	// The value of this annotation should be a comma-separated list of values / glob patterns.
	// If this annotation is not present, all keys will be replicated.
	AnnotationReplicatedKeysKey = "v1alpha1.tls-replicator.gpuninja.com/replicated-keys"
	// FinalizerName is the name of the finalizer that will be added to the secret.
	FinalizerName = "finalizer.tls-replicator.gpu-ninja.com/secret"
)
