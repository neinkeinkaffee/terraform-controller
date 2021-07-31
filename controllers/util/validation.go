package util

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/terraform-controller/api/v1beta1"
)

var localStackProviderTF = `
provider "aws" {
  s3_force_path_style         = true
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  endpoints {
    dynamodb = "http://localstack:4566"
    glue 	 = "http://localstack:4566"
    iam 	 = "http://localstack:4566"
    lambda   = "http://localstack:4566"
    s3       = "http://localstack:4566"
  }
}
`

type ConfigurationType string

const (
	ConfigurationJSON ConfigurationType = "JSON"
	ConfigurationHCL  ConfigurationType = "HCL"
)

func ValidConfiguration(providerNamespace string, ctx context.Context, k8sClient client.Client, configuration *v1beta1.Configuration, controllerNamespace string) (ConfigurationType, string, error) {
	json := configuration.Spec.JSON
	hcl := configuration.Spec.HCL
	switch {
	case json == "" && hcl == "":
		return "", "", errors.New("spec.JSON or spec.HCL should be set")
	case json != "" && hcl != "":
		return "", "", errors.New("spec.JSON and spec.HCL cloud not be set at the same time")
	case json != "":
		return ConfigurationJSON, json, nil
	case hcl != "":
		var providerName string
		if configuration.Spec.ProviderReference != nil {
			providerName = configuration.Spec.ProviderReference.Name
		} else {
			providerName = "default"
		}
		var provider v1beta1.Provider
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: providerName, Namespace: providerNamespace}, &provider); err != nil {
			errMsg := "failed to get Provider object"
			klog.ErrorS(err, errMsg, "Name", providerName)
			return "", "", errors.Wrap(err, errMsg)
		}
		providerTF := ""
		if provider.Spec.Provider == "localstack" {
			providerTF = localStackProviderTF
		}
		if provider.Spec.Backend != nil {
			configuration.Spec.Backend = &v1beta1.Backend{
				Type:            provider.Spec.Backend.Type,
				Bucket:          provider.Spec.Backend.Bucket,
				Region:          provider.Spec.Backend.Region,
				Key:             configuration.Name,
				InClusterConfig: false,
			}
		} else if configuration.Spec.Backend != nil {
			if configuration.Spec.Backend.SecretSuffix == "" {
				configuration.Spec.Backend.SecretSuffix = configuration.Name
			}
			configuration.Spec.Backend.InClusterConfig = true
		} else {
			configuration.Spec.Backend = &v1beta1.Backend{
				SecretSuffix:    configuration.Name,
				InClusterConfig: true,
			}
		}
		backendTF, err := renderTemplate(configuration.Spec.Backend, controllerNamespace)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to prepare Terraform backend configuration")
		}
		return ConfigurationHCL, hcl + "\n" + backendTF + "\n" + providerTF, nil
	}
	return "", "", errors.New("unknown issue")
}

// CompareTwoContainerEnvs compares two slices of v1.EnvVar
func CompareTwoContainerEnvs(s1 []v1.EnvVar, s2 []v1.EnvVar) bool {
	less := func(env1 v1.EnvVar, env2 v1.EnvVar) bool {
		return env1.Name < env2.Name
	}
	return cmp.Diff(s1, s2, cmpopts.SortSlices(less)) == ""
}
