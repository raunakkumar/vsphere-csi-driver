/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager

import (
	"context"
	"time"

	"sigs.k8s.io/vsphere-csi-driver/pkg/csi/service/logger"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/vsphere-csi-driver/pkg/syncer/cnsoperator/apis"
)

const (
	timeout  = 60 * time.Second
	pollTime = 5 * time.Second
)

// createCustomResourceDefinition creates the CRD and add it into Kubernetes. If there is error,
// it will do some clean up.
func createCustomResourceDefinition(ctx context.Context, clientSet apiextensionsclientset.Interface, crdPlural string, crdKind string) error {
	log := logger.GetLogger(ctx)
	crdName := crdPlural + "." + apis.SchemeGroupVersion.Group
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crdName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   apis.SchemeGroupVersion.Group,
			Version: apis.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: crdPlural,
				Kind:   crdKind,
			},
		},
	}
	_, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err == nil {
		log.Infof("%q CRD created successfully", crdName)
	} else if apierrors.IsAlreadyExists(err) {
		log.Debugf("%q CRD already exists", crdName)
		return nil
	} else {
		log.Errorf("failed to create %q CRD with err: %+v", crdName, err)
		return err
	}

	// CRD takes some time to be established
	// Creating an instance of non-established runs into errors. So, wait for CRD to be created
	err = wait.Poll(pollTime, timeout, func() (bool, error) {
		crd, err = clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crdName, metav1.GetOptions{})
		if err != nil {
			log.Errorf("failed to get %q CRD with err: %+v", crdName, err)
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, err
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					log.Debugf("Name conflict while waiting for %q CRD creation", cond.Reason)
				}
			}
		}

		return false, err
	})

	// If there is an error, delete the object to keep it clean.
	if err != nil {
		log.Infof("Cleanup %q CRD because the CRD created was not succesfully established. Error: %+v", crdName, err)
		deleteErr := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crdName, nil)
		if deleteErr != nil {
			log.Errorf("failed to delete %q CRD with error: %+v", crdName, deleteErr)
		}
	}
	return err
}
