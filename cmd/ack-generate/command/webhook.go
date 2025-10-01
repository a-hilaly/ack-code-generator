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

package command

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aws-controllers-k8s/code-generator/pkg/generate/ack"
	ackgenerate "github.com/aws-controllers-k8s/code-generator/pkg/generate/ack"
	ackmetadata "github.com/aws-controllers-k8s/code-generator/pkg/metadata"
	"github.com/aws-controllers-k8s/code-generator/pkg/model/multiversion"
)

var (
	optHubVersion              string
	optDeprecatedVersions      []string
	optEnableConversionWebhook bool
)

var webhooksCmd = &cobra.Command{
	Use:   "webhooks <service>",
	Short: "Generates Go files containing conversion, validation and defaulting webhooks.",
	RunE:  generateWebhooks,
}

func init() {
	webhooksCmd.PersistentFlags().StringVar(
		&optHubVersion, "hub-version", "", "the hub version for conversion webhooks",
	)
	webhooksCmd.PersistentFlags().BoolVar(
		&optEnableConversionWebhook, "conversion", false, "enable conversion webhooks generation",
	)
	rootCmd.AddCommand(webhooksCmd)
}

// generateWebhooks generates the Go files for conversion, defaulting and validating webhooks.
func generateWebhooks(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("please specify the service alias for the AWS service API to generate")
	}
	svcAlias := strings.ToLower(args[0])
	if optOutputPath == "" {
		optOutputPath = filepath.Join(optServicesDir, svcAlias)
	}

	apisVersionPath = filepath.Join(optOutputPath, "apis")
	files, err := ioutil.ReadDir(apisVersionPath)
	if err != nil {
		return err
	}

	apiInfos := map[string]ackmetadata.APIInfo{}
	for _, f := range files {
		metadata, err := ackmetadata.LoadGenerationMetadata(apisVersionPath, f.Name())
		if err != nil {
			return err
		}
		apiInfos[f.Name()] = ackmetadata.APIInfo{
			Status:              ackmetadata.APIStatusUnknown,
			AWSSDKVersion:       metadata.AWSSDKGoVersion,
			GeneratorConfigPath: filepath.Join(apisVersionPath, f.Name(), metadata.GeneratorConfigInfo.OriginalFileName),
		}
	}

	metadataAPIVersions := []ackmetadata.ServiceVersion{}
	for v := range apiInfos {
		metadataAPIVersions = append(metadataAPIVersions, ackmetadata.ServiceVersion{
			APIVersion: v,
			Status:     apiInfos[v].Status,
		})
	}

	if optHubVersion == "" {
		latestAPIVersion, err := getLatestAPIVersion(metadataAPIVersions)
		if err != nil {
			return err
		}
		optHubVersion = latestAPIVersion
	}

	fmt.Println("using md file", optMetadataConfigPath)
	mgr, err := multiversion.NewAPIVersionManager(
		optCacheDir,
		optMetadataConfigPath,
		svcAlias,
		optHubVersion,
		apiInfos,
		ack.DefaultConfig,
	)
	fmt.Println("got it")

	if err != nil {
		fmt.Println("could not create API version manager:", err)
		return err
	}

	fmt.Println("Generating webhooks for", svcAlias)
	if optEnableConversionWebhook {
		ts, err := ackgenerate.ConversionWebhooks(mgr, optTemplateDirs)
		if err != nil {
			return err
		}

		if err = ts.Execute(); err != nil {
			return err
		}

		for path, contents := range ts.Executed() {
			if optDryRun {
				fmt.Printf("============================= %s ======================================\n", path)
				fmt.Println(strings.TrimSpace(contents.String()))
				continue
			}
			outPath := filepath.Join(optOutputPath, path)
			outDir := filepath.Dir(outPath)
			if _, err := ensureDir(outDir); err != nil {
				return err
			}
			if err = ioutil.WriteFile(outPath, contents.Bytes(), 0666); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureDir(dir string) (bool, error) {
	if _, err := ioutil.ReadDir(dir); err != nil {
		if err = ioutil.WriteFile(dir, nil, 0755); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
