// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// Code generated by ack-generate. DO NOT EDIT.

package v1alpha1

import (
	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	"github.com/aws/aws-sdk-go/aws"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Hack to avoid import errors during build...
var (
	_ = &metav1.Time{}
	_ = &aws.JSONValue{}
	_ = ackv1alpha1.AWSAccountID("")
)


// An object representing an Amazon ECR image.
type Image struct {
	RegistryID *string `json:"registryID,omitempty"`
	RepositoryName *string `json:"repositoryName,omitempty"`
}


// An object that describes an image returned by a DescribeImages operation.
type ImageDetail struct {
	RegistryID *string `json:"registryID,omitempty"`
	RepositoryName *string `json:"repositoryName,omitempty"`
}


// An object representing a repository.
type Repository_SDK struct {
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`
	RegistryID *string `json:"registryID,omitempty"`
	RepositoryARN *string `json:"repositoryARN,omitempty"`
	RepositoryName *string `json:"repositoryName,omitempty"`
	RepositoryURI *string `json:"repositoryURI,omitempty"`
}